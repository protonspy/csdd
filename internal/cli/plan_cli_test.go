package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const authoredPlan = `---
name: photos
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest photos | — | M1 | | |
| 2 | thumbs | Thumbnails | upload | M1 | P | |

## Quality Gates

- verify: make check
`

func TestPlanInitScaffold(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, errOut := run(t, "plan", "init", "photos", "--root", dir)
	if code != 0 {
		t.Fatalf("plan init failed (code=%d): out=%s err=%s", code, out, errOut)
	}
	planMD := filepath.Join(dir, "docs", "plans", "photos", "plan.md")
	if _, err := os.Stat(planMD); err != nil {
		t.Fatalf("plan.md not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "plans", "photos", "seeds")); err != nil {
		t.Errorf("seeds/ not created: %v", err)
	}
	// The scaffold has all sections but no authored content, so it must not
	// validate yet (no feats, no gates).
	code, _, _ = run(t, "plan", "validate", "photos", "--root", dir)
	if code != 2 {
		t.Errorf("fresh scaffold should fail validate with exit 2, got %d", code)
	}
}

func TestPlanInitIdempotentAndForce(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("first init failed: %s", e)
	}
	planMD := filepath.Join(dir, "docs", "plans", "photos", "plan.md")
	if err := os.WriteFile(planMD, []byte(authoredPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	// A second init must not clobber the authored content.
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("second init failed: %s", e)
	}
	data, _ := os.ReadFile(planMD)
	if !strings.Contains(string(data), "Ingest photos") {
		t.Errorf("idempotent init overwrote authored plan.md")
	}
	// --force restores the template.
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir, "--force"); code != 0 {
		t.Fatalf("forced init failed: %s", e)
	}
	data, _ = os.ReadFile(planMD)
	if strings.Contains(string(data), "Ingest photos") {
		t.Errorf("--force should have overwritten the authored plan.md")
	}
}

func TestPlanInitRequiresWorkspace(t *testing.T) {
	bare := t.TempDir() // no `csdd init`, so no .csdd/ marker
	code, _, errOut := run(t, "plan", "init", "photos", "--root", bare)
	if code == 0 {
		t.Fatal("plan init should fail outside a csdd workspace")
	}
	if !strings.Contains(errOut, "not a csdd workspace") {
		t.Errorf("expected workspace guidance, got %q", errOut)
	}
}

func TestPlanValidateAndStatus(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("init failed: %s", e)
	}
	planMD := filepath.Join(dir, "docs", "plans", "photos", "plan.md")
	if err := os.WriteFile(planMD, []byte(authoredPlan), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "plan", "validate", "photos", "--root", dir)
	if code != 0 {
		t.Fatalf("authored plan should validate (code=%d): out=%s err=%s", code, out, errOut)
	}

	// status --json exposes the derived per-feat state (both feats pending — no
	// specs exist yet).
	code, out, _ = run(t, "plan", "status", "photos", "--root", dir, "--json")
	if code != 0 {
		t.Fatalf("status --json failed: %d", code)
	}
	if !strings.Contains(out, `"feat": "upload"`) || !strings.Contains(out, `"state": "pending"`) {
		t.Errorf("status JSON missing derived feats: %s", out)
	}

	// Human status renders the plan header and feat table.
	code, out, _ = run(t, "plan", "status", "photos", "--root", dir)
	if code != 0 || !strings.Contains(out, "plan: photos") || !strings.Contains(out, "thumbs") {
		t.Errorf("human status incomplete (code=%d): %s", code, out)
	}
}

func TestPlanValidateJSONExitCode(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("init failed: %s", e)
	}
	// The unauthored scaffold has findings; --json must still exit 2.
	code, out, _ := run(t, "plan", "validate", "photos", "--root", dir, "--json")
	if code != 2 {
		t.Errorf("validate --json should exit 2 on findings, got %d", code)
	}
	if !strings.Contains(out, `"ok": false`) {
		t.Errorf("validate --json should report ok=false: %s", out)
	}
}

// TestPlanRunSquadLimitBounds pins the flag's contract. The ceiling is not
// arbitrary: 6 is the widest topological wave the evidence plan admits, so past it a
// plan's own Depends graph cannot supply the parallelism and a larger number would
// only consume the shared Claude account limit faster.
func TestPlanRunSquadLimitBounds(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"7", "99", "-2"} {
		code, _, errOut := run(t, "plan", "run", "photos", "--root", dir, "--squad-limit", n)
		if code == 0 {
			t.Errorf("--squad-limit %s should be rejected", n)
		}
		if !strings.Contains(errOut, "must be between 1 and 6") {
			t.Errorf("--squad-limit %s should name the bound, got %q", n, errOut)
		}
	}
}

// TestMisspelledFlagNamesItsNeighbour is a usability regression from a real report:
// `--squard-limit` produced only "flag provided but not defined", which reads as
// "that option does not exist" and got the whole capability written off as missing.
func TestMisspelledFlagNamesItsNeighbour(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := run(t, "plan", "run", "photos", "--root", dir, "--squard-limit", "2")
	if code == 0 {
		t.Errorf("an undefined flag must still fail")
	}
	if !strings.Contains(errOut, "did you mean --squad-limit?") {
		t.Errorf("a one-transposition typo should name the flag it meant, got %q", errOut)
	}

	// A name nothing is close to gets the stock message, not a guess.
	_, _, errOut = run(t, "plan", "run", "photos", "--root", dir, "--wildly-unrelated", "2")
	if strings.Contains(errOut, "did you mean") {
		t.Errorf("a distant name must not be corrected into something else, got %q", errOut)
	}
}

// TestPlanListEmpty: a workspace with no plans is a normal state, not an error —
// the command exits 0 and points at how to create one.
func TestPlanListEmpty(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, errOut := run(t, "plan", "list", "--root", dir)
	if code != 0 {
		t.Fatalf("plan list on an empty workspace should exit 0, got %d (%s)", code, errOut)
	}
	if !strings.Contains(out+errOut, "no plans found") {
		t.Errorf("expected the empty-state hint, got out=%q err=%q", out, errOut)
	}
	// --json must still emit a parseable empty array, never `null`: an agent
	// iterating the result must not have to special-case "no plans".
	code, out, _ = run(t, "plan", "list", "--root", dir, "--json")
	if code != 0 {
		t.Fatalf("plan list --json failed: %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("plan list --json is not valid JSON: %v (%s)", err, out)
	}
	if len(rows) != 0 {
		t.Errorf("expected zero rows, got %d", len(rows))
	}
}

// TestPlanListRowsAndJSON covers the whole-workspace view: every plan, its
// approval state, and delivered/total feats.
func TestPlanListRowsAndJSON(t *testing.T) {
	dir := freshWorkspace(t)
	for _, slug := range []string{"photos", "albums"} {
		if code, _, e := run(t, "plan", "init", slug, "--root", dir); code != 0 {
			t.Fatalf("init %s failed: %s", slug, e)
		}
	}
	// Only `photos` is authored+approved; `albums` stays the unauthored scaffold,
	// so the listing has to show both an approved plan and a draft one.
	planMD := filepath.Join(dir, "docs", "plans", "photos", "plan.md")
	if err := os.WriteFile(planMD, []byte(authoredPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := run(t, "plan", "approve", "photos", "--root", dir); code != 0 {
		t.Fatalf("approve failed: %s", e)
	}

	code, out, errOut := run(t, "plan", "list", "--root", dir)
	if code != 0 {
		t.Fatalf("plan list failed (code=%d): %s", code, errOut)
	}
	for _, want := range []string{"photos", "albums", "approved", "draft", "0/2"} {
		if !strings.Contains(out, want) {
			t.Errorf("human listing missing %q: %s", want, out)
		}
	}

	code, out, _ = run(t, "plan", "list", "--root", dir, "--json")
	if code != 0 {
		t.Fatalf("plan list --json failed: %d", code)
	}
	var rows []struct {
		Plan     string `json:"plan"`
		Approved bool   `json:"approved"`
		Drift    bool   `json:"drift"`
		Feats    int    `json:"feats"`
		Done     int    `json:"done"`
		Complete bool   `json:"complete"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("plan list --json is not valid JSON: %v (%s)", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 plans, got %d: %s", len(rows), out)
	}
	// List order is lexical, so `albums` sorts ahead of `photos`.
	if rows[0].Plan != "albums" || rows[1].Plan != "photos" {
		t.Errorf("rows are not in slug order: %s", out)
	}
	if rows[0].Approved {
		t.Errorf("the unapproved scaffold must not read as approved: %s", out)
	}
	photos := rows[1]
	if !photos.Approved || photos.Drift {
		t.Errorf("photos should be approved and drift-free: %+v", photos)
	}
	if photos.Feats != 2 || photos.Done != 0 {
		t.Errorf("photos should show 0/2 feats, got %d/%d", photos.Done, photos.Feats)
	}
	// An unstarted plan is not complete, and neither is one with no feats at all.
	if photos.Complete || rows[0].Complete {
		t.Errorf("no plan here is complete: %s", out)
	}
}

// TestPlanListShowsDrift: an approval that no longer binds the current plan.md is
// what `plan run` refuses on, so the listing must not report it as plain
// "approved".
func TestPlanListShowsDrift(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("init failed: %s", e)
	}
	planMD := filepath.Join(dir, "docs", "plans", "photos", "plan.md")
	if err := os.WriteFile(planMD, []byte(authoredPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := run(t, "plan", "approve", "photos", "--root", dir); code != 0 {
		t.Fatalf("approve failed: %s", e)
	}
	drifted := authoredPlan + "\n- lint: make lint\n"
	if err := os.WriteFile(planMD, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "plan", "list", "--root", dir)
	if code != 0 {
		t.Fatalf("plan list failed: %s", errOut)
	}
	if !strings.Contains(out, "drift") {
		t.Errorf("the listing should flag the drifted approval: %s", out)
	}
	if !strings.Contains(out+errOut, "re-approve before running") {
		t.Errorf("drift should carry the fix: out=%q err=%q", out, errOut)
	}

	code, out, _ = run(t, "plan", "list", "--root", dir, "--json")
	if code != 0 {
		t.Fatalf("plan list --json failed: %d", code)
	}
	if !strings.Contains(out, `"drift": true`) {
		t.Errorf("JSON should carry drift=true: %s", out)
	}
}

// TestPlanListRejectsUnknownFlag keeps the action on the same flag discipline as
// the rest of the CLI.
func TestPlanListRejectsUnknownFlag(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "plan", "list", "--nope", "--root", dir); code == 0 {
		t.Error("an undefined flag must fail")
	}
}
