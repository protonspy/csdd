package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const runnerPlan = `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | |
| 2 | b | B | — | M1 | | |

## Quality Gates

- verify: make check
`

var allApproved = map[string]bool{"requirements": true, "design": true, "tasks": true}

// approvedRunnerWorkspace lays down the plan, approves it, and returns the root.
func approvedRunnerWorkspace(t *testing.T) string {
	t.Helper()
	root := setupWorkspace(t, "p", runnerPlan)
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApprovePlan(root, doc, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	return root
}

// fixedNow is a deterministic clock for journal timestamps.
func fixedNow() time.Time { return time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC) }

// baseHooks returns hooks that always succeed and change nothing, so the loop
// would run to the iteration cap unless a test's stubs advance state.
func baseHooks() Hooks {
	return Hooks{
		Session:         func(Step, string, SessionMode, float64) (Verdict, error) { return Verdict{Status: VerdictDone}, nil },
		CSDD:            func(string, ...string) (bool, string) { return true, "" },
		Gate:            func(string, string) (bool, string) { return true, "" },
		ChangedPaths:    func(string) ([]string, error) { return []string{"specs/a/foo.go"}, nil },
		Commit:          func(string, string, []string) error { return nil },
		Doctor:          func() SandboxReport { return SandboxReport{OK: true} },
		ClaudeAvailable: func() bool { return true },
		Now:             fixedNow,
	}
}

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", `{"status":"done","summary":"ok"}`, VerdictDone, false},
		{"blocked", `{"status":"blocked","summary":"stuck","revision":"split it"}`, VerdictBlocked, false},
		{"envelope", `{"type":"result","result":"{\"status\":\"done\",\"summary\":\"ok\"}"}`, VerdictDone, false},
		{"envelope prose+json", `{"result":"Here is my verdict: {\"status\":\"blocked\",\"summary\":\"x\"}"}`, VerdictBlocked, false},
		{"uppercase", `{"status":"DONE","summary":"ok"}`, VerdictDone, false},
		{"garbage", `not json at all`, "", true},
		{"bad status", `{"status":"maybe","summary":"?"}`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseVerdict([]byte(tc.in))
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got %+v", v)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Status != tc.want {
				t.Errorf("status = %q, want %q", v.Status, tc.want)
			}
		})
	}
}

func TestRunnerPreflight(t *testing.T) {
	// Unapproved plan.
	root := setupWorkspace(t, "p", runnerPlan)
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: baseHooks(), Out: &bytes.Buffer{}}); err == nil {
		t.Errorf("expected preflight failure for unapproved plan")
	}

	// Approved but claude missing.
	root2 := approvedRunnerWorkspace(t)
	h := baseHooks()
	h.ClaudeAvailable = func() bool { return false }
	if _, err := Run(RunOptions{Root: root2, Slug: "p", Hooks: h, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "claude") {
		t.Errorf("expected claude-missing failure, got %v", err)
	}

	// Autonomous with a failing sandbox doctor.
	h2 := baseHooks()
	h2.Doctor = func() SandboxReport { return SandboxReport{OK: false} }
	if _, err := Run(RunOptions{Root: root2, Slug: "p", Autonomous: true, Hooks: h2, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("expected sandbox failure for --autonomous, got %v", err)
	}
}

func TestRunnerPathAuditBlocks(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	// Both feats ready with a task; the session "touches" a forbidden path.
	writeSpec(t, root, "a", allApproved, true, "- [ ] 1. do it\n")
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. done\n") // b already done
	h := baseHooks()
	h.ChangedPaths = func(string) ([]string, error) { return []string{"docs/graph/graph.json"}, nil }

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, MaxRetries: 1, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Blocks == 0 {
		t.Errorf("forbidden path change should block the feat")
	}
	if _, blocked := blockMarker(root, "p", "a"); !blocked {
		t.Errorf("expected a block marker for feat a")
	}
	if sum.Steps != 0 {
		t.Errorf("no step should have advanced, got %d", sum.Steps)
	}
}

func TestRunnerRetryAppendsFailure(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	writeSpec(t, root, "a", allApproved, true, "- [ ] 1. do it\n")
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. done\n")

	var briefs []string
	gateCalls := 0
	h := baseHooks()
	h.Session = func(step Step, brief string, _ SessionMode, _ float64) (Verdict, error) {
		briefs = append(briefs, brief)
		return Verdict{Status: VerdictDone}, nil
	}
	h.Gate = func(string, string) (bool, string) {
		gateCalls++
		if gateCalls == 1 {
			return false, "FAIL: expected 3 got 4"
		}
		return true, ""
	}
	// On the successful attempt the "session" checks the task box so the feat
	// completes and the loop terminates.
	h.Commit = func(string, string, []string) error {
		writeFile(t, filepath.Join(root, "specs", "a", "tasks.md"), "- [x] 1. do it\n")
		return nil
	}

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, MaxRetries: 2, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Retries != 1 {
		t.Errorf("expected 1 retry, got %d", sum.Retries)
	}
	if len(briefs) < 2 || !strings.Contains(briefs[1], "Previous attempt failed") || !strings.Contains(briefs[1], "expected 3 got 4") {
		t.Errorf("retry brief should append the gate failure, got %v", briefs)
	}
	if !sum.Completed {
		t.Errorf("run should complete once the retried step passes")
	}
}

func TestRunnerBlockedVerdictContinues(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	writeSpec(t, root, "a", allApproved, true, "- [ ] 1. do it\n")
	writeSpec(t, root, "b", allApproved, true, "- [ ] 1. do it\n")

	h := baseHooks()
	h.Session = func(step Step, _ string, _ SessionMode, _ float64) (Verdict, error) {
		if step.Feat == "a" {
			return Verdict{Status: VerdictBlocked, Summary: "cannot resolve", Revision: "split feat a"}, nil
		}
		return Verdict{Status: VerdictDone}, nil
	}
	// b advances by checking its box on commit.
	h.Commit = func(_ string, _ string, _ []string) error {
		writeFile(t, filepath.Join(root, "specs", "b", "tasks.md"), "- [x] 1. do it\n")
		return nil
	}

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 8, MaxRetries: 1, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	// a is blocked, b advanced; the loop ends with nothing unblocked.
	if _, blocked := blockMarker(root, "p", "a"); !blocked {
		t.Errorf("feat a should be blocked")
	}
	if sum.Steps < 1 {
		t.Errorf("feat b should have advanced despite a being blocked")
	}
	if sum.Outcome != OutcomeNothing {
		t.Errorf("expected OutcomeNothing after b done and a blocked, got %d", sum.Outcome)
	}
	// The deviation is journaled with its revision.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | blocked") || !strings.Contains(string(logData), "split feat a") {
		t.Errorf("deviation not journaled with revision: %s", logData)
	}
}

func TestRunnerJournalFormat(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	writeSpec(t, root, "a", allApproved, true, "- [ ] 1. do it\n")
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. done\n")
	h := baseHooks()
	h.Commit = func(string, string, []string) error {
		writeFile(t, filepath.Join(root, "specs", "a", "tasks.md"), "- [x] 1. do it\n")
		return nil
	}
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "## [2026-07-07] task 1 | a | done") {
		t.Errorf("journal line format wrong: %s", logData)
	}
}

func TestRunnerDriftStopsMidRun(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	writeSpec(t, root, "a", allApproved, true, "- [ ] 1. do it\n")
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. done\n")
	h := baseHooks()
	// The "session" drifts the plan by editing plan.md — the runner must stop on
	// the next iteration's drift check rather than keep going.
	h.Session = func(Step, string, SessionMode, float64) (Verdict, error) {
		writeFile(t, filepath.Join(Dir(root, "p"), "plan.md"), runnerPlan+"\n<!-- drift -->\n")
		return Verdict{Status: VerdictDone}, nil
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeDrift {
		t.Errorf("expected OutcomeDrift after mid-run edit, got %d (%s)", sum.Outcome, sum.Reason)
	}
}

func TestRunnerSpecPhaseApproval(t *testing.T) {
	// A pending feat drives the spec flow: the runner scaffolds, the session
	// authors, and the runner approves each phase via the CSDD hook.
	root := approvedRunnerWorkspace(t)
	// Make b already done so only a is in play.
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. done\n")

	approvals := map[string]bool{}
	h := baseHooks()
	h.CSDD = func(_ string, args ...string) (bool, string) {
		// Simulate the spec lifecycle the real csdd would perform.
		switch {
		case len(args) >= 2 && args[0] == "plan" && args[1] == "generate":
			writeSpec(t, root, "a", map[string]bool{}, false, "")
		case len(args) >= 4 && args[0] == "spec" && args[1] == "approve":
			phase := args[len(args)-1]
			approvals[phase] = true
			ready := approvals["requirements"] && approvals["design"] && approvals["tasks"]
			tasks := ""
			if ready {
				tasks = "- [x] 1. done\n"
			}
			writeSpec(t, root, "a", approvals, ready, tasks)
		}
		return true, ""
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed {
		t.Errorf("spec-phase-driven run should complete, got %+v", sum)
	}
	if !approvals["requirements"] || !approvals["design"] || !approvals["tasks"] {
		t.Errorf("all three phases should have been approved by the runner: %v", approvals)
	}
}
