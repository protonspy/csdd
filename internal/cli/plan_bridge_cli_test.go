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
	// upload is the only feat and is not yet delivered → next hands out the feat.
	code, out, _ := run(t, "plan", "next", "photos", "--root", dir, "--json")
	if code != 0 {
		t.Fatalf("plan next exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `"feat": "upload"`) {
		t.Errorf("unexpected next feat: %s", out)
	}
	// brief prints the feat's mission pack: the feat itself, what governs it and the
	// plan's gates — the development process lives in the worktree's CLAUDE.md.
	// `--enrich-model none` because the context pass now runs by DEFAULT: without it
	// this test would spawn a real `claude` on any machine that has one installed.
	code, out, errOut := run(t, "plan", "brief", "photos", "--root", dir, "--enrich-model", "none")
	if code != 0 {
		t.Fatalf("plan brief failed (code=%d): %s", code, errOut)
	}
	for _, want := range []string{"# Feat: upload — plan photos", "Objective:", "Quality gates for this plan"} {
		if !strings.Contains(out, want) {
			t.Errorf("brief output is missing %q: %s", want, out)
		}
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

// TestPlanBriefNamesAMissingContextPack pins the notice that replaced a silent
// omission.
//
// The discovered half of a brief — what the feat touches, what governs it, what is
// already there — is simply absent when no context pack has been stored, and the
// brief used to render without it and without a word. That is indistinguishable
// from a pass that ran and found nothing, and it is exactly how a reader concludes
// the enrichment is broken. The pass runs by default now, so the only way to reach
// that state is to turn it off — and then it has to say so.
func TestPlanBriefNamesAMissingContextPack(t *testing.T) {
	dir := scaffoldApprovedPlan(t)
	code, _, errOut := run(t, "plan", "brief", "photos", "--root", dir, "--enrich-model", "none")
	if code != 0 {
		t.Fatalf("plan brief exit=%d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "no stored context pack") {
		t.Errorf("a brief with no pack and the pass switched off must say so on stderr:\n%s", errOut)
	}
}

// TestPlanBriefRefreshRefusesWithNoModel keeps --refresh honest: it forces the pass
// to run, so asking for it with the pass turned off is a contradiction the CLI must
// reject rather than silently print a stale brief for.
func TestPlanBriefRefreshRefusesWithNoModel(t *testing.T) {
	dir := scaffoldApprovedPlan(t)
	code, _, errOut := run(t, "plan", "brief", "photos", "--root", dir, "--refresh", "--enrich-model", "none")
	if code != 1 {
		t.Fatalf("--refresh with no model should exit 1, got %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "--refresh needs a model") {
		t.Errorf("the refusal should name the flag conflict:\n%s", errOut)
	}
}

// TestPlanBriefKeepsAStoredPackEvenWhenStale pins the reuse rule that separates
// `plan brief` from the runner: a pack on disk is what the human gets, whether or
// not the feat's row has changed since it was written.
//
// Regenerating is a model call, and briefing is what a human does over and over
// while editing a plan — invalidating on every edit turns reading a brief into a
// recurring charge. So the staleness is REPORTED and the pack is still used;
// `--refresh` is the way to replace it. The pass is switched off here so the test
// spawns nothing: with a pack present it would not run either way, which is the
// property under test.
func TestPlanBriefKeepsAStoredPackEvenWhenStale(t *testing.T) {
	dir := scaffoldApprovedPlan(t)
	packs := filepath.Join(dir, ".csdd", "plan", "photos", "briefs")
	if err := os.MkdirAll(packs, 0o755); err != nil {
		t.Fatal(err)
	}
	// `key` is deliberately not the current plan row's key: this pack is stale.
	pack := `{"touches":[{"path":"app/upload.go","why":"the upload handler lives here"}],"key":"stale"}`
	if err := os.WriteFile(filepath.Join(packs, "upload.json"), []byte(pack), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "plan", "brief", "photos", "--root", dir, "--enrich-model", "none")
	if code != 0 {
		t.Fatalf("plan brief exit=%d: %s", code, errOut)
	}
	if !strings.Contains(out, "the upload handler lives here") {
		t.Errorf("the stored pack must be rendered into the brief:\n%s", out)
	}
	if !strings.Contains(errOut, "predates the current plan row") {
		t.Errorf("a stale pack must be reported, not silently reused:\n%s", errOut)
	}
}
