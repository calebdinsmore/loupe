package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// writeFile writes content to name within dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// initRepo creates a git repo with a base commit on `main` containing file.txt,
// then a committed change plus an uncommitted edit plus an untracked file. It
// returns the Service and the base branch name.
func initRepo(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Base commit on main.
	writeFile(t, dir, "file.txt", "base line\n")
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "base")

	// A feature branch with a committed change.
	runGit(t, dir, "checkout", "-b", "feat")
	writeFile(t, dir, "file.txt", "base line\ncommitted line\n")
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "committed change")

	// Uncommitted edit on top of the committed change.
	writeFile(t, dir, "file.txt", "base line\ncommitted line\nuncommitted line\n")

	// Untracked file (not added).
	writeFile(t, dir, "new.txt", "brand new file\n")

	return New(dir), "main"
}

func TestDiffWorkingIncludesCommittedUncommittedAndUntracked(t *testing.T) {
	s, base := initRepo(t)

	diff, err := s.DiffWorking(base)
	if err != nil {
		t.Fatalf("DiffWorking: %v", err)
	}

	for _, want := range []string{
		"committed line",   // committed change vs base
		"uncommitted line", // uncommitted edit in the working tree
		"new.txt",          // untracked file path appears
		"brand new file",   // untracked file content appears
	} {
		if !strings.Contains(diff, want) {
			t.Errorf("DiffWorking missing %q\n--- diff ---\n%s", want, diff)
		}
	}
}

// TestDiffWorkingDiffersFromCommittedDiff confirms DiffWorking captures the
// uncommitted edit that the committed-only Diff does not.
func TestDiffWorkingDiffersFromCommittedDiff(t *testing.T) {
	s, base := initRepo(t)

	committed, err := s.Diff(base, "feat")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if strings.Contains(committed, "uncommitted line") {
		t.Fatalf("committed Diff unexpectedly contains the uncommitted edit:\n%s", committed)
	}

	working, err := s.DiffWorking(base)
	if err != nil {
		t.Fatalf("DiffWorking: %v", err)
	}
	if !strings.Contains(working, "uncommitted line") {
		t.Fatalf("DiffWorking missing the uncommitted edit:\n%s", working)
	}
}
