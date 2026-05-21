// Package git wraps the git CLI for branch listing and diff computation.
package git

import (
	"os/exec"
	"strings"
)

// WorkingRef is the sentinel "branch" value that selects the working tree
// (committed + uncommitted changes) instead of a real branch. It is shared by
// the server dispatch and the prompt wording so the working-tree view reuses
// the existing base/branch UI, persistence key, and WebSocket.
const WorkingRef = "*working*"

type Service struct{ root string }

func New(root string) *Service { return &Service{root: root} }

func (s *Service) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.root
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Branches lists local branch names.
func (s *Service) Branches() ([]string, error) {
	out, err := s.run("branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var bs []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			bs = append(bs, l)
		}
	}
	return bs, nil
}

// CurrentBranch returns the checked-out branch, or "" if HEAD is detached.
func (s *Service) CurrentBranch() string {
	if b, err := s.run("rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "HEAD" {
		return b
	}
	return ""
}

// DefaultBase guesses the base branch: origin/HEAD, else main, else master.
func (s *Service) DefaultBase() string {
	if ref, err := s.run("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return strings.TrimPrefix(ref, "origin/")
	}
	for _, c := range []string{"main", "master"} {
		if _, err := s.run("rev-parse", "--verify", c); err == nil {
			return c
		}
	}
	return "main"
}

// MergeBase returns the common ancestor of base and branch.
func (s *Service) MergeBase(base, branch string) (string, error) {
	return s.run("merge-base", base, branch)
}

// Diff returns the raw unified diff from merge-base(base, branch) to branch,
// i.e. only what `branch` introduced — the GitHub "three-dot" comparison.
func (s *Service) Diff(base, branch string) (string, error) {
	mb, err := s.MergeBase(base, branch)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "diff", "--no-color", mb, branch)
	cmd.Dir = s.root
	out, err := cmd.Output()
	return string(out), err
}

// DiffWorking returns the diff of the working tree (committed + uncommitted
// changes together) against merge-base(base, HEAD). Diffing from the merge-base
// rather than HEAD subsumes `git diff HEAD`, so uncommitted edits show up even
// when base is the current branch tip. Untracked files don't appear in a plain
// `git diff`, so each is appended as a synthetic add via `git diff --no-index`.
func (s *Service) DiffWorking(base string) (string, error) {
	mb, err := s.MergeBase(base, "HEAD")
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "diff", "--no-color", mb)
	cmd.Dir = s.root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	diff := string(out)

	untracked, err := s.run("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	for _, f := range strings.Split(untracked, "\n") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		ucmd := exec.Command("git", "diff", "--no-color", "--no-index", "/dev/null", f)
		ucmd.Dir = s.root
		uout, uerr := ucmd.Output()
		// `git diff --no-index` exits 1 when the inputs differ, which is the
		// normal case for an untracked file vs /dev/null — not a real error.
		if uerr != nil {
			if ee, ok := uerr.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
				return "", uerr
			}
		}
		diff += string(uout)
	}
	return diff, nil
}
