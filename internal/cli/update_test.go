package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/protonspy/csdd/internal/manifest"
	"github.com/protonspy/csdd/internal/paths"
)

// oldBackups returns every *.old file under root (the user-edit backups update
// creates). A safe operation on untouched files must leave this empty.
func oldBackups(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".old") {
			found = append(found, p)
		}
		return nil
	})
	return found
}

func TestUpdateNoOpOnFreshWorkspace(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, _ := run(t, "update", "--root", dir)
	if code != 0 {
		t.Fatalf("update on fresh workspace failed: code=%d", code)
	}
	if !strings.Contains(out, "0 conflict(s)") || !strings.Contains(out, "unchanged") {
		t.Errorf("fresh update should be a no-op:\n%s", out)
	}
	if olds := oldBackups(t, dir); len(olds) != 0 {
		t.Errorf("no .old backups should be created on a fresh update: %v", olds)
	}
}

func TestUpdateReaddsMissingShippedFile(t *testing.T) {
	dir := freshWorkspace(t)
	if err := os.RemoveAll(filepath.Join(dir, ".claude", "skills", "tdd-cycle")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "update", "--root", dir)
	if code != 0 {
		t.Fatalf("update failed: code=%d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "tdd-cycle", "SKILL.md")); err != nil {
		t.Errorf("update did not re-add the missing shipped skill: %v", err)
	}
	if !strings.Contains(out, "add") {
		t.Errorf("output should report an add:\n%s", out)
	}
}

func TestUpdatePreservesUserEditAsOld(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("USER EDIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "update", "--root", dir)
	if code != 0 {
		t.Fatalf("update failed: code=%d\n%s", code, out)
	}
	if got := readFile(t, rule); got == "USER EDIT\n" {
		t.Error("the shipped version should have been written in place")
	}
	backup := rule + "-1.old"
	if got := readFile(t, backup); got != "USER EDIT\n" {
		t.Errorf("the user's edit must be preserved in %s, got %q", backup, got)
	}
	if !strings.Contains(out, "1 conflict(s)") {
		t.Errorf("a conflict should be reported:\n%s", out)
	}
}

func TestUpdatePristineOutdatedUpdatesInPlace(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	relKey := ".claude/rules/ears-format.md"

	// Simulate a prior csdd version: the on-disk file and the recorded baseline
	// both hold the OLD content (the user never edited it).
	if err := os.WriteFile(rule, []byte("OLD SHIPPED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _, err := manifest.Load(paths.Manifest(dir))
	if err != nil {
		t.Fatal(err)
	}
	m.Files[relKey] = manifest.Hash("OLD SHIPPED\n")
	if err := m.Save(paths.Manifest(dir), "test", time.Now()); err != nil {
		t.Fatal(err)
	}

	code, out, _ := run(t, "update", "--root", dir)
	if code != 0 {
		t.Fatalf("update failed: code=%d\n%s", code, out)
	}
	if got := readFile(t, rule); got == "OLD SHIPPED\n" || !strings.Contains(got, "EARS") {
		t.Errorf("pristine outdated file should be refreshed to the shipped version, got %q", got)
	}
	if _, err := os.Stat(rule + "-1.old"); !os.IsNotExist(err) {
		t.Error("a pristine (unedited) file must be updated WITHOUT a .old backup")
	}
	if !strings.Contains(out, "1 updated") {
		t.Errorf("output should report 1 updated:\n%s", out)
	}
}

func TestUpdateForceSkipsBackup(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("USER EDIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "update", "--force", "--root", dir); code != 0 {
		t.Fatal("forced update failed")
	}
	if got := readFile(t, rule); got == "USER EDIT\n" {
		t.Error("--force should overwrite the edited file")
	}
	if olds := oldBackups(t, dir); len(olds) != 0 {
		t.Errorf("--force must not create .old backups: %v", olds)
	}
}

func TestUpdateDryRunWritesNothing(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("USER EDIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "update", "--dry-run", "--root", dir)
	if code != 0 {
		t.Fatalf("dry-run failed: code=%d", code)
	}
	if !strings.Contains(out, "Dry run") || !strings.Contains(out, "No files were written") {
		t.Errorf("dry-run should announce itself:\n%s", out)
	}
	if got := readFile(t, rule); got != "USER EDIT\n" {
		t.Error("dry-run must not modify the file")
	}
	if olds := oldBackups(t, dir); len(olds) != 0 {
		t.Errorf("dry-run must not create .old backups: %v", olds)
	}
}

func TestUpdateNeverTouchesUserOwnedFiles(t *testing.T) {
	dir := freshWorkspace(t)

	// User-owned artifacts that update must never touch.
	steering := filepath.Join(dir, ".claude", "steering", "product.md")
	if err := os.WriteFile(steering, []byte("MY PRODUCT NOTES\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "spec", "init", "myfeat", "--root", dir); code != 0 {
		t.Fatal("spec init failed")
	}
	if code, _, _ := run(t, "skill", "create", "my-skill", "--description", "Custom skill.", "--root", dir); code != 0 {
		t.Fatal("skill create failed")
	}
	if code, _, _ := run(t, "mcp", "add", "myserver", "--command", "echo", "--root", dir); code != 0 {
		t.Fatal("mcp add failed")
	}

	guarded := map[string]string{}
	for _, rel := range []string{
		".claude/steering/product.md",
		"CLAUDE.md",
		".mcp.json",
		".claude/settings.json",
		"specs/myfeat/spec.json",
		".claude/skills/my-skill/SKILL.md",
	} {
		guarded[rel] = readFile(t, filepath.Join(dir, filepath.FromSlash(rel)))
	}

	if code, _, _ := run(t, "update", "--root", dir); code != 0 {
		t.Fatal("update failed")
	}

	for rel, want := range guarded {
		if got := readFile(t, filepath.Join(dir, filepath.FromSlash(rel))); got != want {
			t.Errorf("update modified user-owned file %s", rel)
		}
	}
	if olds := oldBackups(t, dir); len(olds) != 0 {
		t.Errorf("update should not have created any .old backups: %v", olds)
	}
}

func TestUpdateRequiresWorkspace(t *testing.T) {
	bare := t.TempDir()
	code, _, errOut := run(t, "update", "--root", bare)
	if code == 0 || !strings.Contains(errOut, "not a csdd workspace") {
		t.Errorf("update outside a workspace should fail: code=%d err=%q", code, errOut)
	}
}

func TestUpdateOldCounterIncrements(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")

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

	if got := readFile(t, rule+"-1.old"); got != "EDIT ONE\n" {
		t.Errorf("-1.old should hold the first edit, got %q", got)
	}
	if got := readFile(t, rule+"-2.old"); got != "EDIT TWO\n" {
		t.Errorf("-2.old should hold the second edit, got %q", got)
	}
}

func TestUpdateConfirmDeclineSkipsOverride(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("USER EDIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	confirmStdin = strings.NewReader("n\n") // interactive "no"
	defer func() { confirmStdin = nil }()

	code, out, _ := run(t, "update", "--root", dir)
	if code != 0 {
		t.Fatalf("update failed: code=%d\n%s", code, out)
	}
	if got := readFile(t, rule); got != "USER EDIT\n" {
		t.Errorf("declining the prompt must keep the user's file, got %q", got)
	}
	if olds := oldBackups(t, dir); len(olds) != 0 {
		t.Errorf("declining must not create .old backups: %v", olds)
	}
	if !strings.Contains(out, "skip") {
		t.Errorf("a skipped override should be reported:\n%s", out)
	}
}

func TestUpdateConfirmAcceptOverrides(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("USER EDIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	confirmStdin = strings.NewReader("y\n") // interactive "yes"
	defer func() { confirmStdin = nil }()

	code, out, _ := run(t, "update", "--root", dir)
	if code != 0 {
		t.Fatalf("update failed: code=%d\n%s", code, out)
	}
	if got := readFile(t, rule); got == "USER EDIT\n" {
		t.Error("accepting the prompt should write the shipped version in place")
	}
	if got := readFile(t, rule+"-1.old"); got != "USER EDIT\n" {
		t.Errorf("the user's edit must be preserved as .old")
	}
}

func TestUpdateYesSkipsPrompt(t *testing.T) {
	dir := freshWorkspace(t)
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("USER EDIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Even with an interactive stdin available, --yes must not prompt.
	confirmStdin = strings.NewReader("n\n")
	defer func() { confirmStdin = nil }()

	code, _, _ := run(t, "update", "--yes", "--root", dir)
	if code != 0 {
		t.Fatalf("update --yes failed: %d", code)
	}
	if got := readFile(t, rule); got == "USER EDIT\n" {
		t.Error("--yes should override without consulting the prompt")
	}
	if got := readFile(t, rule+"-1.old"); got != "USER EDIT\n" {
		t.Errorf("--yes still keeps the user's edit as .old")
	}
}

func TestInitWritesManifest(t *testing.T) {
	dir := freshWorkspace(t)
	m, exists, err := manifest.Load(paths.Manifest(dir))
	if err != nil || !exists {
		t.Fatalf("init should write a manifest: exists=%v err=%v", exists, err)
	}
	for _, key := range []string{
		".claude/rules/ears-format.md",
		".claude/skills/tdd-cycle/SKILL.md",
		".claude/agents/wf-development.md",
	} {
		if _, ok := m.Files[key]; !ok {
			t.Errorf("manifest missing managed file %q", key)
		}
	}
}
