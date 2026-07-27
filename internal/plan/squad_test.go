package plan

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The squad (--squad-limit, R7).
//
// Two things have to hold and they pull in opposite directions: sessions really do
// overlap when the plan admits it, and NOTHING the loop records is touched by more
// than one goroutine. The tests below pin both — the first by making a session
// refuse to finish until its peer is also running (a serial runner cannot satisfy
// that, so it fails rather than passing quietly), the second by leaving the
// bookkeeping assertions exactly as the serial tests make them.

// squadPlan has two independent (P) feats and one unmarked feat that depends on
// both. It is the smallest shape that distinguishes "runs a squad" from "runs one
// at a time": a and b may share the tree, c may not join anything and nothing may
// join c.
const squadPlan = `---
name: p
status: draft
---

## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | P | |
| 2 | b | B | — | M1 | P | |
| 3 | c | C | a, b | M1 | | |

## Quality Gates

- verify: make check
`

// serialPlan is squadPlan's control: the same two independent feats with the (P)
// marker taken off, so the squad must refuse to pair them however high the limit is.
const serialPlan = `---
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

// approvedSquadWorkspace lays down a plan, approves it, and returns the root.
func approvedSquadWorkspace(t *testing.T, planMD string) string {
	t.Helper()
	root := setupWorkspace(t, "p", planMD)
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApprovePlan(root, doc, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	return root
}

// deliverSpecFiles writes what the verdict gate demands of a delivered feat (R10.1)
// and reports an I/O failure instead of calling t.Fatal.
//
// The distinction matters here and nowhere else: a squad test calls this from a
// SESSION goroutine, and t.Fatal on a non-test goroutine runs runtime.Goexit — the
// session would never post its result and the run loop would wait for it until the
// whole test binary timed out. An error the caller records is diagnosable; a hang is
// not.
func deliverSpecFiles(root, slug string) error {
	dir := filepath.Join(root, "specs", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"spec.json": fmt.Sprintf(`{"feature_name":%q,"ready_for_implementation":true,"approvals":{`+
			`"requirements":{"generated":true,"approved":true},`+
			`"design":{"generated":true,"approved":true},`+
			`"tasks":{"generated":true,"approved":true}}}`, slug),
		"tasks.md": "# Tasks\n\n- [x] 1. Deliver the behavior\n      _Requirements: 1.1_\n",
		"test-report.json": fmt.Sprintf(`{"feature":%q,"updatedAt":"2026-07-07T00:00:00Z",`+
			`"command":"go test ./...","tests":{"total":3,"passed":3,"failed":0,"skipped":0}}`, slug),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// concurrencyProbe is a Session hook that delivers each feat and records how many
// sessions were live at once.
//
// `rendezvous` is what makes the observation load-bearing rather than lucky: when
// set, a session parks until `want` of them are live, so a runner that only ever
// opens one session cannot reach the barrier and the test fails on the timeout
// instead of silently observing peak=1 and calling it a race.
type concurrencyProbe struct {
	mu         sync.Mutex
	live, peak int
	order      []string // feats in the order sessions opened
	errs       []error

	rendezvous chan struct{}
	want       int
	// barrierAll makes every feat wait at the barrier, not just the (P) ones. It is
	// how a test proves the marker is NOT consulted: an unmarked feat that reaches
	// the rendezvous could only have been dispatched alongside a peer.
	barrierAll bool
	once       sync.Once
}

// enter records that a session opened and holds it at the barrier until enough
// peers have joined, so an overlap the scheduler refuses to create shows up as a
// timeout rather than as a quietly lower peak.
func (p *concurrencyProbe) enter(f Feat) {
	p.mu.Lock()
	p.live++
	if p.live > p.peak {
		p.peak = p.live
	}
	p.order = append(p.order, f.Slug)
	if p.rendezvous != nil && p.live >= p.want {
		p.once.Do(func() { close(p.rendezvous) })
	}
	p.mu.Unlock()

	if p.rendezvous != nil && (p.barrierAll || f.Parallel) {
		select {
		case <-p.rendezvous:
		case <-time.After(20 * time.Second):
			p.record(fmt.Errorf("%s waited for a peer session that never opened — the squad did not run", f.Slug))
		}
		return
	}
	// No barrier to hold the window open, so linger briefly: an overlap that exists
	// would be observed, and one that does not cannot be manufactured.
	time.Sleep(20 * time.Millisecond)
}

func (p *concurrencyProbe) leave() {
	p.mu.Lock()
	p.live--
	p.mu.Unlock()
}

func (p *concurrencyProbe) hook(root string) func(SessionRequest) (SessionOutcome, error) {
	return func(req SessionRequest) (SessionOutcome, error) {
		p.enter(req.Feat)
		defer p.leave()
		if err := deliverSpecFiles(root, req.Feat.Slug); err != nil {
			p.record(err)
		}
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
}

func (p *concurrencyProbe) record(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errs = append(p.errs, err)
}

// TestSquadRunsParallelFeatsAtOnce is the capability itself. Both (P) feats block
// until the other is live, so the run can only finish if two sessions were open
// simultaneously — the assertion cannot pass by accident on a serial loop.
func TestSquadRunsParallelFeatsAtOnce(t *testing.T) {
	root := approvedSquadWorkspace(t, squadPlan)
	probe := &concurrencyProbe{rendezvous: make(chan struct{}), want: 2}
	h := baseHooks(t, root)
	h.Session = probe.hook(root)

	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", SquadLimit: 2, Hooks: h, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range probe.errs {
		t.Error(e)
	}
	if probe.peak < 2 {
		t.Errorf("--squad-limit 2 must run both (P) feats at once, peak concurrency was %d", probe.peak)
	}
	if !sum.Completed || sum.Steps != 3 {
		t.Errorf("every feat should still be delivered: completed=%v steps=%d (%s)", sum.Completed, sum.Steps, sum.Reason)
	}
	if sum.Sessions != 3 {
		t.Errorf("three feats, one session each: got %d", sum.Sessions)
	}
}

// TestSquadNeverExceedsItsLimit pins the bound: three feats are ready but only two
// sessions may be open, so the third waits for a slot rather than being dispatched
// because the graph would allow it.
func TestSquadNeverExceedsItsLimit(t *testing.T) {
	root := approvedSquadWorkspace(t, `---
name: p
status: draft
---

## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | P | |
| 2 | b | B | — | M1 | P | |
| 3 | d | D | — | M1 | P | |

## Quality Gates

- verify: make check
`)
	probe := &concurrencyProbe{rendezvous: make(chan struct{}), want: 2}
	h := baseHooks(t, root)
	h.Session = probe.hook(root)

	sum, err := Run(RunOptions{Root: root, Slug: "p", SquadLimit: 2, Hooks: h, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range probe.errs {
		t.Error(e)
	}
	if probe.peak != 2 {
		t.Errorf("the squad must open exactly its limit, peak concurrency was %d", probe.peak)
	}
	if !sum.Completed {
		t.Errorf("the run should still complete: %s", sum.Reason)
	}
}

// TestUnmarkedFeatsStillShareTheSquad pins the rule the worktrees bought.
//
// Nothing in serialPlan is marked (P), and both feats still run at once, because
// the marker is no longer consulted: it used to be the author's consent to SHARE a
// working tree, and feats do not share one any more. Had this stayed a gate, every
// plan written before the capability existed would have run serially forever — the
// template told authors the column was decorative.
func TestUnmarkedFeatsStillShareTheSquad(t *testing.T) {
	root := approvedSquadWorkspace(t, serialPlan)
	// The barrier applies to every feat here, marked or not: if the scheduler still
	// refused to pair unmarked feats, neither would reach it and the test fails on
	// the timeout rather than on a lucky reading of peak concurrency.
	probe := &concurrencyProbe{rendezvous: make(chan struct{}), want: 2, barrierAll: true}
	h := baseHooks(t, root)
	h.Session = probe.hook(root)

	sum, err := Run(RunOptions{Root: root, Slug: "p", SquadLimit: 6, Hooks: h, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range probe.errs {
		t.Error(e)
	}
	if probe.peak != 2 {
		t.Errorf("the graph admits both feats, so both should run at once, peak concurrency was %d", probe.peak)
	}
	if !sum.Completed || sum.Steps != 2 {
		t.Errorf("both feats should still be delivered: completed=%v steps=%d (%s)", sum.Completed, sum.Steps, sum.Reason)
	}
}

// TestAdmitFeatFollowsTheGraphAlone is the scheduling rule on its own, without a run
// around it: table order over the ready set, the in-flight feats withheld, and the
// (P) column ignored in both directions.
func TestAdmitFeatFollowsTheGraphAlone(t *testing.T) {
	doc := dagDoc(t) // d(b,c) · b(P)→a · a · c(P)→a · e
	done := map[string]bool{"a": true}
	feat := func(slug string) Feat {
		t.Helper()
		f, ok := doc.Feat(slug)
		if !ok {
			t.Fatalf("plan has no feat %q", slug)
		}
		return f
	}

	// Empty squad: the head of the ready set, exactly what nextFeat picks.
	got, ok := admitFeat(doc, done, nil, nil, nil)
	if !ok || got.Slug != "b" {
		t.Errorf("an empty squad takes the head of the ready set, got %q ok=%v", got.Slug, ok)
	}

	// b in flight: the next ready feat in table order, marker irrelevant.
	got, ok = admitFeat(doc, done, nil, map[string]Feat{"b": feat("b")}, nil)
	if !ok || got.Slug != "c" {
		t.Errorf("the next ready feat should be offered, got %q ok=%v", got.Slug, ok)
	}

	// b and c in flight: e is unmarked and ready, and it is offered anyway — the
	// graph is the only gate. (d still is not: it depends on b and c.)
	got, ok = admitFeat(doc, done, nil, map[string]Feat{"b": feat("b"), "c": feat("c")}, nil)
	if !ok || got.Slug != "e" {
		t.Errorf("an unmarked ready feat must join the squad, got %q ok=%v", got.Slug, ok)
	}

	// Symmetry: an unmarked feat in flight does not close the squad either.
	got, ok = admitFeat(doc, done, nil, map[string]Feat{"e": feat("e")}, nil)
	if !ok || got.Slug != "b" {
		t.Errorf("a feat in flight must not block its ready peers, got %q ok=%v", got.Slug, ok)
	}

	// The graph still gates: with nothing delivered, only the roots are workable.
	if got, ok := admitFeat(doc, map[string]bool{}, nil, map[string]Feat{"a": feat("a")}, nil); !ok || got.Slug != "e" {
		t.Errorf("only a root feat is workable before `a` lands, got %q ok=%v", got.Slug, ok)
	}
}

// TestSquadNeverHandsOutAnInflightFeat is the invariant that makes the attempt
// bound and the ledger meaningful: one feat, one session. Without it two sessions
// would drive the same spec tree at once and both would settle against it.
func TestSquadNeverHandsOutAnInflightFeat(t *testing.T) {
	doc := dagDoc(t)
	b, _ := doc.Feat("b")
	got, ok := admitFeat(doc, map[string]bool{"a": true}, nil, map[string]Feat{"b": b}, nil)
	if ok && got.Slug == "b" {
		t.Error("a feat already in flight must never be dispatched again")
	}
}

// TestSquadKeepsBookkeepingSingleWriter runs a squad under the race detector's
// favourite shape — several sessions failing, retrying and delivering — and asserts
// the run's records are exactly what a serial run would have produced. Sessions
// overlap; the ledger, the journal and the session log do not.
func TestSquadKeepsBookkeepingSingleWriter(t *testing.T) {
	root := approvedSquadWorkspace(t, squadPlan)
	var mu sync.Mutex
	seen := map[string]int{}
	h := baseHooks(t, root)
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		f := req.Feat
		mu.Lock()
		seen[f.Slug]++
		n := seen[f.Slug]
		mu.Unlock()
		// Every feat reports partial work once before delivering, so each one settles
		// twice and the two paths (continue, then done) both run under concurrency.
		if n == 1 {
			return SessionOutcome{Verdict: Verdict{Status: VerdictContinue, Summary: "half of " + f.Slug}}, nil
		}
		if err := deliverSpecFiles(root, f.Slug); err != nil {
			return SessionOutcome{}, err
		}
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone, Summary: "delivered " + f.Slug}}, nil
	}

	sum, err := Run(RunOptions{Root: root, Slug: "p", SquadLimit: 3, Hooks: h, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed || sum.Steps != 3 || sum.Sessions != 6 {
		t.Fatalf("3 feats × 2 sessions: completed=%v steps=%d sessions=%d (%s)",
			sum.Completed, sum.Steps, sum.Sessions, sum.Reason)
	}

	// The append-only record must hold every attempt, opened and settled: 6 sessions
	// = 6 `started` rows + 6 settling rows. A lost or interleaved write shows up here.
	recs := LoadSessionRecords(root, "p")
	started, settled := 0, 0
	for _, r := range recs {
		if r.Status == SessionStarted {
			started++
			continue
		}
		settled++
	}
	if started != 6 || settled != 6 {
		t.Errorf("every attempt is opened then settled exactly once, got %d started / %d settled of %d rows",
			started, settled, len(recs))
	}

	ledger := LoadLedger(root, "p")
	for _, slug := range []string{"a", "b", "c"} {
		if !ledger.Done(slug) {
			t.Errorf("%s should be recorded delivered in the ledger", slug)
		}
	}
}
