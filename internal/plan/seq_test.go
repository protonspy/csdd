package plan

import (
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

func nextFeatFor(t *testing.T, root, slug string, requireApproved bool) (Feat, int) {
	t.Helper()
	doc, err := Load(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	f, outcome, err := NextFeat(root, doc, requireApproved)
	if err != nil {
		t.Fatal(err)
	}
	return f, outcome
}

func markFeatDone(t *testing.T, root, slug, feat string) {
	t.Helper()
	l := LoadLedger(root, slug)
	l.MarkDone(feat, "done", time.Unix(0, 0))
	if err := l.Save(root, slug); err != nil {
		t.Fatal(err)
	}
}

func TestNextFeatOrderAndComplete(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)

	// Nothing done: the first feat in table order → a.
	f, outcome := nextFeatFor(t, root, "p", false)
	if outcome != SeqFeat || f.Slug != "a" {
		t.Fatalf("expected feat a, got %+v outcome=%d", f, outcome)
	}

	// a done → b (next in table order).
	markFeatDone(t, root, "p", "a")
	if f, _ := nextFeatFor(t, root, "p", false); f.Slug != "b" {
		t.Errorf("expected b next, got %s", f.Slug)
	}

	// a + b done → c.
	markFeatDone(t, root, "p", "b")
	if f, _ := nextFeatFor(t, root, "p", false); f.Slug != "c" {
		t.Errorf("expected c next, got %s", f.Slug)
	}

	// all done → complete.
	markFeatDone(t, root, "p", "c")
	if _, outcome := nextFeatFor(t, root, "p", false); outcome != SeqComplete {
		t.Errorf("expected SeqComplete, got %d", outcome)
	}
}

func TestNextFeatRequiresApproval(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	// Unapproved + requireApproved → SeqNotReady.
	if _, outcome := nextFeatFor(t, root, "p", true); outcome != SeqNotReady {
		t.Fatalf("expected SeqNotReady for unapproved plan, got %d", outcome)
	}
	// Approve, then it sequences.
	doc, _ := Load(root, "p")
	if err := ApprovePlan(root, doc, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if f, outcome := nextFeatFor(t, root, "p", true); outcome != SeqFeat || f.Slug != "a" {
		t.Errorf("expected feat a after approval, got %+v/%d", f, outcome)
	}
}

func TestNextFeatDeterministic(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	a, o1 := nextFeatFor(t, root, "p", false)
	b, o2 := nextFeatFor(t, root, "p", false)
	if a.Slug != b.Slug || o1 != o2 {
		t.Errorf("nondeterministic next: %+v/%d vs %+v/%d", a, o1, b, o2)
	}
}

// TestNextFeatIgnoresDiskState proves the ledger, not disk-derived feat state, is
// what the sequencer advances on (the loop trusts the session's `done` verdict).
func TestNextFeatIgnoresDiskState(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	// Fully implement feat a on disk, but do NOT mark it in the ledger.
	allApproved := map[string]bool{"requirements": true, "design": true, "tasks": true}
	writeSpec(t, root, "a", allApproved, true, "- [x] 1. done\n")
	// The sequencer still hands out a — the ledger has not recorded it.
	if f, _ := nextFeatFor(t, root, "p", false); f.Slug != "a" {
		t.Errorf("sequencer should follow the ledger, not disk; got %s", f.Slug)
	}
	// Once the ledger marks it, it advances.
	markFeatDone(t, root, "p", "a")
	if f, _ := nextFeatFor(t, root, "p", false); f.Slug != "b" {
		t.Errorf("expected b after ledger marks a, got %s", f.Slug)
	}
}
