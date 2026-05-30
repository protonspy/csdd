package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfirmAnswer drives the interactive prompt with a canned answer for the
// duration of f, then restores the non-interactive default.
func withConfirmAnswer(t *testing.T, answer string, f func()) {
	t.Helper()
	prev := confirmStdin
	confirmStdin = strings.NewReader(answer)
	defer func() { confirmStdin = prev }()
	f()
}

func readGitignore(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(data)
}

// TestInitAddsArtifactsToGitignoreOnYes covers the happy path: kspec.md (always
// created by init) and a present binary are both offered and appended on "y".
func TestInitAddsArtifactsToGitignoreOnYes(t *testing.T) {
	dir := t.TempDir()
	// Drop a fake binary at root so gitignoreTargets picks it up.
	if err := os.WriteFile(filepath.Join(dir, "kspec"), []byte("ELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	withConfirmAnswer(t, "y\n", func() {
		if code, _, errOut := run(t, "init", "--root", dir); code != 0 {
			t.Fatalf("init failed (code=%d): %s", code, errOut)
		}
	})
	got := readGitignore(t, dir)
	for _, want := range []string{"/kspec", "/kspec.md"} {
		if !strings.Contains(got, want) {
			t.Errorf(".gitignore missing %q; got:\n%s", want, got)
		}
	}
}

// TestInitDoesNotTouchGitignoreOnNo verifies declining leaves no .gitignore.
func TestInitDoesNotTouchGitignoreOnNo(t *testing.T) {
	dir := t.TempDir()
	withConfirmAnswer(t, "n\n", func() {
		if code, _, errOut := run(t, "init", "--root", dir); code != 0 {
			t.Fatalf("init failed (code=%d): %s", code, errOut)
		}
	})
	if got := readGitignore(t, dir); got != "" {
		t.Errorf("expected no .gitignore on decline, got:\n%s", got)
	}
}

// TestInitNonInteractiveSkipsGitignore confirms headless/agent runs (no injected
// reader, captured non-TTY stdout) never prompt and never write .gitignore.
func TestInitNonInteractiveSkipsGitignore(t *testing.T) {
	dir := t.TempDir()
	if code, _, errOut := run(t, "init", "--root", dir); code != 0 {
		t.Fatalf("init failed (code=%d): %s", code, errOut)
	}
	if got := readGitignore(t, dir); got != "" {
		t.Errorf("non-interactive init must not write .gitignore, got:\n%s", got)
	}
}

// TestEnsureGitignoreIsIdempotent appends once, then proves a second call adds
// nothing — neither anchored nor bare duplicates.
func TestEnsureGitignoreIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	added, err := ensureGitignore(dir, []string{"kspec", "kspec.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("first call should add 2 entries, got %v", added)
	}
	first := readGitignore(t, dir)

	added, err = ensureGitignore(dir, []string{"kspec", "kspec.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Errorf("second call should add nothing, got %v", added)
	}
	if got := readGitignore(t, dir); got != first {
		t.Errorf(".gitignore changed on idempotent call:\n%s", got)
	}
}

// TestEnsureGitignoreRespectsExistingBareEntry skips a name already ignored with
// no leading slash, so an existing `kspec` line is not duplicated as `/kspec`.
func TestEnsureGitignoreRespectsExistingBareEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("kspec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := ensureGitignore(dir, []string{"kspec", "kspec.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "/kspec.md" {
		t.Fatalf("expected only /kspec.md added, got %v", added)
	}
	if got := readGitignore(t, dir); strings.Contains(got, "/kspec\n") {
		t.Errorf("bare kspec entry was duplicated as /kspec:\n%s", got)
	}
}

// TestGitignoreTargetsOnlyExisting reports only the artifacts present at root.
func TestGitignoreTargetsOnlyExisting(t *testing.T) {
	dir := t.TempDir()
	if got := gitignoreTargets(dir); len(got) != 0 {
		t.Errorf("empty dir should yield no targets, got %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "kspec.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := gitignoreTargets(dir)
	if len(got) != 1 || got[0] != "kspec.md" {
		t.Errorf("expected [kspec.md], got %v", got)
	}
}
