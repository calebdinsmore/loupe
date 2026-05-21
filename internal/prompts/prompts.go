// Package prompts owns the on-disk, user-editable agent prompt files under
// .loupe/agent/prompts. It seeds embedded defaults on first run, lists the
// available prompts, and renders a selected prompt through text/template.
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"
)

//go:embed defaults/*.md
var defaultsFS embed.FS

// Loader reads and renders prompt files from a single directory.
type Loader struct{ dir string }

// New returns a Loader bound to <root>/.loupe/agent/prompts.
func New(root string) *Loader {
	return &Loader{dir: filepath.Join(root, ".loupe", "agent", "prompts")}
}

// Prompt identifies a prompt file. ID is the filename stem (e.g. "document");
// Name is a display label (title-case of the stem).
type Prompt struct {
	ID, Name string
}

// Vars are the template placeholders available to every prompt.
type Vars struct {
	ReviewID     int64
	Branch, Base string
}

// Seed copies the embedded default prompts into the loader's directory the
// first time it runs. If the directory already exists it is left untouched
// (seed-once), so user edits and additions are never clobbered.
func (l *Loader) Seed() error {
	if _, err := os.Stat(l.dir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(defaultsFS, "defaults")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(defaultsFS, "defaults/"+e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(l.dir, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// List returns the available prompts sorted by ID, ignoring non-.md files. A
// missing directory yields an empty slice (the dir is seeded lazily on startup).
func (l *Loader) List() ([]Prompt, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ps []Prompt
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		ps = append(ps, Prompt{ID: id, Name: title(id)})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	return ps, nil
}

// Render reads the prompt with the given ID, executes it as a text/template
// against v, and returns the result with trailing whitespace trimmed.
func (l *Loader) Render(id string, v Vars) (string, error) {
	data, err := os.ReadFile(filepath.Join(l.dir, id+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("prompt %q not found", id)
		}
		return "", err
	}
	tmpl, err := template.New(id).Parse(string(data))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, v); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), " \t\r\n"), nil
}

// title upper-cases the first rune of s for use as a display label.
func title(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
