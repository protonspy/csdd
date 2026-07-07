package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scaffoldApprovedPlan lays down an authored plan with one seed, then approves it,
// returning the workspace root. The seed is written BEFORE approval so the
// approval hash covers it (adding it after would register as drift).
func scaffoldApprovedPlan(t *testing.T) string {
	t.Helper()
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan init failed: %s", e)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "plans", "photos", "plan.md"), []byte(authoredPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	// A trivial (EARS-clean) seed for upload, written BEFORE approval so the hash
	// covers it.
	seed := filepath.Join(dir, "docs", "plans", "photos", "seeds", "upload", "requirements.md")
	if err := os.MkdirAll(filepath.Dir(seed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seed, []byte("# Requirements\n\nSeeded intent.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, e := run(t, "plan", "approve", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan approve failed (code=%d): %s / %s", code, out, e)
	}
	return dir
}

func TestPlanApproveWritesJSON(t *testing.T) {
	dir := scaffoldApprovedPlan(t)
	data, err := os.ReadFile(filepath.Join(dir, "docs", "plans", "photos", "plan.json"))
	if err != nil {
		t.Fatalf("plan.json not written: %v", err)
	}
	var pj struct {
		Approvals struct {
			Approved    bool   `json:"approved"`
			ContentHash string `json:"content_hash"`
		} `json:"approvals"`
	}
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatal(err)
	}
	if !pj.Approvals.Approved || pj.Approvals.ContentHash == "" {
		t.Errorf("plan.json should record approved+hash, got %+v", pj.Approvals)
	}
}

func TestPlanGenerateProvenanceAndSeed(t *testing.T) {
	dir := scaffoldApprovedPlan(t)
	code, out, errOut := run(t, "plan", "generate", "photos", "upload", "--root", dir)
	if code != 0 {
		t.Fatalf("plan generate failed (code=%d): %s / %s", code, out, errOut)
	}
	// The spec exists with plan provenance.
	specJSON, err := os.ReadFile(filepath.Join(dir, "specs", "upload", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(specJSON), `"plan": "photos"`) {
		t.Errorf("spec.json missing plan provenance: %s", specJSON)
	}
	// The seed was copied into the spec and marked generated.
	if _, err := os.Stat(filepath.Join(dir, "specs", "upload", "requirements.md")); err != nil {
		t.Errorf("seed requirements.md not copied into the spec: %v", err)
	}
	if !strings.Contains(string(specJSON), `"generated": true`) {
		t.Errorf("seeded phase should be marked generated: %s", specJSON)
	}
}

func TestPlanNextAndBriefCLI(t *testing.T) {
	dir := scaffoldApprovedPlan(t)
	if code, _, e := run(t, "plan", "generate", "photos", "upload", "--root", dir); code != 0 {
		t.Fatalf("generate failed: %s", e)
	}
	// upload now has a generated (unapproved) requirements → next is its requirements step.
	code, out, _ := run(t, "plan", "next", "photos", "--root", dir, "--json")
	if code != 0 {
		t.Fatalf("plan next exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `"feat": "upload"`) || !strings.Contains(out, `"step": "spec-requirements"`) {
		t.Errorf("unexpected next step: %s", out)
	}
	// brief prints a context pack for that step.
	code, out, errOut := run(t, "plan", "brief", "photos", "--root", dir)
	if code != 0 {
		t.Fatalf("plan brief failed (code=%d): %s", code, errOut)
	}
	if !strings.Contains(out, "Brief — photos / upload") || !strings.Contains(out, "Forbidden actions") {
		t.Errorf("brief output incomplete: %s", out)
	}
}

func TestPlanGenerateRequireApprovedFails(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("init failed: %s", e)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "plans", "photos", "plan.md"), []byte(authoredPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not approved: --require-approved must refuse.
	code, _, errOut := run(t, "plan", "generate", "photos", "upload", "--root", dir, "--require-approved")
	if code == 0 {
		t.Errorf("generate --require-approved should fail on an unapproved plan")
	}
	if !strings.Contains(errOut, "not approved") {
		t.Errorf("expected not-approved error, got %q", errOut)
	}
}

func TestPlanRunPreflightCLI(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("init failed: %s", e)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "plans", "photos", "plan.md"), []byte(authoredPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unapproved plan: run refuses before ever spawning a session.
	code, _, errOut := run(t, "plan", "run", "photos", "--root", dir)
	if code == 0 {
		t.Errorf("plan run should refuse an unapproved plan")
	}
	if !strings.Contains(errOut, "not approved") {
		t.Errorf("expected not-approved preflight error, got %q", errOut)
	}
}

func TestPlanNextDriftExitCode(t *testing.T) {
	dir := scaffoldApprovedPlan(t)
	// Edit plan.md after approval → drift → next exits 5.
	planMD := filepath.Join(dir, "docs", "plans", "photos", "plan.md")
	if err := os.WriteFile(planMD, []byte(authoredPlan+"\n<!-- drift -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ := run(t, "plan", "next", "photos", "--root", dir)
	if code != 5 {
		t.Errorf("drifted plan next should exit 5, got %d", code)
	}
}
