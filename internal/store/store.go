// Package store persists reviews and their comments in a local SQLite file.
package store

import (
	"database/sql"
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
	Body     string `json:"body"`
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
  body       TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
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

func (s *Store) AddComment(c Comment) error {
	_, err := s.db.Exec(
		`INSERT INTO comments (review_id, path, side, line, blob_sha, body) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ReviewID, c.Path, c.Side, c.Line, c.BlobSHA, c.Body)
	return err
}

func (s *Store) Comments(reviewID int64) ([]Comment, error) {
	rows, err := s.db.Query(
		`SELECT id, review_id, path, side, line, blob_sha, body FROM comments WHERE review_id = ? ORDER BY path, line`,
		reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.ReviewID, &c.Path, &c.Side, &c.Line, &c.BlobSHA, &c.Body); err != nil {
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

func (s *Store) SetStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE reviews SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) SetSession(id int64, session string) error {
	_, err := s.db.Exec(`UPDATE reviews SET session_id = ? WHERE id = ?`, session, id)
	return err
}
