package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// destroyTargetRels are the workspace-relative paths `csdd destroy` must remove.
// specs/ is deliberately absent — it is the one tree destroy preserves.
var destroyTargetRels = []string{
	".claude",
	"CLAUDE.md",
	".mcp.json",
	filepath.Join(".githooks", "pre-push"),
}

func TestDestroyRemovesWorkspaceKeepsSpecs(t *testing.T) {
	dir := freshWorkspace(t)

	// A real spec the user authored — destroy must not touch it.
	specFile := filepath.Join(dir, "specs", "photo-albums", "requirements.md")
	if err := os.MkdirAll(filepath.Dir(specFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specFile, []byte("# my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "destroy", "--force", "--root", dir)
	if code != 0 {
		t.Fatalf("destroy failed: code=%d\nout=%s\nerr=%s", code, out, errOut)
	}
	if !strings.Contains(out, "Destroyed csdd workspace") {
		t.Errorf("expected a success summary:\n%s", out)
	}

	for _, rel := range destroyTargetRels {
		if pathExists(filepath.Join(dir, rel)) {
			t.Errorf("%s should have been removed", rel)
		}
	}
	if !pathExists(specFile) {
		t.Errorf("specs/ work must be preserved, but %s is gone", specFile)
	}
}

func TestDestroyAlwaysAlertsAndIsIrreversibleInMessage(t *testing.T) {
	dir := freshWorkspace(t)
	// The alert is emitted to stderr (render.Warn); the file list to stdout.
	_, out, errOut := run(t, "destroy", "--force", "--root", dir)
	combined := out + errOut
	if !strings.Contains(combined, "DESTRUCTIVE") || !strings.Contains(combined, "cannot be undone") {
		t.Errorf("destroy must show a destructive-action alert:\nstdout=%s\nstderr=%s", out, errOut)
	}
	if !strings.Contains(combined, "CLAUDE.md and .mcp.json are deleted in full") {
		t.Errorf("destroy must warn that shared files are deleted whole:\n%s", combined)
	}
}

func TestDestroyDryRunDeletesNothing(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, _ := run(t, "destroy", "--dry-run", "--root", dir)
	if code != 0 {
		t.Fatalf("destroy --dry-run failed: %d", code)
	}
	if !strings.Contains(out, "Dry run") {
		t.Errorf("dry-run should announce itself:\n%s", out)
	}
	for _, rel := range destroyTargetRels {
		if !pathExists(filepath.Join(dir, rel)) {
			t.Errorf("dry-run must keep %s on disk", rel)
		}
	}
}

// Without --force and with no interactive TTY, destroy must refuse rather than
// silently wipe the workspace — the safe default for headless/agent runs.
func TestDestroyWithoutForceAbortsNonInteractive(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "destroy", "--root", dir)
	if code == 0 {
		t.Fatalf("destroy without --force should abort non-interactively")
	}
	if !strings.Contains(errOut, "aborted") {
		t.Errorf("expected an abort message:\n%s", errOut)
	}
	if !pathExists(filepath.Join(dir, ".claude")) {
		t.Errorf(".claude must still exist after an aborted destroy")
	}
}

func TestDestroyNotAWorkspace(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := run(t, "destroy", "--force", "--root", dir)
	if code == 0 {
		t.Fatalf("destroy on a non-workspace should fail")
	}
	if !strings.Contains(errOut, "not a csdd workspace") {
		t.Errorf("expected a not-a-workspace error:\n%s", errOut)
	}
}

func TestDestroyConfirmYesProceeds(t *testing.T) {
	dir := freshWorkspace(t)
	restore := confirmStdin
	confirmStdin = strings.NewReader("y\n")
	defer func() { confirmStdin = restore }()

	code, out, _ := run(t, "destroy", "--root", dir)
	if code != 0 {
		t.Fatalf("destroy with a 'y' answer should proceed: %d\n%s", code, out)
	}
	if pathExists(filepath.Join(dir, ".claude")) {
		t.Errorf(".claude should be gone after a confirmed destroy")
	}
}
