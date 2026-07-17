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

// TestNextFeatCountsDiskDelivered proves a feat fully delivered on disk — every
// phase approved and every task checked — counts as done for sequencing even when
// the ledger has no entry, so a plan advanced by hand (developed directly in a
// session) resumes from where disk reality left off instead of redoing finished
// feats. A merely partial spec is NOT skipped.
func TestNextFeatCountsDiskDelivered(t *testing.T) {
	root := setupWorkspace(t, "p", seqPlan)
	allApproved := map[string]bool{"requirements": true, "design": true, "tasks": true}

	// Feat a fully delivered on disk, no ledger entry → the sequencer skips it.
	writeSpec(t, root, "a", allApproved, true, "- [x] 1. done\n")
	if f, _ := nextFeatFor(t, root, "p", false); f.Slug != "b" {
		t.Errorf("a is delivered on disk; sequencer should advance to b, got %s", f.Slug)
	}

	// Feat b only partially implemented on disk → still handed out (not delivered).
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. one\n- [ ] 2. two\n")
	if f, _ := nextFeatFor(t, root, "p", false); f.Slug != "b" {
		t.Errorf("b is partial; sequencer should still hand it out, got %s", f.Slug)
	}

	// Finishing b on disk advances to c.
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. one\n- [x] 2. two\n")
	if f, _ := nextFeatFor(t, root, "p", false); f.Slug != "c" {
		t.Errorf("b now delivered on disk; sequencer should advance to c, got %s", f.Slug)
	}
}
