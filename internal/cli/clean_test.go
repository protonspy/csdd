package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanRemovesOldBackups(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")

	// Two rounds of user edits + update produce -1.old and -2.old.
	if err := os.WriteFile(rule, []byte("EDIT ONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "update", "--root", dir); code != 0 {
		t.Fatal("first update failed")
	}
	if err := os.WriteFile(rule, []byte("EDIT TWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "update", "--root", dir); code != 0 {
		t.Fatal("second update failed")
	}
	if olds := oldBackups(t, dir); len(olds) != 2 {
		t.Fatalf("expected 2 .old backups before clean, got %v", olds)
	}

	code, out, _ := run(t, "clean", "--root", dir)
	if code != 0 {
		t.Fatalf("clean failed: code=%d\n%s", code, out)
	}
	if !strings.Contains(out, "Cleaned 2") {
		t.Errorf("clean should report removals:\n%s", out)
	}
	if olds := oldBackups(t, dir); len(olds) != 0 {
		t.Errorf(".old backups should be gone after clean: %v", olds)
	}
}

func TestCleanDryRunKeepsBackups(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("EDIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "update", "--root", dir); code != 0 {
		t.Fatal("update failed")
	}
	code, out, _ := run(t, "clean", "--dry-run", "--root", dir)
	if code != 0 {
		t.Fatalf("clean --dry-run failed: %d", code)
	}
	if !strings.Contains(out, "would remove") {
		t.Errorf("dry-run should list backups:\n%s", out)
	}
	if olds := oldBackups(t, dir); len(olds) != 1 {
		t.Errorf("dry-run must keep the backup: %v", olds)
	}
}

func TestCleanNoBackups(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, _ := run(t, "clean", "--root", dir)
	if code != 0 {
		t.Fatalf("clean on a clean workspace failed: %d", code)
	}
	if !strings.Contains(out, "no .old backups") {
		t.Errorf("expected a no-op message:\n%s", out)
	}
}
