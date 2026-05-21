package store

import (
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestEnsureReviewIdempotent(t *testing.T) {
	s := openTest(t)
	first, err := s.EnsureReview("feat/multiply", "main", "document")
	if err != nil {
		t.Fatalf("EnsureReview: %v", err)
	}
	again, err := s.EnsureReview("feat/multiply", "main", "direct")
	if err != nil {
		t.Fatalf("EnsureReview (second): %v", err)
	}
	if first != again {
		t.Fatalf("EnsureReview not idempotent: got %d then %d", first, again)
	}
	// A different pair gets its own review.
	other, err := s.EnsureReview("feat/other", "main", "document")
	if err != nil {
		t.Fatalf("EnsureReview (other pair): %v", err)
	}
	if other == first {
		t.Fatalf("distinct pairs shared a review id %d", other)
	}
}

func TestAddCommentReturnsUsableID(t *testing.T) {
	s := openTest(t)
	rev, err := s.CreateReview("feat", "main", "document")
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	id, err := s.AddComment(Comment{ReviewID: rev, Path: "a.go", Line: 3, Side: "right", Body: "hi"})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if id <= 0 {
		t.Fatalf("AddComment returned non-positive id %d", id)
	}
	got, err := s.CommentByID(id)
	if err != nil {
		t.Fatalf("CommentByID: %v", err)
	}
	if got.Body != "hi" || got.Path != "a.go" || got.Line != 3 {
		t.Fatalf("round-tripped comment mismatch: %+v", got)
	}
}

func TestUpdateAndDeleteComment(t *testing.T) {
	s := openTest(t)
	rev, _ := s.CreateReview("feat", "main", "document")
	id, _ := s.AddComment(Comment{ReviewID: rev, Path: "a.go", Line: 1, Body: "old"})

	if err := s.UpdateComment(id, "new", true, true); err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	got, _ := s.CommentByID(id)
	if got.Body != "new" || !got.Submitted || !got.Collapsed {
		t.Fatalf("UpdateComment did not persist edits: %+v", got)
	}

	if err := s.DeleteComment(id); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if _, err := s.CommentByID(id); err == nil {
		t.Fatalf("CommentByID succeeded after delete")
	}
}

func TestCommentsAndReviewsFiltering(t *testing.T) {
	s := openTest(t)
	a, _ := s.CreateReview("feat/a", "main", "document")
	b, _ := s.CreateReview("feat/b", "main", "document")
	s.AddComment(Comment{ReviewID: a, Path: "z.go", Line: 2, Body: "1"})
	s.AddComment(Comment{ReviewID: a, Path: "a.go", Line: 1, Body: "2"})
	s.AddComment(Comment{ReviewID: b, Path: "c.go", Line: 1, Body: "3"})

	ca, err := s.Comments(a)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(ca) != 2 {
		t.Fatalf("Comments(a) = %d, want 2", len(ca))
	}
	// Comments come back ordered by path, then line.
	if ca[0].Path != "a.go" || ca[1].Path != "z.go" {
		t.Fatalf("Comments not ordered by path: %+v", ca)
	}

	revs, err := s.Reviews("feat/b", "main")
	if err != nil {
		t.Fatalf("Reviews: %v", err)
	}
	if len(revs) != 1 || revs[0].ID != b {
		t.Fatalf("Reviews filter mismatch: %+v", revs)
	}
}

func TestMarkSubmittedReturnsFlaggedRows(t *testing.T) {
	s := openTest(t)
	rev, _ := s.CreateReview("feat", "main", "document")
	want := map[int64]bool{}
	for i := 0; i < 3; i++ {
		id, _ := s.AddComment(Comment{ReviewID: rev, Path: "f.go", Line: i, Body: "x"})
		want[id] = true
	}

	batch, err := s.MarkSubmitted(rev)
	if err != nil {
		t.Fatalf("MarkSubmitted: %v", err)
	}
	if len(batch) != len(want) {
		t.Fatalf("MarkSubmitted returned %d, want %d", len(batch), len(want))
	}
	for _, c := range batch {
		if !want[c.ID] {
			t.Fatalf("MarkSubmitted returned unexpected comment %d", c.ID)
		}
		if !c.Submitted || !c.Collapsed {
			t.Fatalf("returned comment %d not flagged submitted/collapsed: %+v", c.ID, c)
		}
	}
	// The returned batch must equal exactly the rows now flagged submitted.
	all, _ := s.Comments(rev)
	flagged := 0
	for _, c := range all {
		if c.Submitted {
			flagged++
			if !want[c.ID] {
				t.Fatalf("comment %d flagged submitted but never returned", c.ID)
			}
		}
	}
	if flagged != len(batch) {
		t.Fatalf("%d rows flagged submitted but batch had %d", flagged, len(batch))
	}

	// Re-submitting with nothing pending returns an empty batch.
	again, err := s.MarkSubmitted(rev)
	if err != nil {
		t.Fatalf("MarkSubmitted (second): %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second MarkSubmitted returned %d, want 0", len(again))
	}
}

// TestMarkSubmittedNoDropUnderConcurrency is the regression for the
// insert-during-submit interleaving: a comment inserted between the old
// SELECT-then-UPDATE could be flagged submitted yet absent from the returned
// batch (silently dropped from the agent run). With the atomic UPDATE ...
// RETURNING, every comment that ever exists is returned by exactly one
// MarkSubmitted call.
func TestMarkSubmittedNoDropUnderConcurrency(t *testing.T) {
	s := openTest(t)
	rev, _ := s.CreateReview("feat", "main", "document")

	const n = 200
	added := make([]int64, 0, n)
	var addMu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			id, err := s.AddComment(Comment{ReviewID: rev, Path: "f.go", Line: i, Body: "x"})
			if err != nil {
				t.Errorf("AddComment: %v", err)
				return
			}
			addMu.Lock()
			added = append(added, id)
			addMu.Unlock()
		}
	}()

	returned := map[int64]int{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			batch, err := s.MarkSubmitted(rev)
			if err != nil {
				t.Errorf("MarkSubmitted: %v", err)
				return
			}
			for _, c := range batch {
				returned[c.ID]++
			}
			addMu.Lock()
			done := len(added) == n
			addMu.Unlock()
			if done && len(batch) == 0 {
				return
			}
		}
	}()

	wg.Wait()
	// Drain anything added after the submitter's final pass.
	for {
		batch, err := s.MarkSubmitted(rev)
		if err != nil {
			t.Fatalf("MarkSubmitted (drain): %v", err)
		}
		for _, c := range batch {
			returned[c.ID]++
		}
		if len(batch) == 0 {
			break
		}
	}

	if len(added) != n {
		t.Fatalf("added %d comments, want %d", len(added), n)
	}
	for _, id := range added {
		switch returned[id] {
		case 1: // returned exactly once — correct
		case 0:
			t.Fatalf("comment %d was flagged submitted but never returned (dropped)", id)
		default:
			t.Fatalf("comment %d returned %d times (double-submitted)", id, returned[id])
		}
	}
	// And nothing extra was returned.
	want := append([]int64(nil), added...)
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(returned) != len(added) {
		t.Fatalf("returned %d distinct comments, want %d", len(returned), len(added))
	}
}
