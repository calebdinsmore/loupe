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
	"time"

	"github.com/calebjdinsmore/loupe/internal/agent"
	"github.com/calebjdinsmore/loupe/internal/git"
	"github.com/calebjdinsmore/loupe/internal/prompts"
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
	// The prompts loader is seeded so /api/prompts has the three defaults.
	pl := prompts.New(t.TempDir())
	if err := pl.Seed(); err != nil {
		t.Fatalf("prompts.Seed: %v", err)
	}
	srv := New(git.New(t.TempDir()), st, agent.New(t.TempDir()), pl)
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

// promptsResp mirrors the /api/prompts payload.
type promptsResp struct {
	Prompts []struct {
		ID   string `json:"ID"`
		Name string `json:"Name"`
	} `json:"prompts"`
	Selected string `json:"selected"`
}

func getPrompts(t *testing.T, srv *Server) promptsResp {
	t.Helper()
	w := do(t, srv, http.MethodGet, "/api/prompts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/prompts = %d (%s)", w.Code, w.Body.String())
	}
	var out promptsResp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode prompts: %v", err)
	}
	return out
}

// TestPromptsEndpointDefaults verifies a freshly seeded loader exposes the three
// default prompts (sorted by id) and defaults the selection to "document".
func TestPromptsEndpointDefaults(t *testing.T) {
	srv, _ := newTestServer(t)
	out := getPrompts(t, srv)
	var ids []string
	for _, p := range out.Prompts {
		ids = append(ids, p.ID)
	}
	if len(ids) != 3 || ids[0] != "beads" || ids[1] != "direct" || ids[2] != "document" {
		t.Fatalf("expected [beads direct document], got %v", ids)
	}
	if out.Selected != "document" {
		t.Fatalf("default selected = %q, want document", out.Selected)
	}
}

// TestPromptsSelectionPersistsOnSubmit checks that submitting persists the chosen
// prompt as the global selection (this happens before the no-pending-comments
// guard, so it sticks even when there is nothing to run).
func TestPromptsSelectionPersistsOnSubmit(t *testing.T) {
	srv, st := newTestServer(t)
	rev, _ := st.CreateReview("feat", "main", "document")
	// No pending comments: submit 400s after persisting the selection.
	w := do(t, srv, http.MethodPost, "/api/reviews/"+itoa(rev)+"/submit", `{"mode":"direct"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("submit = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if out := getPrompts(t, srv); out.Selected != "direct" {
		t.Fatalf("selected after submit = %q, want direct", out.Selected)
	}
}

// TestPromptsSelectionFallsBack verifies the selection falls back to an existing
// prompt when last_prompt points at one that no longer exists.
func TestPromptsSelectionFallsBack(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.SetSetting("last_prompt", "ghost"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if out := getPrompts(t, srv); out.Selected != "document" {
		t.Fatalf("selected with stale last_prompt = %q, want document fallback", out.Selected)
	}
}

// TestSubmitMissingPromptBroadcastsError verifies that submitting a review whose
// prompt id no longer resolves broadcasts an error event and marks the review
// errored, rather than running the agent with an empty task.
func TestSubmitMissingPromptBroadcastsError(t *testing.T) {
	srv, st := newTestServer(t)
	rev, _ := st.CreateReview("feat", "main", "document")
	if _, err := st.AddComment(store.Comment{ReviewID: rev, Path: "a.go", Line: 1, Body: "x"}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	// Subscribe before submitting so the run's error event is delivered live.
	ch := srv.hubFor(rev).subscribe()

	w := do(t, srv, http.MethodPost, "/api/reviews/"+itoa(rev)+"/submit", `{"mode":"ghost"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("submit = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	select {
	case ev := <-ch:
		if ev.Type != agent.EventError {
			t.Fatalf("first event = %q, want error", ev.Type)
		}
		if !strings.Contains(ev.Text, "ghost") {
			t.Fatalf("error text %q should name the missing prompt", ev.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the render error event")
	}

	// The run never started: status is error.
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := st.Review(rev)
		if err != nil {
			t.Fatalf("Review: %v", err)
		}
		if got.Status == "error" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %q, want error", got.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

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
	// A second, non-checked-out branch sitting at the base commit. Its diff
	// against main is empty and, crucially, it never reflects the working tree —
	// so it stays committed-only even after the uncommitted edit below.
	runGit("branch", "other")
	// Stay on main and make an uncommitted edit in the working tree.
	write("file.txt", "base line\nuncommitted line\n")

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(git.New(dir), st, agent.New(dir), prompts.New(t.TempDir()))

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

	// A non-checked-out branch keeps the committed-only diff (no working tree).
	if got := diffOf("other"); strings.Contains(got, "uncommitted line") {
		t.Fatalf("committed diff unexpectedly contains the uncommitted edit:\n%s", got)
	}
	// The sentinel routes to DiffWorking, which surfaces the uncommitted edit.
	if got := diffOf(git.WorkingRef); !strings.Contains(got, "uncommitted line") {
		t.Fatalf("working-tree diff missing the uncommitted edit:\n%s", got)
	}
}

// TestDiffCurrentBranchFoldsWorkingTree verifies the refinement from loupe-2sq.4:
// requesting the currently checked-out branch folds in uncommitted working
// changes (routes to DiffWorking) without selecting the *working* sentinel, while
// a different, non-checked-out branch stays committed-only.
func TestDiffCurrentBranchFoldsWorkingTree(t *testing.T) {
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
	// Branch off and commit a change, then check the feature branch out so it is
	// the current branch. main stays at the base commit (non-checked-out).
	runGit("checkout", "-b", "feature")
	write("file.txt", "base line\ncommitted feature line\n")
	runGit("commit", "-am", "feature change")
	// Uncommitted edit in the working tree on the current (feature) branch.
	write("file.txt", "base line\ncommitted feature line\nuncommitted line\n")

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(git.New(dir), st, agent.New(dir), prompts.New(t.TempDir()))

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

	// The current branch folds in the working tree: both the committed feature
	// change and the uncommitted edit show, with no *working* selection.
	cur := diffOf("feature")
	if !strings.Contains(cur, "committed feature line") {
		t.Fatalf("current-branch diff missing the committed change:\n%s", cur)
	}
	if !strings.Contains(cur, "uncommitted line") {
		t.Fatalf("current-branch diff missing the uncommitted edit:\n%s", cur)
	}
	// A different, non-checked-out branch stays committed-only — main sits at the
	// base commit and never reflects feature's working tree.
	if got := diffOf("main"); strings.Contains(got, "uncommitted line") {
		t.Fatalf("non-checked-out branch diff unexpectedly contains the uncommitted edit:\n%s", got)
	}
}

// TestDiffDetachedHeadRoutesToCommittedDiff covers the cur != "" guard in
// diffFor: when HEAD is detached, CurrentBranch() is "", so no requested branch
// should be mistaken for "the current branch". A request for a real branch must
// route to the committed-only Diff path even though uncommitted edits sit in the
// working tree — otherwise a detached checkout would silently fold them in.
func TestDiffDetachedHeadRoutesToCommittedDiff(t *testing.T) {
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
	runGit("checkout", "-b", "feature")
	write("file.txt", "base line\ncommitted feature line\n")
	runGit("commit", "-am", "feature change")
	// Detach HEAD at feature's tip, then make an uncommitted edit. CurrentBranch()
	// now returns "" — the working tree no longer belongs to a named branch.
	runGit("checkout", "--detach")
	write("file.txt", "base line\ncommitted feature line\nuncommitted line\n")

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(git.New(dir), st, agent.New(dir), prompts.New(t.TempDir()))

	if cur := git.New(dir).CurrentBranch(); cur != "" {
		t.Fatalf("expected detached HEAD (empty CurrentBranch), got %q", cur)
	}

	// A request for a real branch routes to the committed-only Diff path: the
	// committed feature change shows, the working-tree edit does not.
	target := "/api/diff?base=main&branch=" + url.QueryEscape("feature")
	w := do(t, srv, http.MethodGet, target, "")
	if w.Code != http.StatusOK {
		t.Fatalf("diff feature = %d (%s)", w.Code, w.Body.String())
	}
	var out struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if !strings.Contains(out.Diff, "committed feature line") {
		t.Fatalf("committed diff missing the committed change:\n%s", out.Diff)
	}
	if strings.Contains(out.Diff, "uncommitted line") {
		t.Fatalf("detached-HEAD request unexpectedly folded in the working tree:\n%s", out.Diff)
	}

	// Pin the `cur != ""` guard specifically: an empty branch param must not match
	// the (also empty) detached CurrentBranch and route to DiffWorking. Without the
	// guard, "" == "" would fold the working tree in; with it, the empty ref falls
	// through to Diff and errors. Either way the uncommitted edit must never leak.
	empty := do(t, srv, http.MethodGet, "/api/diff?base=main&branch=", "")
	if strings.Contains(empty.Body.String(), "uncommitted line") {
		t.Fatalf("empty branch param under detached HEAD folded in the working tree (guard regressed):\n%s", empty.Body.String())
	}
}
