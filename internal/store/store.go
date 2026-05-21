// Package store persists reviews and their comments in a local SQLite file.
package store

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // cgo-free SQLite driver, registered as "sqlite"
)

type Store struct{ db *sql.DB }

type Review struct {
	ID        int64
	Branch    string
	Base      string
	Mode      string
	Status    string
	SessionID string
	CreatedAt time.Time
}

type Comment struct {
	ID       int64  `json:"id"`
	ReviewID int64  `json:"review_id"`
	Path     string `json:"path"`
	Side     string `json:"side"` // "left" (deletion) | "right" (addition/context)
	Line     int    `json:"line"` // 0 means a file-level comment
	BlobSHA  string `json:"blob_sha"`
	// AnchorText is the content of the commented line, captured at creation. It
	// lets the frontend relocate a comment whose stored line has drifted (working
	// -tree edits, amend/rebase) instead of misplacing or losing it.
	AnchorText string `json:"anchor_text"`
	Body       string `json:"body"`
	Submitted  bool   `json:"submitted"` // sent to the agent as part of a review run
	Collapsed  bool   `json:"collapsed"` // shown resolved/folded in the UI
}

const schema = `
CREATE TABLE IF NOT EXISTS reviews (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  branch     TEXT NOT NULL,
  base       TEXT NOT NULL,
  mode       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'draft',
  session_id TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS comments (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  review_id  INTEGER NOT NULL REFERENCES reviews(id),
  path       TEXT NOT NULL,
  side       TEXT NOT NULL DEFAULT 'right',
  line       INTEGER NOT NULL DEFAULT 0,
  blob_sha   TEXT NOT NULL DEFAULT '',
  anchor_text TEXT NOT NULL DEFAULT '',
  body       TEXT NOT NULL,
  submitted  INTEGER NOT NULL DEFAULT 0,
  collapsed  INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// migrations add columns to databases created before those columns existed.
// CREATE TABLE IF NOT EXISTS won't touch an existing table, so we ALTER and
// tolerate the "duplicate column" error on already-migrated databases.
var migrations = []string{
	`ALTER TABLE comments ADD COLUMN submitted INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE comments ADD COLUMN collapsed INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE comments ADD COLUMN anchor_text TEXT NOT NULL DEFAULT ''`,
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection serializes access to the single-file SQLite DB, avoiding
	// SQLITE_BUSY under concurrent requests (this is a local single-user app).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateReview(branch, base, mode string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO reviews (branch, base, mode) VALUES (?, ?, ?)`, branch, base, mode)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EnsureReview returns the id of the review for this branch/base pair, creating
// one (in mode) if none exists yet. A single long-lived review per pair lets the
// frontend reattach its comments after a reload.
func (s *Store) EnsureReview(branch, base, mode string) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM reviews WHERE branch = ? AND base = ? ORDER BY id LIMIT 1`, branch, base).Scan(&id)
	switch err {
	case nil:
		return id, nil
	case sql.ErrNoRows:
		return s.CreateReview(branch, base, mode)
	default:
		return 0, err
	}
}

// Reviews lists stored reviews, newest first. When branch and base are non-empty
// the list is filtered to that pair.
func (s *Store) Reviews(branch, base string) ([]Review, error) {
	query := `SELECT id, branch, base, mode, status, session_id, created_at FROM reviews`
	var args []any
	if branch != "" && base != "" {
		query += ` WHERE branch = ? AND base = ?`
		args = append(args, branch, base)
	}
	query += ` ORDER BY id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.Branch, &r.Base, &r.Mode, &r.Status, &r.SessionID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddComment(c Comment) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO comments (review_id, path, side, line, blob_sha, anchor_text, body, submitted, collapsed) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ReviewID, c.Path, c.Side, c.Line, c.BlobSHA, c.AnchorText, c.Body, c.Submitted, c.Collapsed)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateComment overwrites the editable fields of a single comment.
func (s *Store) UpdateComment(id int64, body string, submitted, collapsed bool) error {
	_, err := s.db.Exec(
		`UPDATE comments SET body = ?, submitted = ?, collapsed = ? WHERE id = ?`,
		body, submitted, collapsed, id)
	return err
}

// CommentByID fetches a single comment, used to apply partial updates.
func (s *Store) CommentByID(id int64) (Comment, error) {
	var c Comment
	err := s.db.QueryRow(
		`SELECT id, review_id, path, side, line, blob_sha, anchor_text, body, submitted, collapsed FROM comments WHERE id = ?`, id).
		Scan(&c.ID, &c.ReviewID, &c.Path, &c.Side, &c.Line, &c.BlobSHA, &c.AnchorText, &c.Body, &c.Submitted, &c.Collapsed)
	return c, err
}

func (s *Store) DeleteComment(id int64) error {
	_, err := s.db.Exec(`DELETE FROM comments WHERE id = ?`, id)
	return err
}

// MarkSubmitted flags every still-pending comment of a review as submitted and
// collapsed, returning exactly the comments that were affected by this call. The
// flag and the read happen in one UPDATE ... RETURNING so a comment inserted
// concurrently can't be marked submitted yet absent from the returned batch
// (which would silently drop it from the agent run).
func (s *Store) MarkSubmitted(reviewID int64) ([]Comment, error) {
	rows, err := s.db.Query(
		`UPDATE comments SET submitted = 1, collapsed = 1 WHERE review_id = ? AND submitted = 0
		 RETURNING id, review_id, path, side, line, blob_sha, anchor_text, body, submitted, collapsed`,
		reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanComments(rows)
	if err != nil {
		return nil, err
	}
	// RETURNING gives no ordering guarantee; sort to match the rest of the API
	// (and keep the agent prompt's comment order stable).
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

func (s *Store) Comments(reviewID int64) ([]Comment, error) {
	rows, err := s.db.Query(
		`SELECT id, review_id, path, side, line, blob_sha, anchor_text, body, submitted, collapsed FROM comments WHERE review_id = ? ORDER BY path, line`,
		reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanComments(rows)
}

func scanComments(rows *sql.Rows) ([]Comment, error) {
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.ReviewID, &c.Path, &c.Side, &c.Line, &c.BlobSHA, &c.AnchorText, &c.Body, &c.Submitted, &c.Collapsed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Review(id int64) (Review, error) {
	var r Review
	err := s.db.QueryRow(
		`SELECT id, branch, base, mode, status, session_id, created_at FROM reviews WHERE id = ?`, id).
		Scan(&r.ID, &r.Branch, &r.Base, &r.Mode, &r.Status, &r.SessionID, &r.CreatedAt)
	return r, err
}

func (s *Store) SetMode(id int64, mode string) error {
	_, err := s.db.Exec(`UPDATE reviews SET mode = ? WHERE id = ?`, mode, id)
	return err
}

func (s *Store) SetStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE reviews SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) SetSession(id int64, session string) error {
	_, err := s.db.Exec(`UPDATE reviews SET session_id = ? WHERE id = ?`, session, id)
	return err
}
