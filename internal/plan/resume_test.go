package plan

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- spawn failures are absorbed, not charged to the feat (R1) ----------------

// TestSpawnFailureDoesNotConsumeAnAttempt is the regression for the defect the
// `violet` run exposed: eight `fork/exec` failures in a row spent a feat's whole
// attempt budget and surfaced it as `blocked` — "this feat cannot converge" —
// while its code sat finished on disk. A process that never started is not an
// attempt at anything.
func TestSpawnFailureDoesNotConsumeAnAttempt(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	calls := 0
	var slept []time.Duration
	h := baseHooks(t, root)
	h.Sleep = func(d time.Duration) { slept = append(slept, d) }
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat := req.Feat
		calls++
		// The first two spawns fail outright; the third runs and delivers.
		if calls <= 2 {
			return SessionOutcome{}, &SpawnError{Err: errString("fork/exec claude.exe: the filename or extension is too long")}
		}
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone, Summary: "shipped"}}, nil
	}

	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, FeatAttempts: 2, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	// FeatAttempts is 2 and there were 2 spawn failures: had they counted, feat `a`
	// would have been blocked before ever running.
	if len(sum.Blocked) != 0 {
		t.Fatalf("a spawn failure must not exhaust the attempt bound, got blocked=%v", sum.Blocked)
	}
	if sum.Failures != 0 {
		t.Errorf("a spawn failure is not a work failure, got Failures=%d", sum.Failures)
	}
	if len(slept) != 2 {
		t.Errorf("expected one backoff per failed spawn, got %d: %v", len(slept), slept)
	}
	// Backoff doubles, so a transient fault is cheap and a permanent one is not
	// retried at a constant hot rate.
	if len(slept) == 2 && slept[1] <= slept[0] {
		t.Errorf("backoff should grow between retries, got %v", slept)
	}

	// The record settles the `started` row as infra, so a later resume does not
	// read it as a crash — and does not count it either.
	var infra, started int
	for _, r := range LoadSessionRecords(root, "p") {
		switch r.Status {
		case SessionInfra:
			infra++
		case SessionStarted:
			started++
		}
	}
	if infra != 0 {
		t.Errorf("a spawn failure that later succeeded settles no infra row (the run recovered), got %d", infra)
	}
	if started == 0 {
		t.Errorf("every attempt should open a `started` row")
	}
}

// TestSpawnFailurePersistentEndsRun covers R1.4: five identical exec failures mean
// the environment is broken, so the run must end loudly instead of grinding every
// remaining feat into `blocked` against a `claude` that will not start.
func TestSpawnFailurePersistentEndsRun(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	calls := 0
	h := baseHooks(t, root)
	h.Sleep = func(time.Duration) {}
	h.Session = func(SessionRequest) (SessionOutcome, error) {
		calls++
		return SessionOutcome{}, &SpawnError{Err: errString("fork/exec claude.exe: the filename or extension is too long")}
	}

	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 50, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeSpawnFailed {
		t.Fatalf("a persistently failing spawn should end the run as OutcomeSpawnFailed, got %d (%s)", sum.Outcome, sum.Reason)
	}
	if calls != maxSpawnRetries {
		t.Errorf("expected exactly %d spawn attempts before giving up, got %d", maxSpawnRetries, calls)
	}
	if len(sum.Blocked) != 0 {
		t.Errorf("a broken environment must not manufacture blocked feats, got %v", sum.Blocked)
	}
	if !strings.Contains(sum.Reason, "environment is broken") {
		t.Errorf("the reason should name the environment, not the plan: %q", sum.Reason)
	}
	// The abandoned attempt is settled as infra so the record stays consistent.
	var infra int
	for _, r := range LoadSessionRecords(root, "p") {
		if r.Status == SessionInfra {
			infra++
		}
	}
	if infra != 1 {
		t.Errorf("the abandoned attempt should settle exactly one infra row, got %d", infra)
	}
}

// TestSpawnFailureCounterResetsOnRealSession pins that the counter measures "the
// exec path is broken right now", not "this run has had trouble" — otherwise a
// long run accumulating occasional transient failures would eventually abort on a
// perfectly healthy environment.
func TestSpawnFailureCounterResetsOnRealSession(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	calls := 0
	h := baseHooks(t, root)
	h.Sleep = func(time.Duration) {}
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat := req.Feat
		calls++
		// Fail, succeed, fail, succeed ... never maxSpawnRetries in a row.
		if calls%2 == 1 {
			return SessionOutcome{}, &SpawnError{Err: errString("transient")}
		}
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 20, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeComplete {
		t.Fatalf("alternating failures never reach %d consecutive, so the run should complete: %d (%s)",
			maxSpawnRetries, sum.Outcome, sum.Reason)
	}
}

// --- restoreRunState (R2, R3) -------------------------------------------------

// writeRecords lays down a synthetic sessions.jsonl, which is what lets the resume
// be tested against histories that would take a real run hours to produce.
func writeRecords(t *testing.T, root, slug string, recs ...SessionRecord) {
	t.Helper()
	for _, r := range recs {
		if r.At == "" {
			r.At = fixedNow().UTC().Format(time.RFC3339)
		}
		if err := AppendSessionRecord(root, slug, r); err != nil {
			t.Fatal(err)
		}
	}
}

func rec(feat string, iter, attempt int, status, detail string) SessionRecord {
	return SessionRecord{Feat: feat, Iteration: iter, Attempt: attempt, Status: status, Detail: detail}
}

func TestRestoreRunStateRebuildsAttemptsAndHandoff(t *testing.T) {
	root := t.TempDir()
	writeRecords(t, root, "p",
		rec("a", 1, 1, SessionStarted, ""),
		rec("a", 1, 1, SessionFailed, "compile error"),
		rec("a", 2, 2, SessionStarted, ""),
		rec("a", 2, 2, SessionContinue, "parser is in; wire the CLI next"),
		rec("b", 3, 1, SessionStarted, ""),
		rec("b", 3, 1, SessionDone, "shipped"),
	)

	st := restoreRunState(root, "p", 8, LoadLedger(root, "p"))

	if st.attempts["a"] != 2 {
		t.Errorf("feat a spent 2 attempts, got %d", st.attempts["a"])
	}
	if got := st.handoffs["a"]; got != "parser is in; wire the CLI next" {
		t.Errorf("the last continue's detail is the handoff, got %q", got)
	}
	if st.hists["a"] == nil || st.hists["a"].len() != 1 {
		t.Errorf("the failed attempt should seed the failure history")
	}
	// A delivered feat leaves no trail: it will never be handed out again.
	if _, ok := st.attempts["b"]; ok {
		t.Errorf("a delivered feat should be cleared, got attempts=%d", st.attempts["b"])
	}
	if len(st.crashed) != 0 {
		t.Errorf("every attempt settled, so nothing crashed, got %v", st.crashed)
	}
}

// TestRestoreRunStateCountsCrashedAttempt is the other half of the bound: an
// attempt that opened and never settled killed its host. It must still count, or a
// feat that reliably crashes the machine is retried forever.
func TestRestoreRunStateCountsCrashedAttempt(t *testing.T) {
	root := t.TempDir()
	writeRecords(t, root, "p",
		rec("a", 1, 1, SessionStarted, ""),
		rec("a", 1, 1, SessionContinue, "made progress"),
		rec("a", 2, 2, SessionStarted, ""), // the host died here: no settling row
	)

	st := restoreRunState(root, "p", 8, LoadLedger(root, "p"))

	if st.attempts["a"] != 2 {
		t.Errorf("the crashed attempt still counts, want 2 got %d", st.attempts["a"])
	}
	if len(st.crashed) != 1 || st.crashed[0] != "a" {
		t.Errorf("the crash should be attributed to feat a, got %v", st.crashed)
	}
	if s := st.resumeSummary(); !strings.Contains(s, "died mid-session") {
		t.Errorf("the resume summary should surface the crash: %q", s)
	}
}

// TestRestoreRunStateInfraRowGivesTheAttemptBack pins that the two halves agree:
// executeFeat gives an attempt back when the spawn fails, and the resume must
// reach the same number from the record alone.
func TestRestoreRunStateInfraRowGivesTheAttemptBack(t *testing.T) {
	root := t.TempDir()
	writeRecords(t, root, "p",
		rec("a", 1, 1, SessionStarted, ""),
		rec("a", 1, 1, SessionInfra, "spawn failed"),
	)
	st := restoreRunState(root, "p", 8, LoadLedger(root, "p"))
	if st.attempts["a"] != 0 {
		t.Errorf("a spawn failure is not an attempt, got %d", st.attempts["a"])
	}
	if len(st.crashed) != 0 {
		t.Errorf("an infra row settles its started row, got crashed=%v", st.crashed)
	}
}

// TestRestoreRunStateDerivesBlocked covers the derivation rather than storage of
// the blocked set: a stale stored flag could outlive the fact it describes.
func TestRestoreRunStateDerivesBlocked(t *testing.T) {
	root := t.TempDir()
	var recs []SessionRecord
	for i := 1; i <= 3; i++ {
		recs = append(recs, rec("a", i, i, SessionStarted, ""), rec("a", i, i, SessionContinue, "still going"))
	}
	writeRecords(t, root, "p", recs...)

	if st := restoreRunState(root, "p", 3, LoadLedger(root, "p")); !st.blocked["a"] {
		t.Errorf("3 attempts against a bound of 3 is blocked")
	}
	if st := restoreRunState(root, "p", 4, LoadLedger(root, "p")); st.blocked["a"] {
		t.Errorf("3 attempts against a bound of 4 is not blocked")
	}

	// A feat the ledger marks done is never blocked, however many attempts it took.
	l := LoadLedger(root, "p")
	l.MarkDone("a", "shipped", fixedNow())
	if err := l.Save(root, "p"); err != nil {
		t.Fatal(err)
	}
	if st := restoreRunState(root, "p", 3, LoadLedger(root, "p")); st.blocked["a"] {
		t.Errorf("a delivered feat must never be blocked")
	}
}

// TestRestoreRunStateReadsPreStartedRecords keeps an existing sessions.jsonl
// readable: rows written before `started` existed have no opening row, and must
// still be counted from the settling row alone.
func TestRestoreRunStateReadsPreStartedRecords(t *testing.T) {
	root := t.TempDir()
	writeRecords(t, root, "p",
		rec("a", 1, 1, SessionFailed, "boom"),
		rec("a", 2, 2, SessionContinue, "handoff text"),
	)
	st := restoreRunState(root, "p", 8, LoadLedger(root, "p"))
	if st.attempts["a"] != 2 {
		t.Errorf("legacy records must still count, want 2 got %d", st.attempts["a"])
	}
	if st.handoffs["a"] != "handoff text" {
		t.Errorf("legacy handoff lost: %q", st.handoffs["a"])
	}
}

func TestRestoreRunStateMissingFileIsEmpty(t *testing.T) {
	st := restoreRunState(t.TempDir(), "p", 8, &Ledger{})
	if len(st.attempts) != 0 || len(st.handoffs) != 0 || st.resumeSummary() != "" {
		t.Errorf("a missing record must yield an empty state, got %+v", st)
	}
}

func TestRestoreRunStateFindsFailureLog(t *testing.T) {
	root := t.TempDir()
	writeRecords(t, root, "p", rec("a", 1, 1, SessionStarted, ""), rec("a", 1, 1, SessionFailed, "boom"))
	rel := failureLogRel("p", "a")
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("prior output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := restoreRunState(root, "p", 8, LoadLedger(root, "p")).logs["a"]; got != rel {
		t.Errorf("the failure log should be found by its deterministic path, want %q got %q", rel, got)
	}
}

// --- end to end: a killed run resumes where it stopped ------------------------

// TestRunResumesAfterInterruption is the behavior all of the above exists for. The
// first run is cut off mid-feat; the second must know how many attempts the feat
// has already spent and must carry the handoff into the next brief, rather than
// starting cold with a fresh budget.
func TestRunResumesAfterInterruption(t *testing.T) {
	root := approvedRunnerWorkspace(t)

	// Run one: two `continue`s on feat `a`, then the iteration cap cuts it off.
	h := baseHooks(t, root)
	h.Session = func(SessionRequest) (SessionOutcome, error) {
		return SessionOutcome{Verdict: Verdict{Status: VerdictContinue, Summary: "parser is in"}}, nil
	}
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 2, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}

	// Run two: a fresh process. Nothing is in memory; everything comes from disk.
	var briefs []string
	h2 := baseHooks(t, root)
	h2.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat, brief := req.Feat, req.Brief
		briefs = append(briefs, brief)
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone, Summary: "shipped"}}, nil
	}
	var out bytes.Buffer
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h2, MaxIterations: 10, Out: &out}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "resumed:") {
		t.Errorf("a resumed run should say so: %s", out.String())
	}
	// And durably, not only on a terminal nobody was watching: the journal is the
	// half of the record that survives the run.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| resumed") {
		t.Errorf("the resume should be journaled: %s", logData)
	}
	if len(briefs) == 0 {
		t.Fatal("the resumed run spawned no session")
	}
	if !strings.Contains(briefs[0], "parser is in") {
		t.Errorf("the predecessor's handoff must reach the next brief, got:\n%s", briefs[0])
	}
}

// TestRunResumeCarriesAttemptBudget proves the bound is not silently reset by an
// interruption: without the resume, a feat could burn its budget repeatedly, once
// per crash, and never be surfaced as blocked.
func TestRunResumeCarriesAttemptBudget(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	spend := func(iterations int) RunSummary {
		h := baseHooks(t, root)
		h.Session = func(SessionRequest) (SessionOutcome, error) {
			return SessionOutcome{Verdict: Verdict{Status: VerdictContinue, Summary: "still going"}}, nil
		}
		sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: iterations, FeatAttempts: 4, Out: &bytes.Buffer{}})
		if err != nil {
			t.Fatal(err)
		}
		return sum
	}
	// Two runs of two iterations each: four attempts total against a bound of four.
	if sum := spend(2); len(sum.Blocked) != 0 {
		t.Fatalf("two attempts should not exhaust a bound of four, got %v", sum.Blocked)
	}
	sum := spend(2)
	if len(sum.Blocked) == 0 {
		t.Errorf("the attempt budget must survive the interruption: 2+2 attempts against a bound of 4 is blocked, got %+v", sum)
	}
}

// TestRunJournalsCrashedAttemptByName covers R2.2's durable half. A crashed
// attempt is counted whether or not anyone is watching stdout, so the feat it
// belongs to has to reach log.md — "something crashed" is not actionable, and a
// budget partly spent with no recorded reason is exactly the confusion the journal
// exists to prevent.
func TestRunJournalsCrashedAttemptByName(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	// An attempt that opened and never settled: the host died mid-session.
	writeRecords(t, root, "p", rec("a", 1, 1, SessionStarted, ""))

	h := baseHooks(t, root)
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat := req.Feat
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}

	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	got := string(logData)
	if !strings.Contains(got, "died mid-session and still counting: a") {
		t.Errorf("the crashed feat should be named in the journal: %s", got)
	}
}

// --- diagnostics (R4) ---------------------------------------------------------

// TestSessionFailureContextNamesTheInputs pins the annotation that exists because
// ten failures in the `violet` run read only `exit status 1:` with an empty
// stderr — not enough to tell a rejected prompt from a child that died before it
// opened its stream.
func TestSessionFailureContextNamesTheInputs(t *testing.T) {
	got := sessionFailureContext([]string{"-p", "--verbose"}, "brief body", newSessionStream(fixedNow), "")
	for _, want := range []string{"stream events=0", "stdout=0B", "argv=", "brief=10B"} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics missing %q: %s", want, got)
		}
	}
	// The brief itself must never be echoed — only its size.
	if strings.Contains(got, "brief body") {
		t.Errorf("diagnostics must not echo the brief: %s", got)
	}
}

func TestSpawnErrorUnwraps(t *testing.T) {
	inner := errString("fork/exec: too long")
	err := fmt.Errorf("wrapped: %w", &SpawnError{Err: inner})
	var spawn *SpawnError
	if !errors.As(err, &spawn) {
		t.Fatal("a wrapped SpawnError must still be recognizable")
	}
	if !errors.Is(err, inner) {
		t.Error("SpawnError must unwrap to the underlying exec error")
	}
}
