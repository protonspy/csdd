package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/protonspy/csdd/internal/graph"
	"github.com/protonspy/csdd/internal/plan"
)

// e2ePlan is a minimal one-feat plan so the golden path drives a single feat all
// the way to done without the full spec-authoring gauntlet (covered elsewhere).
const e2ePlan = `---
name: photos
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest photos | — | M1 | | |

## Quality Gates

- verify: make check
`

// mustWrite writes content under an ensured parent directory.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPlanRunE2EGoldenPath exercises the whole plan-mode surface end to end:
// init → author → validate → approve → generate → (stub) run one feat to done,
// then asserts the three read surfaces — CLI status, the graph, and the web
// read-model — all agree on the outcome.
func TestPlanRunE2EGoldenPath(t *testing.T) {
	dir := freshWorkspace(t)

	// 1. Author + approve the plan through the real CLI.
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan init: %s", e)
	}
	mustWrite(t, filepath.Join(dir, "docs", "plans", "photos", "plan.md"), e2ePlan)
	if code, _, e := run(t, "plan", "validate", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan validate should pass on an authored plan: %s", e)
	}
	if code, _, e := run(t, "plan", "approve", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan approve: %s", e)
	}

	// 2. Generate the feat's spec — this stamps plan provenance (spec.json plan=photos).
	if code, _, e := run(t, "plan", "generate", "photos", "upload", "--root", dir); code != 0 {
		t.Fatalf("plan generate: %s", e)
	}

	// 3. Simulate the spec having been authored + approved (that flow is tested on
	//    its own); leave one implementation task unchecked for the runner to close.
	specDir := filepath.Join(dir, "specs", "upload")
	mustWrite(t, filepath.Join(specDir, "spec.json"),
		`{"feature_name":"upload","plan":"photos","ready_for_implementation":true,`+
			`"approvals":{"requirements":{"generated":true,"approved":true},`+
			`"design":{"generated":true,"approved":true},`+
			`"tasks":{"generated":true,"approved":true}}}`)
	mustWrite(t, filepath.Join(specDir, "tasks.md"), "- [ ] 1. Implement upload\n")

	// 4. Run the plan with a stub runner (no real claude/git): the session authors
	//    the change, checks its task box, and declares done. The loop trusts the
	//    verdict, records the feat in the ledger, and advances. The runner never
	//    touches git — commit/branch/PR are the session's dev-cycle.
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	hooks := plan.Hooks{
		Session: func(plan.Feat, string, float64) (plan.Verdict, error) {
			mustWrite(t, filepath.Join(specDir, "tasks.md"), "- [x] 1. Implement upload\n")
			return plan.Verdict{Status: plan.VerdictDone}, nil
		},
		Doctor:          func() plan.SandboxReport { return plan.SandboxReport{OK: true} },
		ClaudeAvailable: func() bool { return true },
		Now:             func() time.Time { return now },
	}
	sum, err := plan.Run(plan.RunOptions{Root: dir, Slug: "photos", MaxIterations: 5, Out: io.Discard, Hooks: hooks})
	if err != nil {
		t.Fatalf("plan run: %v", err)
	}
	if !sum.Completed || sum.Outcome != plan.OutcomeComplete {
		t.Fatalf("expected a complete run, got %+v", sum)
	}

	// 5a. STATUS agrees: the feat is done, plan approved and drift-free.
	code, out, _ := run(t, "plan", "status", "photos", "--root", dir, "--json")
	if code != 0 {
		t.Fatalf("plan status --json exit %d", code)
	}
	var st plan.PlanStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &st); err != nil {
		t.Fatalf("decode status json: %v\n%s", err, out)
	}
	if len(st.Feats) != 1 || st.Feats[0].State != plan.StateDone {
		t.Errorf("status should report upload done, got %+v", st.Feats)
	}
	if !st.Approved || st.Drift {
		t.Errorf("plan should be approved and drift-free, got approved=%v drift=%v", st.Approved, st.Drift)
	}

	// 5b. GRAPH agrees: plan + feat nodes exist, the feat plans its (existing)
	//     spec, and upload is not flagged as an unplanned spec.
	g, err := graph.Build(dir)
	if err != nil {
		t.Fatalf("graph build: %v", err)
	}
	var hasPlan, hasFeat, hasPlansEdge bool
	for _, n := range g.Nodes {
		switch n.FileType {
		case graph.TypePlan:
			hasPlan = true
		case graph.TypeFeat:
			hasFeat = true
		}
	}
	for _, e := range g.Links {
		if e.Relation == graph.RelPlans {
			hasPlansEdge = true
		}
	}
	if !hasPlan || !hasFeat || !hasPlansEdge {
		t.Errorf("graph missing plan structure: plan=%v feat=%v plansEdge=%v", hasPlan, hasFeat, hasPlansEdge)
	}
	for _, f := range graph.Analyze(g, dir).Findings {
		if f.Kind == "unplanned_spec" && strings.Contains(f.Label+f.Message, "upload") {
			t.Errorf("upload is planned; must not be flagged unplanned: %+v", f)
		}
	}

	// 5c. WEB agrees: the /api/plan route serves plan.DeriveStatus — the same
	//     derivation, so it must show the same done state.
	doc, err := plan.Load(dir, "photos")
	if err != nil {
		t.Fatal(err)
	}
	webSt, err := plan.DeriveStatus(dir, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(webSt.Feats) != 1 || webSt.Feats[0].State != plan.StateDone {
		t.Errorf("web read-model should show upload done, got %+v", webSt.Feats)
	}

	// The runner journaled the completion with the deterministic timestamp.
	logData, _ := os.ReadFile(filepath.Join(dir, "docs", "plans", "photos", "log.md"))
	if !strings.Contains(string(logData), "## [2026-07-07] - | upload | done") {
		t.Errorf("run journal missing the completion line:\n%s", logData)
	}
}
