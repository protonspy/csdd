package plan

import (
	"path/filepath"
	"testing"
	"time"
)

const seqPlan = `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | |
| 2 | b | B | a | M1 | | |
| 3 | c | C | — | M2 | | |

## Quality Gates

- verify: make check
`

func nextStep(t *testing.T, root, slug string, requireApproved bool) (Step, int) {
	t.Helper()
	doc, err := Load(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	step, outcome, err := Next(root, doc, requireApproved)
	if err != nil {
		t.Fatal(err)
	}
	return step, outcome
}

func TestNextOrderingAndDeps(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	allApproved := map[string]bool{"requirements": true, "design": true, "tasks": true}

	// Nothing done: first M1 feat with no deps → a, at the requirements phase.
	step, outcome := nextStep(t, root, "p", false)
	if outcome != SeqStep || step.Feat != "a" || step.Step != StepSpecRequirements {
		t.Fatalf("expected a/spec-requirements, got %+v outcome=%d", step, outcome)
	}

	// a done: b's dep is met and b (M1) precedes c (M2).
	writeSpec(t, root, "a", allApproved, true, "- [x] 1. done\n")
	step, _ = nextStep(t, root, "p", false)
	if step.Feat != "b" {
		t.Errorf("expected b next (M1 before M2), got %s", step.Feat)
	}

	// a + b done: c (M2) is next.
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. done\n")
	step, _ = nextStep(t, root, "p", false)
	if step.Feat != "c" {
		t.Errorf("expected c next, got %s", step.Feat)
	}

	// all done → complete.
	writeSpec(t, root, "c", allApproved, true, "- [x] 1. done\n")
	_, outcome = nextStep(t, root, "p", false)
	if outcome != SeqComplete {
		t.Errorf("expected SeqComplete, got %d", outcome)
	}
}

func TestNextBlockedAndNothing(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	// a is blocked (runner marker); b depends on a, c is independent.
	writeFile(t, filepath.Join(stateDir(root, "p"), "blocked", "a"), "gate failed\n")

	// c is still reachable.
	step, outcome := nextStep(t, root, "p", false)
	if outcome != SeqStep || step.Feat != "c" {
		t.Fatalf("expected c reachable while a blocked, got %+v/%d", step, outcome)
	}

	// Now mark c done too; only b remains, but it waits on blocked a → nothing.
	allApproved := map[string]bool{"requirements": true, "design": true, "tasks": true}
	writeSpec(t, root, "c", allApproved, true, "- [x] 1. done\n")
	_, outcome = nextStep(t, root, "p", false)
	if outcome != SeqNothing {
		t.Errorf("expected SeqNothing (b waits on blocked a), got %d", outcome)
	}
}

func TestNextStepProgression(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	cases := []struct {
		approvals map[string]bool
		ready     bool
		tasks     string
		wantStep  string
	}{
		{map[string]bool{}, false, "", StepSpecRequirements},
		{map[string]bool{"requirements": true}, false, "", StepSpecDesign},
		{map[string]bool{"requirements": true, "design": true}, false, "", StepSpecTasks},
		{map[string]bool{"requirements": true, "design": true, "tasks": true}, true,
			"- [ ] 1. Parent\n  - [ ] 1.1 First _Requirements: 1.1_\n  - [ ] 1.2 Second _Depends: 1.1_\n", "task 1.1"},
	}
	for _, tc := range cases {
		writeSpec(t, root, "a", tc.approvals, tc.ready, tc.tasks)
		step, outcome := nextStep(t, root, "p", false)
		if outcome != SeqStep || step.Feat != "a" || step.Step != tc.wantStep {
			t.Errorf("approvals=%v → step %q (want %q)", tc.approvals, step.Step, tc.wantStep)
		}
	}
}

func TestNextTaskHonorsDepends(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	allApproved := map[string]bool{"requirements": true, "design": true, "tasks": true}
	// 1.1 done; 1.2 depends on 1.1 (met) → next is 1.2, not 1.3 (depends on 1.2, unmet).
	tasks := "- [x] 1.1 First _Requirements: 1.1_\n- [ ] 1.2 Second _Depends: 1.1_\n- [ ] 1.3 Third _Depends: 1.2_\n"
	writeSpec(t, root, "a", allApproved, true, tasks)
	step, _ := nextStep(t, root, "p", false)
	if step.Step != "task 1.2" {
		t.Errorf("expected task 1.2 (deps met), got %q", step.Step)
	}
}

func TestNextRequiresApproval(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	// Unapproved + requireApproved → SeqNotReady.
	_, outcome := nextStep(t, root, "p", true)
	if outcome != SeqNotReady {
		t.Fatalf("expected SeqNotReady for unapproved plan, got %d", outcome)
	}
	// Approve, then it sequences.
	doc, _ := Load(root, "p")
	if err := ApprovePlan(root, doc, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	_, outcome = nextStep(t, root, "p", true)
	if outcome != SeqStep {
		t.Errorf("expected SeqStep after approval, got %d", outcome)
	}
	// Drift (edit plan.md) → SeqNotReady again.
	writeFile(t, filepath.Join(Dir(root, "p"), "plan.md"), seqPlan+"\n<!-- edit -->\n")
	_, outcome = nextStep(t, root, "p", true)
	if outcome != SeqNotReady {
		t.Errorf("expected SeqNotReady after drift, got %d", outcome)
	}
}

func TestNextDeterministic(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	a, o1 := nextStep(t, root, "p", false)
	b, o2 := nextStep(t, root, "p", false)
	if a != b || o1 != o2 {
		t.Errorf("nondeterministic next: %+v/%d vs %+v/%d", a, o1, b, o2)
	}
}
