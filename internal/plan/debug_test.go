package plan

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A debugger for `plan run`.
//
// The suite already proves individual properties — the squad overlaps, the gate
// refuses, a conflict rolls back. What it does not give you is the ONE artifact you
// want when a real run misbehaves: an ordered account of every decision the loop
// made, next to the durable record it left behind, so you can see where the two
// disagree.
//
// That gap is not hypothetical. A real run (`frontend-design-refresh` in the
// `violet` workspace, 2026-07-27) spent thirteen dispatches to deliver one of four
// feats, and its sessions.jsonl explains almost none of it: sixteen of nineteen
// rows are `started` with no settled counterpart and no cost. Reading the run after
// the fact meant reconstructing it from a journal, a failure log and a progress
// file that each hold a different slice of the truth.
//
// runTrace records the loop from inside its own seams (Hooks) and then reconciles
// what it saw against sessions.jsonl. `go test -run TestDebug -v` prints the
// timeline; the scenarios below assert on it. Every scenario runs one deliberately
// trivial feat, because the subject under observation is the LOOP, and a feat with
// real content only adds noise to the trace.

// --- the trace ----------------------------------------------------------------

type traceKind string

const (
	trEnsure    traceKind = "ensure"    // a worktree was handed to a dispatch
	trSessionIn traceKind = "session>"  // the session hook was entered
	trSessionOu traceKind = "session<"  // the session hook returned a verdict
	trIntegrate traceKind = "integrate" // the feat's branch was merged into the base
	trDiscard   traceKind = "discard"   // the worktree was released
	trSleep     traceKind = "sleep"     // the loop waited (account limit / spawn backoff)
	trNote      traceKind = "note"      // an observation a scenario made from inside a session
)

// traceEvent is one decision, in the order the loop made it.
type traceEvent struct {
	seq    int
	at     time.Duration // since the run started, so a stall is visible in the dump
	kind   traceKind
	feat   string
	detail string
}

// runTrace observes a run through the hooks it already exposes and doubles as the
// treeKeeper, which is the only way to see the worktree each dispatch was handed —
// the fact that distinguishes "the retry got a fresh tree" from "the retry got the
// stale one", and neither the journal nor the ledger records it.
//
// It is safe for a squad: every field is behind the mutex because the Session hook
// runs on a session goroutine while Ensure/Integrate/Discard run on the loop's.
type runTrace struct {
	mu     sync.Mutex
	start  time.Time
	events []traceEvent
	// dirs is the worktree handed to each dispatch of a feat, in order.
	dirs map[string][]string
	// errs collects failures observed from a session goroutine. t.Fatal there runs
	// runtime.Goexit, the session never posts its result, and the run loop waits for
	// it until the whole binary times out — so a session-side check records instead
	// of failing, exactly as the squad tests do.
	errs  []error
	inner treeKeeper
}

func newRunTrace(inner treeKeeper) *runTrace {
	return &runTrace{start: time.Now(), dirs: map[string][]string{}, inner: inner}
}

func (tr *runTrace) add(kind traceKind, feat, detail string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.events = append(tr.events, traceEvent{
		seq: len(tr.events) + 1, at: time.Since(tr.start), kind: kind, feat: feat, detail: detail,
	})
}

func (tr *runTrace) fail(err error) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.errs = append(tr.errs, err)
}

// treeKeeper: delegate to the real keeper, record what it returned.

func (tr *runTrace) Ensure(feat string) (string, error) {
	dir, err := tr.inner.Ensure(feat)
	tr.mu.Lock()
	tr.dirs[feat] = append(tr.dirs[feat], dir)
	n := len(tr.dirs[feat])
	tr.mu.Unlock()
	tr.add(trEnsure, feat, fmt.Sprintf("dispatch #%d dir=%s err=%v", n, filepath.Base(dir), err))
	return dir, err
}

func (tr *runTrace) Integrate(feat string) error {
	err := tr.inner.Integrate(feat)
	tr.add(trIntegrate, feat, describeErr(err))
	return err
}

func (tr *runTrace) Discard(feat string) error {
	err := tr.inner.Discard(feat)
	tr.add(trDiscard, feat, describeErr(err))
	return err
}

// describeErr names an integration outcome the way the loop branches on it, so the
// dump distinguishes the three cases the runner treats differently rather than
// printing one opaque error string.
func describeErr(err error) string {
	switch e := err.(type) {
	case nil:
		return "merged"
	case *MergeConflictError:
		return "CONFLICT in " + strings.Join(e.Files, ", ")
	case *UncommittedWorkError:
		return fmt.Sprintf("UNCOMMITTED %d path(s)", len(e.Paths))
	default:
		return "error: " + err.Error()
	}
}

// wrap installs the trace into a set of hooks: it becomes the treeKeeper and
// interposes on the session so both ends of a dispatch land in the timeline.
func (tr *runTrace) wrap(h Hooks) Hooks {
	inner := h.Session
	h.Trees = tr
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		tr.add(trSessionIn, req.Feat.Slug, fmt.Sprintf("brief=%dB dir=%s", len(req.Brief), filepath.Base(req.Dir)))
		out, err := inner(req)
		tr.add(trSessionOu, req.Feat.Slug, fmt.Sprintf("verdict=%s err=%v cost=$%.2f tokens=%d",
			orNone(out.Verdict.Status), err, out.Metrics.CostUSD, out.Metrics.Tokens.Total()))
		return out, err
	}
	sleep := h.Sleep
	h.Sleep = func(d time.Duration) {
		tr.add(trSleep, "-", d.String())
		if sleep != nil {
			sleep(d)
		}
	}
	return h
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}

// waitFor blocks until an event matching kind and feat has been recorded. It is how
// a scenario imposes an order on a squad without reaching into the runner: a session
// can park until its peer's merge has actually landed, which turns a race into a
// deterministic sequence. Returns false on timeout so the caller reports a stuck
// run instead of hanging the binary.
func (tr *runTrace) waitFor(kind traceKind, feat string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if tr.count(kind, feat) > 0 {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// count is how many recorded events match kind and feat; an empty feat counts every
// feat.
func (tr *runTrace) count(kind traceKind, feat string) int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	n := 0
	for _, e := range tr.events {
		if e.kind == kind && (feat == "" || e.feat == feat) {
			n++
		}
	}
	return n
}

// dirsFor is every worktree handed to a feat, in dispatch order.
func (tr *runTrace) dirsFor(feat string) []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]string(nil), tr.dirs[feat]...)
}

// --- reconciliation against the durable record --------------------------------

// ledgerAudit is what sessions.jsonl claims about a run, summarized the way an
// operator reading it after the fact needs it.
type ledgerAudit struct {
	started   int
	settled   int
	unsettled []string // "feat#attempt" opened and never closed — a crashed attempt
	costed    int      // settled rows that carry a cost
	free      []string // settled rows reporting no cost at all
	totalUSD  float64
	byStatus  map[string]int
}

// auditLedger reads sessions.jsonl back and pairs every `started` row with its
// settled counterpart.
//
// This is the check the real run needed and nobody could make: a `started` row with
// no settled row is an attempt that crashed, and its cost is gone — the metrics are
// only ever attached on the settle path. Sixteen of the nineteen rows in the violet
// run were exactly that, which is why its recorded $20.01 is a fraction of what it
// actually spent.
func auditLedger(root, slug string) ledgerAudit {
	a := ledgerAudit{byStatus: map[string]int{}}
	open := map[string]bool{}
	for _, r := range LoadSessionRecords(root, slug) {
		a.byStatus[r.Status]++
		key := fmt.Sprintf("%s#%d", r.Feat, r.Attempt)
		if r.Status == SessionStarted {
			a.started++
			open[key] = true
			continue
		}
		a.settled++
		delete(open, key)
		a.totalUSD += r.CostUSD
		if r.CostUSD > 0 || r.Tokens.Total() > 0 {
			a.costed++
		} else {
			a.free = append(a.free, key+" ("+r.Status+")")
		}
	}
	for key := range open {
		a.unsettled = append(a.unsettled, key)
	}
	return a
}

// nearUSD compares two dollar figures at cent precision. Costs are summed as
// float64 across rows, so 11.16 + 4.22 is not exactly 15.38 and an equality check
// would fail on arithmetic rather than on behavior.
func nearUSD(got, want float64) bool {
	d := got - want
	return d < 0.005 && d > -0.005
}

// dump prints the timeline and the audit through t.Log, which is the debugger
// itself: `go test -run <scenario> -v` is the whole interface.
func (tr *runTrace) dump(t *testing.T, root, slug string, sum RunSummary, runLog string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("\n╭─ plan run timeline ─────────────────────────────────────────────\n")
	tr.mu.Lock()
	for _, e := range tr.events {
		fmt.Fprintf(&b, "│ %3d  %7dms  %-10s %-10s %s\n", e.seq, e.at.Milliseconds(), e.kind, e.feat, e.detail)
	}
	tr.mu.Unlock()
	b.WriteString("├─ sessions.jsonl ────────────────────────────────────────────────\n")
	for _, r := range LoadSessionRecords(root, slug) {
		fmt.Fprintf(&b, "│ %-10s attempt %d  %-9s cost=$%-6.2f tokens=%-9d gated=%v %s\n",
			r.Feat, r.Attempt, r.Status, r.CostUSD, r.Tokens.Total(), r.Gated, firstLine(r.Detail))
	}
	a := auditLedger(root, slug)
	b.WriteString("├─ audit ─────────────────────────────────────────────────────────\n")
	fmt.Fprintf(&b, "│ started=%d settled=%d unsettled=%v\n", a.started, a.settled, a.unsettled)
	fmt.Fprintf(&b, "│ settled rows with a cost=%d, without=%v, total=$%.2f\n", a.costed, a.free, a.totalUSD)
	fmt.Fprintf(&b, "│ summary: sessions=%d delivered=%d failures=%d gated=%d blocked=%v outcome=%d\n",
		sum.Sessions, sum.Steps, sum.Failures, sum.Gated, sum.Blocked, sum.Outcome)
	fmt.Fprintf(&b, "│ reason: %s\n", sum.Reason)
	if runLog != "" {
		b.WriteString("├─ runner output ─────────────────────────────────────────────────\n")
		for _, line := range strings.Split(strings.TrimRight(runLog, "\n"), "\n") {
			fmt.Fprintf(&b, "│ %s\n", line)
		}
	}
	b.WriteString("╰─────────────────────────────────────────────────────────────────")
	t.Log(b.String())
}

// --- the subject: one trivial feat --------------------------------------------

// debugSoloPlan is a single feat with nothing in it. The loop is what is under
// observation, so the feat carries no dependencies, no stack refs and no wiki refs —
// anything it did carry would appear in the brief and in the trace without telling
// us anything about the runner.
const debugSoloPlan = `---
name: p
status: draft
---

## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | widget | Ship one trivial widget | — | M1 | | |

## Quality Gates

- verify: make check
`

// debugPairPlan is the same trivial feat twice, independent, so a squad of two can
// put both in flight at once. It is the smallest shape in which two feats can
// collide on the base.
const debugPairPlan = `---
name: p
status: draft
---

## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | widget | Ship one trivial widget | — | M1 | P | |
| 2 | gadget | Ship one trivial gadget | — | M1 | P | |

## Quality Gates

- verify: make check
`

// debugRepo is a real repository with the plan approved and committed, plus the
// tracing keeper wrapped around the real git one. Real git is the point: the
// failures worth debugging live in the merge, and a stubbed keeper cannot produce
// them.
//
// The returned buffer is BOTH the keeper's log destination and the run's, mirroring
// how Run wires them together — otherwise a scenario could not see the one thing
// the keeper decides on its own. Both are written from the loop's goroutine only,
// so sharing the buffer is safe under a squad.
func debugRepo(t *testing.T, planMD string) (root string, tr *runTrace, out *bytes.Buffer) {
	t.Helper()
	root = gitRepo(t)
	seedApprovedPlan(t, root, planMD)
	out = &bytes.Buffer{}
	keeper := newTrees(root)
	keeper.logf = func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
	return root, newRunTrace(keeper), out
}

// debugHooks are baseHooks with the trace installed and the session left to the
// caller: every scenario differs only in what its session does.
func debugHooks(t *testing.T, root string, tr *runTrace, session func(SessionRequest) (SessionOutcome, error)) Hooks {
	t.Helper()
	h := baseHooks(t, root)
	h.Session = session
	return tr.wrap(h)
}

// deliverInTree writes the three artifacts the verdict gate demands and commits
// them, which is the whole of what a session contracts to leave behind. Errors are
// returned rather than fatal because this runs on a session goroutine.
func deliverInTree(dir, slug string, extra map[string]string) error {
	if err := deliverSpecFiles(dir, slug); err != nil {
		return err
	}
	for rel, content := range extra {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	if out, err := runGit(dir, "add", "-A"); err != nil {
		return fmt.Errorf("git add in %s: %v: %s", dir, err, out)
	}
	// A retry that re-does work its predecessor already committed has nothing new to
	// record, and git calls that an error. It is not one here: the contract the
	// runner checks is that the tree is clean and the work is on the branch, which
	// is already true. Treating it as a failure would make every scenario that
	// re-dispatches a feat fail for a reason that has nothing to do with the loop.
	if out, err := runGit(dir, "status", "--porcelain"); err == nil && strings.TrimSpace(out) == "" {
		return nil
	}
	if out, err := runGit(dir, "commit", "-m", "feat "+slug); err != nil {
		return fmt.Errorf("git commit in %s: %v: %s", dir, err, out)
	}
	return nil
}

// paidOutcome is a verdict carrying plausible metrics, so a scenario can watch what
// the loop does with the cost of an attempt rather than always seeing zero.
func paidOutcome(status string, usd float64) SessionOutcome {
	return SessionOutcome{
		Verdict: Verdict{Status: status},
		Metrics: SessionMetrics{
			Duration: time.Minute, CostUSD: usd, NumTurns: 12,
			Tokens: SessionTokens{Input: 60, Output: 20000, CacheRead: 1500000, CacheCreation: 40000},
			Models: []string{"claude-opus-5"},
		},
	}
}

// --- scenarios ----------------------------------------------------------------

// TestDebugHappyPathIsFullyAccounted is the baseline every other scenario is read
// against: one feat, one session, delivered. It pins the invariant the violet run
// broke — every attempt that was opened was also settled, and every settled attempt
// carries what it cost.
func TestDebugHappyPathIsFullyAccounted(t *testing.T) {
	root, tr, out := debugRepo(t, debugSoloPlan)
	h := debugHooks(t, root, tr, func(req SessionRequest) (SessionOutcome, error) {
		if err := deliverInTree(req.Dir, req.Feat.Slug, map[string]string{"widget.go": "package p\n"}); err != nil {
			return SessionOutcome{}, err
		}
		return paidOutcome(VerdictDone, 4.64), nil
	})

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	tr.dump(t, root, "p", sum, out.String())

	if !sum.Completed || sum.Steps != 1 || sum.Sessions != 1 {
		t.Fatalf("one trivial feat should take exactly one session: %+v", sum)
	}
	if n := tr.count(trIntegrate, "widget"); n != 1 {
		t.Errorf("the delivered feat must be merged exactly once, got %d", n)
	}
	if n := tr.count(trDiscard, "widget"); n != 1 {
		t.Errorf("a delivered feat's worktree must be released, got %d discards", n)
	}
	a := auditLedger(root, "p")
	if len(a.unsettled) != 0 {
		t.Errorf("every opened attempt must be settled, dangling: %v", a.unsettled)
	}
	if a.started != 1 || a.settled != 1 {
		t.Errorf("want 1 started + 1 settled row, got %d/%d", a.started, a.settled)
	}
	if a.costed != 1 || !nearUSD(a.totalUSD, 4.64) {
		t.Errorf("the session's cost must reach the record: costed=%d total=$%.2f", a.costed, a.totalUSD)
	}
}

// TestDebugRefusedDoneCostsAWholeSecondSession prices the verdict gate. A session
// that claims `done` without leaving the artifacts is handed back, and the retry is
// not a cheap correction — it is another entire session, paid in full.
//
// This is the shape that dominated the violet run: two of its three settled sessions
// were gated `done` claims, $15.38 of the $20.01 it managed to record.
func TestDebugRefusedDoneCostsAWholeSecondSession(t *testing.T) {
	root, tr, out := debugRepo(t, debugSoloPlan)
	var attempts int
	h := debugHooks(t, root, tr, func(req SessionRequest) (SessionOutcome, error) {
		attempts++
		if attempts == 1 {
			// Confident and wrong: nothing on disk, `done` anyway.
			return paidOutcome(VerdictDone, 11.16), nil
		}
		if err := deliverInTree(req.Dir, req.Feat.Slug, nil); err != nil {
			return SessionOutcome{}, err
		}
		return paidOutcome(VerdictDone, 4.22), nil
	})

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	tr.dump(t, root, "p", sum, out.String())

	if sum.Gated != 1 {
		t.Errorf("the empty `done` must be refused exactly once, gated=%d", sum.Gated)
	}
	if sum.Sessions != 2 || !sum.Completed {
		t.Fatalf("the refusal must cost a second full session and then complete: %+v", sum)
	}
	// The refused session never reached Integrate: the gate runs first, so nothing
	// was merged and the tree was not released.
	if n := tr.count(trIntegrate, "widget"); n != 1 {
		t.Errorf("only the delivering session should reach the merge, integrates=%d", n)
	}
	a := auditLedger(root, "p")
	if !nearUSD(a.totalUSD, 11.16+4.22) {
		t.Errorf("both sessions were paid for and both must be recorded, total=$%.2f", a.totalUSD)
	}
	if a.byStatus[SessionContinue] != 1 {
		t.Errorf("the refused `done` must be recorded as a continue, got %v", a.byStatus)
	}
}

// TestDebugCrashedAttemptLosesItsCost pins the instrumentation hole directly.
//
// recordSession writes the `started` row BEFORE the session is spawned and the cost
// is only ever attached on the settle path, so everything between the two is a
// window in which an interrupted run loses what it spent. The scenario observes the
// window from inside the session — the exact moment a reboot would land — and
// asserts what the record holds there: an open attempt with no cost.
//
// The attempt itself survives, which is deliberate (a crashed feat must not get its
// bound back). The money does not, and that is the defect.
func TestDebugCrashedAttemptLosesItsCost(t *testing.T) {
	root, tr, out := debugRepo(t, debugSoloPlan)
	var midFlight ledgerAudit
	h := debugHooks(t, root, tr, func(req SessionRequest) (SessionOutcome, error) {
		// The row for THIS attempt is already on disk; a crash here is what the
		// violet run hit sixteen times.
		midFlight = auditLedger(root, "p")
		if err := deliverInTree(req.Dir, req.Feat.Slug, nil); err != nil {
			return SessionOutcome{}, err
		}
		return paidOutcome(VerdictDone, 7.00), nil
	})

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	tr.dump(t, root, "p", sum, out.String())

	if midFlight.started != 1 {
		t.Fatalf("the attempt must be on disk before the session runs, started=%d", midFlight.started)
	}
	if len(midFlight.unsettled) != 1 {
		t.Fatalf("mid-session the attempt must be open, unsettled=%v", midFlight.unsettled)
	}
	if !nearUSD(midFlight.totalUSD, 0) {
		t.Errorf("nothing is expected to be costed yet, got $%.2f", midFlight.totalUSD)
	}
	t.Logf("crash window: an interrupted run leaves %v with $0.00 recorded, "+
		"while the session had already spent whatever it spent", midFlight.unsettled)

	// And once it settles normally, the cost does land — so the loss is specific to
	// the interrupted path, not to the accounting as a whole.
	if a := auditLedger(root, "p"); !nearUSD(a.totalUSD, 7.00) {
		t.Errorf("a session that returns must have its cost recorded, total=$%.2f", a.totalUSD)
	}
	_ = sum
}

// TestDebugRetryKeepsTheStaleBase is the stale-base defect, isolated.
//
// Ensure reuses a LIVE worktree untouched, deliberately: a feat that reported
// `continue` has unfinished, uncommitted work in that tree and cutting a fresh one
// would discard it (tree.go). The cost of that choice is invisible until a peer
// lands: the retry keeps working against the base its FIRST dispatch was cut from,
// however far the base has moved since.
//
// The scenario forces the order rather than racing for it — gadget parks until
// widget's merge has actually been recorded — so the observation is deterministic.
//
// It pins the defect as it stands today: the retry demonstrably cannot see a peer
// that is already on the base. When the runner learns to re-sync a live tree before
// re-dispatching it, THIS TEST WILL FAIL — and that failure is the notification
// that the fix landed, not a regression. Re-point it at the new behavior then.
func TestDebugRetryKeepsTheStaleBase(t *testing.T) {
	root, tr, out := debugRepo(t, debugPairPlan)
	var gadgetTries int
	h := debugHooks(t, root, tr, func(req SessionRequest) (SessionOutcome, error) {
		switch req.Feat.Slug {
		case "widget":
			if err := deliverInTree(req.Dir, "widget", map[string]string{"widget.go": "package p // widget\n"}); err != nil {
				return SessionOutcome{}, err
			}
			return paidOutcome(VerdictDone, 1), nil
		default:
			gadgetTries++
			if gadgetTries == 1 {
				// Hold the first attempt open until widget has landed on the base,
				// so the retry happens in a world where the base has moved.
				if !tr.waitFor(trIntegrate, "widget", 30*time.Second) {
					tr.fail(fmt.Errorf("widget never merged; the scenario could not be sequenced"))
				}
				return paidOutcome(VerdictContinue, 1), nil
			}
			// The retry. Whatever this session builds against is what the defect is
			// about, so record it rather than judging it here — the assertion belongs
			// on the test goroutine, and a session that calls t.Fatal would hang the run.
			if _, err := os.Stat(filepath.Join(req.Dir, "widget.go")); err != nil {
				tr.add(trNote, "gadget", "STALE: retry cannot see widget.go, already on the run base")
			} else {
				tr.add(trNote, "gadget", "FRESH: retry sees widget.go from the run base")
			}
			if err := deliverInTree(req.Dir, "gadget", map[string]string{"gadget.go": "package p // gadget\n"}); err != nil {
				return SessionOutcome{}, err
			}
			return paidOutcome(VerdictDone, 1), nil
		}
	})

	sum, err := Run(RunOptions{Root: root, Slug: "p", SquadLimit: 2, Hooks: h, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	tr.dump(t, root, "p", sum, out.String())

	// The retry was handed the SAME directory as the first attempt — that is the
	// mechanism, and it holds whether or not the staleness bites in this run.
	dirs := tr.dirsFor("gadget")
	if len(dirs) < 2 {
		t.Fatalf("gadget should have been dispatched twice, got %d", len(dirs))
	}
	if dirs[0] != dirs[1] {
		t.Errorf("Ensure is documented to reuse a live tree; got %q then %q", dirs[0], dirs[1])
	}
	// The consequence of that reuse, stated as the current behavior.
	stale, fresh := 0, 0
	tr.mu.Lock()
	for _, e := range tr.events {
		if e.kind != trNote {
			continue
		}
		if strings.HasPrefix(e.detail, "STALE") {
			stale++
		} else {
			fresh++
		}
	}
	tr.mu.Unlock()
	if stale+fresh != 1 {
		t.Fatalf("the retry should have made exactly one observation, got stale=%d fresh=%d", stale, fresh)
	}
	if fresh == 1 {
		t.Fatal("the retry now sees the peer's merged work — the runner re-syncs live trees. " +
			"That is the fix, not a regression: update this test to assert the new behavior.")
	}
	t.Log("CONFIRMED DEFECT: gadget's second session worked against the base its FIRST " +
		"dispatch was cut from, with widget's merge invisible to it. Nothing in the loop " +
		"moves a live tree forward, so a feat's staleness grows with every peer that lands.")
	for _, e := range tr.errs {
		t.Errorf("%v", e)
	}
}

// TestDebugGeneratedArtifactNoLongerBlocksTheMerge is the regression test for the
// single most expensive behavior the violet run exhibited.
//
// Every feat's brief tells the session to run `csdd graph analyze --strict` before
// declaring done, and the graph is a byte-stable BINARY blob the CLI rewrites from
// the whole workspace. Two feats in flight therefore always produce two different
// versions of a file git cannot merge — so with a squad, a conflict on it was not an
// unlucky collision between two feats that happened to touch the same code. It was
// the guaranteed outcome of the plan running at all.
//
// The real failure log named exactly this file first:
//
//	warning: Cannot merge binary files: docs/graph/graph.json.gz
//	CONFLICT (content): Merge conflict in docs/graph/graph.json.gz
//
// Before the fix this scenario produced three identical conflicts across four
// sessions costing $24.24 and delivered one of two feats. Integrate now settles a
// conflict whose every path is generated, so both feats land in one session each.
func TestDebugGeneratedArtifactNoLongerBlocksTheMerge(t *testing.T) {
	root, tr, out := debugRepo(t, debugPairPlan)
	// A NUL byte is what makes git treat the blob as binary and refuse to merge it,
	// which is the property that matters — not the gzip framing around it.
	graphBlob := func(feat string) string { return "\x1f\x8b\x08\x00" + feat + "\x00\x01\x02" }

	var gadgetTries int
	h := debugHooks(t, root, tr, func(req SessionRequest) (SessionOutcome, error) {
		slug := req.Feat.Slug
		artifacts := map[string]string{
			"docs/graph/graph.json.gz": graphBlob(slug),
			slug + ".go":               "package p // " + slug + "\n",
		}
		if slug == "gadget" {
			gadgetTries++
			if gadgetTries == 1 {
				// Both feats are cut from the same base; hold gadget until widget's
				// version of the graph is on it, so the merge order is fixed.
				if err := deliverInTree(req.Dir, slug, artifacts); err != nil {
					return SessionOutcome{}, err
				}
				if !tr.waitFor(trIntegrate, "widget", 30*time.Second) {
					tr.fail(fmt.Errorf("widget never merged; the scenario could not be sequenced"))
				}
				return paidOutcome(VerdictDone, 11.16), nil
			}
			// The retry rebuilds the graph and tries again — and, on a tree still cut
			// from the old base, conflicts exactly as before.
			if err := deliverInTree(req.Dir, slug, artifacts); err != nil {
				return SessionOutcome{}, err
			}
			return paidOutcome(VerdictDone, 4.22), nil
		}
		if err := deliverInTree(req.Dir, slug, artifacts); err != nil {
			return SessionOutcome{}, err
		}
		return paidOutcome(VerdictDone, 4.64), nil
	})

	sum, err := Run(RunOptions{
		Root: root, Slug: "p", SquadLimit: 2,
		// Bound the feat so a permanent conflict surfaces as `blocked` instead of
		// grinding to the iteration cap — which is precisely what the bound is for.
		FeatAttempts: 3,
		Hooks:        h, Out: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	tr.dump(t, root, "p", sum, out.String())

	conflicts := 0
	tr.mu.Lock()
	for _, e := range tr.events {
		if e.kind == trIntegrate && strings.Contains(e.detail, "CONFLICT") {
			conflicts++
		}
	}
	tr.mu.Unlock()
	if conflicts != 0 {
		t.Errorf("a conflict confined to generated artifacts must be settled by the keeper, got %d", conflicts)
	}
	if !sum.Completed || sum.Steps != 2 {
		t.Fatalf("both feats should land, one session each: completed=%v steps=%d (%s)",
			sum.Completed, sum.Steps, sum.Reason)
	}
	if sum.Sessions != 2 {
		t.Errorf("neither feat should need a retry, sessions=%d", sum.Sessions)
	}
	if sum.Gated != 0 {
		t.Errorf("no `done` should be handed back, gated=%d", sum.Gated)
	}
	// The merged base carries both feats' code, and the generated blob is whichever
	// one the base already had — stale on purpose, rebuilt by `csdd graph build`.
	for _, f := range []string{"widget.go", "gadget.go"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("%s must be on the run base: %v", f, err)
		}
	}
	if !strings.Contains(out.String(), "auto-resolved") {
		t.Error("the keeper settled a conflict on its own and must say so in the run log")
	}
	a := auditLedger(root, "p")
	t.Logf("FIXED: 0 conflicts, %d session(s), $%.2f, %d of 2 feats delivered "+
		"(was: 3 conflicts, 4 sessions, $24.24, 1 of 2)", sum.Sessions, a.totalUSD, sum.Steps)
	for _, e := range tr.errs {
		t.Errorf("%v", e)
	}
}

// TestDebugAuthoredConflictStillRollsBack is the other half of the fix, and the one
// that keeps it honest: the keeper settles a conflict only when EVERY path in it is
// generated. Two feats that genuinely disagree about a source file must still be
// handed back, because there is no version of that file the runner is entitled to
// choose.
//
// It also pins what the handoff says. A conflict that mixes a generated artifact
// with an authored one names only the authored file: the session is told to rebase,
// reads the list literally, and sending it to reconcile a blob it does not own is
// how a correct handoff becomes wasted work.
func TestDebugAuthoredConflictStillRollsBack(t *testing.T) {
	root, tr, out := debugRepo(t, debugPairPlan)
	// Both feats edit the same source file — a real disagreement — and both also
	// rebuild the graph, so the conflict is mixed.
	writeRepoFile(t, root, "shared.go", "package p\n\nconst Owner = \"none\"\n")
	commitAll(t, root, "add the contested file")

	var gadgetTries int
	h := debugHooks(t, root, tr, func(req SessionRequest) (SessionOutcome, error) {
		slug := req.Feat.Slug
		artifacts := map[string]string{
			"docs/graph/graph.json.gz": "\x1f\x8b\x08\x00" + slug + "\x00\x01\x02",
			"shared.go":                "package p\n\nconst Owner = \"" + slug + "\"\n",
		}
		if slug == "gadget" {
			gadgetTries++
			if err := deliverInTree(req.Dir, slug, artifacts); err != nil {
				return SessionOutcome{}, err
			}
			if gadgetTries == 1 && !tr.waitFor(trIntegrate, "widget", 30*time.Second) {
				tr.fail(fmt.Errorf("widget never merged; the scenario could not be sequenced"))
			}
			return paidOutcome(VerdictDone, 1), nil
		}
		if err := deliverInTree(req.Dir, slug, artifacts); err != nil {
			return SessionOutcome{}, err
		}
		return paidOutcome(VerdictDone, 1), nil
	})

	sum, err := Run(RunOptions{Root: root, Slug: "p", SquadLimit: 2, FeatAttempts: 2, Hooks: h, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	tr.dump(t, root, "p", sum, out.String())

	var details []string
	tr.mu.Lock()
	for _, e := range tr.events {
		if e.kind == trIntegrate && strings.Contains(e.detail, "CONFLICT") {
			details = append(details, e.detail)
		}
	}
	tr.mu.Unlock()
	if len(details) == 0 {
		t.Fatal("two feats writing different contents to the same source file must still conflict")
	}
	for _, d := range details {
		if !strings.Contains(d, "shared.go") {
			t.Errorf("the conflict must name the contested source file, got %q", d)
		}
		if strings.Contains(d, "docs/graph/") {
			t.Errorf("the handoff must not send the session after a generated artifact, got %q", d)
		}
	}
	if sum.Gated == 0 {
		t.Error("an authored conflict must be handed back as partial work")
	}
	if sum.Steps != 1 {
		t.Errorf("only the first feat can land on a contested file, steps=%d", sum.Steps)
	}
	// The base must not be left mid-merge by the rollback. Untracked runner state
	// (.csdd/, the journal) is not a merge leftover, so the check is specifically
	// for unmerged paths and an in-progress merge.
	if left, err := runGit(root, "diff", "--name-only", "--diff-filter=U"); err != nil || len(gitLines(left)) > 0 {
		t.Errorf("no path may be left unmerged after a rollback, got %q err=%v", left, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Errorf("the base must not be left mid-merge, MERGE_HEAD stat err = %v", err)
	}
	for _, e := range tr.errs {
		t.Errorf("%v", e)
	}
}

// TestDebugAttemptBoundStopsARunawayFeat proves the one guard that stands between a
// conflict loop and the iteration cap. `continue` resets the stall guard, so a feat
// whose `done` is refused forever is bounded only by FeatAttempts — and the trace
// should show it stopping there, not at 100 sessions.
func TestDebugAttemptBoundStopsARunawayFeat(t *testing.T) {
	root, tr, out := debugRepo(t, debugSoloPlan)
	h := debugHooks(t, root, tr, func(req SessionRequest) (SessionOutcome, error) {
		// Always claims done, never leaves the artifacts: the gate refuses every time.
		return paidOutcome(VerdictDone, 5.00), nil
	})

	sum, err := Run(RunOptions{Root: root, Slug: "p", FeatAttempts: 3, MaxIterations: 50, Hooks: h, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	tr.dump(t, root, "p", sum, out.String())

	if sum.Sessions != 3 {
		t.Errorf("the feat must be handed out exactly FeatAttempts times, got %d", sum.Sessions)
	}
	if len(sum.Blocked) != 1 || sum.Blocked[0] != "widget" {
		t.Errorf("the exhausted feat must be surfaced as blocked, got %v", sum.Blocked)
	}
	if sum.Outcome != OutcomeBlocked {
		t.Errorf("outcome should be blocked (%d), got %d", OutcomeBlocked, sum.Outcome)
	}
	if a := auditLedger(root, "p"); !nearUSD(a.totalUSD, 15.00) {
		t.Errorf("three refused sessions still cost three sessions, total=$%.2f", a.totalUSD)
	}
}
