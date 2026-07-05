package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpecDeleteRejectsTraversal is the regression for the highest-severity
// finding: `csdd spec delete .. --force` must not RemoveAll the workspace root.
func TestSpecDeleteRejectsTraversal(t *testing.T) {
	dir := freshWorkspace(t)
	sentinel := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("precondition: CLAUDE.md should exist: %v", err)
	}
	for _, bad := range []string{"..", "../..", "../src"} {
		code, _, errOut := run(t, "spec", "delete", bad, "--force", "--root", dir)
		if code == 0 {
			t.Errorf("spec delete %q should fail, got code 0", bad)
		}
		if !strings.Contains(errOut, "invalid") {
			t.Errorf("spec delete %q should report an invalid name, got %q", bad, errOut)
		}
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("workspace was damaged by a traversal delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); err != nil {
		t.Fatalf(".claude/ was damaged by a traversal delete: %v", err)
	}
}

func TestSkillDeleteRejectsTraversal(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "skill", "delete", "..", "--force", "--root", dir)
	if code == 0 {
		t.Error("skill delete .. should not succeed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills")); err != nil {
		t.Fatalf(".claude/skills was damaged: %v", err)
	}
}

func TestSteeringDeleteRejectsTraversal(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "steering", "delete", "../../CLAUDE", "--force", "--root", dir)
	if code == 0 {
		t.Error("steering delete ../../CLAUDE should not succeed")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md was deleted via steering traversal: %v", err)
	}
}

func TestInitRejectsStrayPositional(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := run(t, "init", "somedir", "--root", dir)
	if code == 0 {
		t.Error("`csdd init somedir` should fail (no positional args), not silently scaffold cwd")
	}
	if !strings.Contains(errOut, "positional") {
		t.Errorf("expected a positional-args error, got %q", errOut)
	}
}

func TestSpecValidateJSON(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "spec", "init", "photo-albums", "--root", dir); code != 0 {
		t.Fatalf("spec init failed: %s", e)
	}
	if code, _, e := run(t, "spec", "generate", "photo-albums", "--artifact", "requirements", "--root", dir); code != 0 {
		t.Fatalf("generate failed: %s", e)
	}
	code, out, _ := run(t, "spec", "validate", "photo-albums", "--json", "--root", dir)
	_ = code
	var payload validationJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("validate --json did not emit valid JSON: %v\noutput=%q", err, out)
	}
	if payload.Target != "photo-albums" {
		t.Errorf("json target = %q, want photo-albums", payload.Target)
	}
}

func TestSpecListJSON(t *testing.T) {
	dir := freshWorkspace(t)
	run(t, "spec", "init", "alpha", "--root", dir)
	code, out, _ := run(t, "spec", "list", "--json", "--root", dir)
	if code != 0 {
		t.Fatalf("spec list --json failed: %d", code)
	}
	var rows []specSummaryJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("spec list --json invalid: %v\n%q", err, out)
	}
	if len(rows) != 1 || rows[0].Feature != "alpha" {
		t.Errorf("unexpected list payload: %+v", rows)
	}
}

// TestApprovalDriftDetected covers the approval-gate content binding: editing an
// approved artifact after approval must surface a drift issue.
func TestApprovalDriftDetected(t *testing.T) {
	dir := freshWorkspace(t)
	run(t, "spec", "init", "feat", "--root", dir)
	if code, _, e := run(t, "spec", "generate", "feat", "--artifact", "requirements", "--root", dir); code != 0 {
		t.Fatalf("generate failed: %s", e)
	}
	// Overwrite requirements.md with a valid EARS criterion so approval passes.
	reqPath := filepath.Join(dir, "specs", "feat", "requirements.md")
	valid := "# Requirements\n\n### Requirement 1: R\n\n**Acceptance Criteria**\n1. WHEN a user acts THE SYSTEM SHALL respond.\n"
	if err := os.WriteFile(reqPath, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := run(t, "spec", "approve", "feat", "--phase", "requirements", "--root", dir); code != 0 {
		t.Fatalf("approve failed: %s", e)
	}
	// No drift right after approval.
	if code, _, _ := run(t, "spec", "validate", "feat", "--root", dir); code != 0 {
		t.Errorf("validate should pass immediately after approval, got %d", code)
	}
	// Edit the approved artifact → drift.
	if err := os.WriteFile(reqPath, []byte(valid+"\n2. WHEN b THE SYSTEM SHALL c.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "spec", "validate", "feat", "--root", dir)
	combined := out + errOut
	if code == 0 || !strings.Contains(combined, "after approval") {
		t.Errorf("edited-after-approval drift not reported: code=%d out=%q", code, combined)
	}
}
