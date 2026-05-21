// Package adapter turns a submitted review into the prompt and tool allowlist
// handed to the agent.
package adapter

import (
	"fmt"
	"strings"

	"github.com/calebjdinsmore/loupe/internal/git"
	"github.com/calebjdinsmore/loupe/internal/store"
)

// DefaultTools is the permissive allowlist used for every prompt so headless
// runs do not stall on permission prompts.
func DefaultTools() []string {
	return []string{"Edit", "Write", "Read", "Grep", "Glob", "Bash"}
}

// BuildPrompt assembles the full payload handed to the agent. The task text is
// the rendered prompt body that goes under the "## Your task" header.
func BuildPrompt(r store.Review, comments []store.Comment, diff, task string) string {
	var b strings.Builder
	if r.Branch == git.WorkingRef {
		fmt.Fprintf(&b, "You are addressing a code review of the uncommitted working-tree changes against base `%s` (committed + uncommitted changes together).\n\n", r.Base)
	} else {
		fmt.Fprintf(&b, "You are addressing a code review on branch `%s` (base `%s`).\n\n", r.Branch, r.Base)
	}

	b.WriteString("## Reviewer comments\n\n")
	for _, c := range comments {
		if c.Line > 0 {
			fmt.Fprintf(&b, "- `%s:%d` (%s side): %s\n", c.Path, c.Line, c.Side, c.Body)
		} else {
			fmt.Fprintf(&b, "- `%s` (file-level): %s\n", c.Path, c.Body)
		}
	}

	b.WriteString("\n## Diff under review\n\n```diff\n")
	b.WriteString(diff)
	b.WriteString("```\n\n## Your task\n\n")
	b.WriteString(task)
	return b.String()
}
