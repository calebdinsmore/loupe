// Package agent spawns the Claude Code CLI in headless stream-json mode and
// turns its newline-delimited JSON output into a channel of typed events.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
)

type Runner struct{ root string }

func New(root string) *Runner { return &Runner{root: root} }

type EventType string

const (
	EventSystem EventType = "system"   // init event; carries SessionID
	EventText   EventType = "text"     // assistant prose
	EventTool   EventType = "tool_use" // assistant invoked a tool
	EventResult EventType = "result"   // terminal summary
	EventError  EventType = "error"    // spawn/exit failure
)

type Event struct {
	Type      EventType `json:"type"`
	Text      string    `json:"text,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

// streamLine is the subset of claude's stream-json schema we consume.
type streamLine struct {
	Type      string          `json:"type"` // system | assistant | user | result
	SessionID string          `json:"session_id"`
	Message   json.RawMessage `json:"message"`
	Result    string          `json:"result"`
}

type apiMessage struct {
	Content []struct {
		Type string `json:"type"` // text | tool_use | tool_result
		Text string `json:"text"`
		Name string `json:"name"`
	} `json:"content"`
}

// Run executes `claude -p` for a one-shot review pass. Pass resumeID to continue
// a prior session (interactive follow-ups); leave it empty to start fresh.
func (r *Runner) Run(ctx context.Context, prompt string, allowedTools []string, resumeID string) (<-chan Event, error) {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose", // required with stream-json in print mode
		"--permission-mode", "acceptEdits",
		"--add-dir", r.root,
	}
	if len(allowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(allowedTools, ","))
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = r.root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	out := make(chan Event, 64)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 1<<20), 8<<20) // tolerate large lines
		for sc.Scan() {
			var line streamLine
			if json.Unmarshal(sc.Bytes(), &line) != nil {
				continue
			}
			switch line.Type {
			case "system":
				out <- Event{Type: EventSystem, SessionID: line.SessionID}
			case "assistant":
				var m apiMessage
				_ = json.Unmarshal(line.Message, &m)
				for _, c := range m.Content {
					switch c.Type {
					case "text":
						if c.Text != "" {
							out <- Event{Type: EventText, Text: c.Text}
						}
					case "tool_use":
						out <- Event{Type: EventTool, Tool: c.Name}
					}
				}
			case "result":
				out <- Event{Type: EventResult, Text: line.Result}
			}
		}
		if err := cmd.Wait(); err != nil {
			out <- Event{Type: EventError, Text: err.Error()}
		}
	}()
	return out, nil
}
