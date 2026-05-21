package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/calebjdinsmore/loupe/internal/agent"
	"github.com/calebjdinsmore/loupe/internal/git"
	"github.com/calebjdinsmore/loupe/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	// git/agent point at a temp dir; the routes under test never invoke them.
	srv := New(git.New(t.TempDir()), st, agent.New(t.TempDir()))
	return srv, st
}

func do(t *testing.T, srv *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

// TestNonNumericPathIDReturns400 is the regression for the discarded
// strconv.ParseInt error: a non-numeric id used to become 0 and hit the store
// as a silent miss. Each id route must now reject it with 400.
func TestNonNumericPathIDReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	cases := []struct {
		method, target, body string
	}{
		{http.MethodGet, "/api/reviews/abc/comments", ""},
		{http.MethodPost, "/api/reviews/abc/comments", `{"path":"a.go","line":1,"body":"x"}`},
		{http.MethodPost, "/api/reviews/abc/submit", `{"mode":"document"}`},
		{http.MethodPatch, "/api/comments/abc", `{"body":"x"}`},
		{http.MethodDelete, "/api/comments/abc", ""},
	}
	for _, c := range cases {
		w := do(t, srv, c.method, c.target, c.body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400 (body %q)", c.method, c.target, w.Code, w.Body.String())
		}
	}
}

func TestCommentCRUD(t *testing.T) {
	srv, st := newTestServer(t)
	rev, err := st.CreateReview("feat", "main", "document")
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	// Add a comment via the API.
	w := do(t, srv, http.MethodPost, "/api/reviews/"+itoa(rev)+"/comments",
		`{"path":"a.go","side":"right","line":4,"body":"hi"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add comment = %d (%s)", w.Code, w.Body.String())
	}
	var created store.Comment
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}
	if created.ID == 0 || created.ReviewID != rev || created.Body != "hi" {
		t.Fatalf("unexpected created comment: %+v", created)
	}

	// List it back.
	w = do(t, srv, http.MethodGet, "/api/reviews/"+itoa(rev)+"/comments", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list comments = %d", w.Code)
	}
	var listed struct {
		Comments []store.Comment `json:"comments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Comments) != 1 || listed.Comments[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", listed.Comments)
	}

	// Patch the body.
	w = do(t, srv, http.MethodPatch, "/api/comments/"+itoa(created.ID), `{"body":"edited"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch = %d (%s)", w.Code, w.Body.String())
	}
	got, _ := st.CommentByID(created.ID)
	if got.Body != "edited" {
		t.Fatalf("patch did not persist: %+v", got)
	}

	// Delete returns 204 and removes the row.
	w = do(t, srv, http.MethodDelete, "/api/comments/"+itoa(created.ID), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", w.Code)
	}
	if _, err := st.CommentByID(created.ID); err == nil {
		t.Fatalf("comment still present after delete")
	}
}

func TestSubmitNoPendingReturns400(t *testing.T) {
	srv, st := newTestServer(t)
	rev, _ := st.CreateReview("feat", "main", "document")
	// No comments added, so submit has nothing pending and must 400 before
	// spawning the agent.
	w := do(t, srv, http.MethodPost, "/api/reviews/"+itoa(rev)+"/submit", `{"mode":"document"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("submit with no pending = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

func TestListReviewsEmbedsComments(t *testing.T) {
	srv, st := newTestServer(t)
	rev, _ := st.CreateReview("feat/multiply", "main", "document")
	st.AddComment(store.Comment{ReviewID: rev, Path: "a.go", Line: 1, Body: "x"})

	w := do(t, srv, http.MethodGet, "/api/reviews?branch=feat/multiply&base=main", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list reviews = %d", w.Code)
	}
	var out struct {
		Reviews []struct {
			ID       int64           `json:"ID"`
			Comments []store.Comment `json:"comments"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode reviews: %v", err)
	}
	if len(out.Reviews) != 1 || len(out.Reviews[0].Comments) != 1 {
		t.Fatalf("expected one review with one comment, got %+v", out.Reviews)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestDiffWorkingRefRoutesToWorkingTree verifies the working-tree sentinel
// routes /api/diff to DiffWorking: requesting branch=*working* surfaces an
// uncommitted edit that the committed branch diff does not.
func TestDiffWorkingRefRoutesToWorkingTree(t *testing.T) {
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	write("file.txt", "base line\n")
	runGit("add", "file.txt")
	runGit("commit", "-m", "base")
	// Uncommitted edit in the working tree on main.
	write("file.txt", "base line\nuncommitted line\n")

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(git.New(dir), st, agent.New(dir))

	diffOf := func(branch string) string {
		t.Helper()
		target := "/api/diff?base=main&branch=" + url.QueryEscape(branch)
		w := do(t, srv, http.MethodGet, target, "")
		if w.Code != http.StatusOK {
			t.Fatalf("diff %s = %d (%s)", branch, w.Code, w.Body.String())
		}
		var out struct {
			Diff string `json:"diff"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode diff: %v", err)
		}
		return out.Diff
	}

	// The committed diff of main against itself is empty (no uncommitted view).
	if got := diffOf("main"); strings.Contains(got, "uncommitted line") {
		t.Fatalf("committed diff unexpectedly contains the uncommitted edit:\n%s", got)
	}
	// The sentinel routes to DiffWorking, which surfaces the uncommitted edit.
	if got := diffOf(git.WorkingRef); !strings.Contains(got, "uncommitted line") {
		t.Fatalf("working-tree diff missing the uncommitted edit:\n%s", got)
	}
}
