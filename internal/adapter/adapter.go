// Package adapter turns a submitted review into the prompt and tool allowlist
// for the chosen output mode: beads, document, or direct edits.
package adapter

import (
	"fmt"
	"strings"

	"github.com/calebjdinsmore/loupe/internal/store"
)

type Mode string

const (
	ModeBeads    Mode = "beads"
	ModeDocument Mode = "document"
	ModeDirect   Mode = "direct"
)

// AllowedTools is the minimal tool set each mode needs, so headless runs don't
// stall on permission prompts.
func AllowedTools(m Mode) []string {
	switch m {
	case ModeBeads:
		return []string{"Bash(bd:*)", "Read", "Grep", "Glob"}
	case ModeDocument:
		return []string{"Write", "Read", "Grep", "Glob"}
	case ModeDirect:
		return []string{"Edit", "Write", "Read", "Grep", "Glob", "Bash"}
	default:
		return []string{"Read", "Grep", "Glob"}
	}
}

func instruction(m Mode, reviewID int64) string {
	switch m {
	case ModeBeads:
		return "Using the `bd` CLI, create one epic issue for this review and a child issue " +
			"for each cluster of related comments, wiring up parent/child dependencies. " +
			"Do not modify code — only create issues."
	case ModeDocument:
		return fmt.Sprintf("Write a markdown implementation plan that addresses every comment to "+
			"`.loupe/plans/review-%d.md`. Research the codebase as needed. Do not modify any other files.", reviewID)
	case ModeDirect:
		return "Address each comment by editing the working tree directly. " +
			"After each change, briefly state which comment it resolves."
	default:
		return ""
	}
}

// BuildPrompt assembles the full payload handed to the agent.
func BuildPrompt(r store.Review, comments []store.Comment, diff string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are addressing a code review on branch `%s` (base `%s`).\n\n", r.Branch, r.Base)

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
	b.WriteString(instruction(Mode(r.Mode), r.ID))
	return b.String()
}
