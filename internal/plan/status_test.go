package plan

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// writeSpec creates specs/<slug>/spec.json with the given approvals and optional
// tasks.md content, to drive status derivation.
func writeSpec(t *testing.T, root, slug string, approvals map[string]bool, ready bool, tasksMD string) {
	t.Helper()
	spec := map[string]any{
		"feature_name":             slug,
		"ready_for_implementation": ready,
		"approvals":                map[string]any{},
	}
	ap := spec["approvals"].(map[string]any)
	for _, p := range []string{"requirements", "design", "tasks"} {
		ap[p] = map[string]any{"generated": true, "approved": approvals[p]}
	}
	b, _ := json.MarshalIndent(spec, "", "  ")
	writeFile(t, filepath.Join(root, "specs", slug, "spec.json"), string(b))
	if tasksMD != "" {
		writeFile(t, filepath.Join(root, "specs", slug, "tasks.md"), tasksMD)
	}
}

func stateOf(st PlanStatus, slug string) string {
	for _, f := range st.Feats {
		if f.Slug == slug {
			return f.State
		}
	}
	return "<absent>"
}

func TestDeriveStatusAllStates(t *testing.T) {
	planMD := `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | pend | none | — | M1 | | |
| 2 | inreq | req | — | M1 | | |
| 3 | indesign | des | — | M1 | | |
| 4 | intasks | tsk | — | M1 | | |
| 5 | ready | rdy | — | M1 | | |
| 6 | impl | imp | — | M1 | | |
| 7 | done | dn | — | M1 | | |

## Quality Gates

- verify: make check
`
	root := setupWorkspace(t, "p", planMD)

	// pend: no spec at all (nothing written).
	// inreq: spec exists, nothing approved.
	writeSpec(t, root, "inreq", map[string]bool{}, false, "")
	// indesign: requirements approved only.
	writeSpec(t, root, "indesign", map[string]bool{"requirements": true}, false, "")
	// intasks: requirements + design approved.
	writeSpec(t, root, "intasks", map[string]bool{"requirements": true, "design": true}, false, "")
	// ready: all approved, no task checked.
	allApproved := map[string]bool{"requirements": true, "design": true, "tasks": true}
	writeSpec(t, root, "ready", allApproved, true, "- [ ] 1. Do a thing\n")
	// impl: all approved, some tasks checked.
	writeSpec(t, root, "impl", allApproved, true, "- [x] 1. Done\n- [ ] 2. Not yet\n")
	// done: all approved, every task checked.
	writeSpec(t, root, "done", allApproved, true, "- [x] 1. Done\n- [x] 2. Also done\n")

	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	st, err := DeriveStatus(root, doc)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"pend":     StatePending,
		"inreq":    StateRequirements,
		"indesign": StateDesign,
		"intasks":  StateTasks,
		"ready":    StateReady,
		"impl":     StateImplementing,
		"done":     StateDone,
	}
	for slug, wantState := range want {
		if got := stateOf(st, slug); got != wantState {
			t.Errorf("feat %s: state = %s, want %s", slug, got, wantState)
		}
	}
}

func TestDeriveStatusApprovalAndDrift(t *testing.T) {
	planMD := `---
name: p
status: approved
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | |

## Quality Gates

- verify: make check
`
	root := setupWorkspace(t, "p", planMD)
	dir := Dir(root, "p")

	// Unapproved.
	doc, _ := Load(root, "p")
	st, _ := DeriveStatus(root, doc)
	if st.Approved {
		t.Errorf("plan should be unapproved before plan.json exists")
	}

	// Approve at the current hash → approved, no drift.
	h, err := HashPlan(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := SavePlanJSON(dir, PlanJSON{Name: "p", Approvals: PlanApproval{Approved: true, ContentHash: h}}); err != nil {
		t.Fatal(err)
	}
	st, _ = DeriveStatus(root, doc)
	if !st.Approved || st.Drift {
		t.Errorf("expected approved & no drift, got approved=%v drift=%v", st.Approved, st.Drift)
	}

	// Edit plan.md → drift.
	writeFile(t, filepath.Join(dir, "plan.md"), planMD+"\n<!-- edit -->\n")
	st, _ = DeriveStatus(root, doc)
	if !st.Drift {
		t.Errorf("expected drift after editing plan.md")
	}
}

// TestDeriveStatusLedgerWins proves a feat the loop recorded delivered reads done
// regardless of what the disk says — the ledger is the loop's source of truth.
func TestDeriveStatusLedgerWins(t *testing.T) {
	planMD := `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | |

## Quality Gates

- verify: make check
`
	root := setupWorkspace(t, "p", planMD)
	// No spec on disk → would derive as pending; the ledger overrides to done.
	l := LoadLedger(root, "p")
	l.MarkDone("a", "delivered", fixedNow())
	if err := l.Save(root, "p"); err != nil {
		t.Fatal(err)
	}
	doc, _ := Load(root, "p")
	st, _ := DeriveStatus(root, doc)
	if stateOf(st, "a") != StateDone {
		t.Errorf("ledger-marked feat should read done, got %s", stateOf(st, "a"))
	}
}
