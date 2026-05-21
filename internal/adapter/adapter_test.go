package adapter

import (
	"strings"
	"testing"

	"github.com/calebjdinsmore/loupe/internal/git"
	"github.com/calebjdinsmore/loupe/internal/store"
)

// TestBuildPromptWorkingRefHeader checks the prompt header switches wording for
// the working-tree sentinel and keeps the branch/base wording otherwise.
func TestBuildPromptWorkingRefHeader(t *testing.T) {
	comments := []store.Comment{{Path: "a.go", Line: 4, Side: "right", Body: "fix this"}}

	working := BuildPrompt(store.Review{Branch: git.WorkingRef, Base: "main", Mode: string(ModeDocument)}, comments, "diff")
	if !strings.Contains(working, "uncommitted working-tree changes against base `main`") {
		t.Errorf("working-tree prompt missing the sentinel header wording:\n%s", working)
	}
	if strings.Contains(working, "on branch `"+git.WorkingRef+"`") {
		t.Errorf("working-tree prompt should not name the sentinel as a branch:\n%s", working)
	}

	normal := BuildPrompt(store.Review{Branch: "feat", Base: "main", Mode: string(ModeDocument)}, comments, "diff")
	if !strings.Contains(normal, "on branch `feat` (base `main`)") {
		t.Errorf("normal prompt missing the branch/base header wording:\n%s", normal)
	}
	if strings.Contains(normal, "uncommitted working-tree changes") {
		t.Errorf("normal prompt unexpectedly used the working-tree wording:\n%s", normal)
	}
}
