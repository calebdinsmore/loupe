package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// promptsDir returns the directory New() seeds into for a given root.
func promptsDir(root string) string {
	return filepath.Join(root, ".loupe", "agent", "prompts")
}

func TestSeedCreatesDefaults(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Seed(); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	want := map[string]string{
		"beads.md":    "Do not modify code: only create issues.",
		"document.md": "review-{{.ReviewID}}.md",
		"direct.md":   "editing the working tree directly",
	}
	for name, substr := range want {
		data, err := os.ReadFile(filepath.Join(promptsDir(root), name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(data), substr) {
			t.Errorf("%s = %q, want substring %q", name, data, substr)
		}
	}
}

func TestSeedIsNoOpWhenDirExists(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Seed(); err != nil {
		t.Fatalf("first Seed: %v", err)
	}

	// Edit a default and delete another to prove re-seeding leaves the
	// directory untouched (no overwrite, no re-adding removed files).
	edited := filepath.Join(promptsDir(root), "beads.md")
	if err := os.WriteFile(edited, []byte("custom edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed := filepath.Join(promptsDir(root), "direct.md")
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}

	if err := l.Seed(); err != nil {
		t.Fatalf("second Seed: %v", err)
	}

	data, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom edit" {
		t.Errorf("beads.md = %q, want it unchanged after re-seed", data)
	}
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Errorf("direct.md was re-added on re-seed (err=%v); expected seed-once", err)
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Seed(); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// A non-.md file must be ignored.
	if err := os.WriteFile(filepath.Join(promptsDir(root), "notes.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := l.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wantIDs := []string{"beads", "direct", "document"} // sorted by ID
	if len(got) != len(wantIDs) {
		t.Fatalf("List returned %d prompts, want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Errorf("List[%d].ID = %q, want %q", i, got[i].ID, id)
		}
	}
	// Name is the title-cased stem.
	if got[0].Name != "Beads" {
		t.Errorf("List[0].Name = %q, want %q", got[0].Name, "Beads")
	}
}

func TestListMissingDir(t *testing.T) {
	l := New(t.TempDir()) // never seeded
	got, err := l.List()
	if err != nil {
		t.Fatalf("List on missing dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List on missing dir = %+v, want empty", got)
	}
}

func TestRender(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Seed(); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	out, err := l.Render("document", Vars{ReviewID: 42})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "review-42.md") {
		t.Errorf("Render(document) = %q, want it to contain %q", out, "review-42.md")
	}
}

func TestRenderTrimsTrailingWhitespace(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Seed(); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	// Seed a prompt with known trailing whitespace so the assertion proves the
	// trim actually happened (rather than passing tautologically).
	if err := os.WriteFile(filepath.Join(promptsDir(root), "spacey.md"), []byte("hello {{.Branch}}\n\n  \t"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := l.Render("spacey", Vars{Branch: "feature"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != "hello feature" {
		t.Errorf("Render(spacey) = %q, want %q", out, "hello feature")
	}
}

func TestRenderMalformedTemplate(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Seed(); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir(root), "broken.md"), []byte("oops {{.Branch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Render("broken", Vars{}); err == nil {
		t.Fatal("Render(broken) returned nil error, want a template parse error")
	}
}

func TestRenderMissing(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.Seed(); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	if _, err := l.Render("missing", Vars{}); err == nil {
		t.Fatal("Render(missing) returned nil error, want a not-found error")
	}
}
