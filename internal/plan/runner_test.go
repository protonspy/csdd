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

// deliverSpec writes exactly what a session must leave on disk for the verdict
// gate to let its `done` stand (R10.1): a spec approved through all three phases
// and flagged ready, a tasks.md whose every box is `[x]`, and green recorded test
// evidence with no open attentions.
func deliverSpec(t *testing.T, root, slug string) {
	t.Helper()
	writeSpec(t, root, slug,
		map[string]bool{"requirements": true, "design": true, "tasks": true}, true,
		"# Tasks\n\n- [x] 1. Deliver the behavior\n      _Requirements: 1.1_\n")
	writeFile(t, filepath.Join(root, "specs", slug, "test-report.json"),
		`{"feature":"`+slug+`","updatedAt":"2026-07-07T00:00:00Z",`+
			`"command":"go test ./...","tests":{"total":3,"passed":3,"failed":0,"skipped":0}}`)
}

// deliveringSession is a Session hook that does what a real plan-dev session does
// before declaring done: it leaves the feat's artifacts on disk, THEN returns the
// verdict. Sessions that skip that step now have their `done` refused, which is
// the entire point of the gate — so a test that expects a feat to be delivered
// has to produce the evidence, exactly as the real loop demands.
func deliveringSession(t *testing.T, root string) func(SessionRequest) (SessionOutcome, error) {
	return func(req SessionRequest) (SessionOutcome, error) {
		f := req.Feat
		deliverSpec(t, root, f.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
}

// doneOutcome is the bare `done` verdict, with no artifacts written — the shape
// the gate is designed to refuse.
func doneOutcome(summary string) SessionOutcome {
	return SessionOutcome{Verdict: Verdict{Status: VerdictDone, Summary: summary}}
}

// rootTrees is the loop tests' treeKeeper: every feat "works" in the workspace root
// itself, and integration is a no-op.
//
// Injecting it is what keeps these tests about the LOOP — verdicts, the ledger, the
// stall guard, the attempt bound — rather than about git. It also keeps them honest
// about what they assert: a fixture that writes a spec to the root and a gate that
// reads the root are describing the same tree, which is exactly the serial world
// these tests were written for. Worktree isolation and the merge have their own
// tests, against a real repository (tree_test.go).
type rootTrees struct{ root string }

func (t rootTrees) Ensure(string) (string, error) { return t.root, nil }
func (t rootTrees) Integrate(string) error        { return nil }
func (t rootTrees) Discard(string) error          { return nil }

// baseHooks returns hooks whose session delivers each feat properly and declares
// `done`, so a fresh plan runs to completion (one session per feat). The runner
// spawns nothing but the Session hook — there is no csdd seam, because the loop
// owns only the ledger, the verdict gate's three file reads, and the worktree each
// feat is handed.
func baseHooks(t *testing.T, root string) Hooks {
	return Hooks{
		Session:         deliveringSession(t, root),
		Doctor:          func() SandboxReport { return SandboxReport{OK: true} },
		Confirm:         func(string) bool { return false },
		ClaudeAvailable: func() bool { return true },
		Now:             fixedNow,
		Sleep:           func(time.Duration) {},
		Trees:           rootTrees{root},
	}
}

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain done", `{"status":"done","summary":"ok"}`, VerdictDone, false},
		{"continue", `{"status":"continue","summary":"half the parser is in; wire the CLI next"}`, VerdictContinue, false},
		{"envelope", `{"type":"result","result":"{\"status\":\"done\",\"summary\":\"ok\"}"}`, VerdictDone, false},
		{"envelope prose+json", `{"result":"Here is my verdict: {\"status\":\"continue\",\"summary\":\"x\"}"}`, VerdictContinue, false},
		{"uppercase", `{"status":"DONE","summary":"ok"}`, VerdictDone, false},
		{"garbage", `not json at all`, "", true},
		{"bad status", `{"status":"halt","summary":"?"}`, "", true},
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
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: baseHooks(t, root), Out: &bytes.Buffer{}}); err == nil {
		t.Errorf("expected preflight failure for unapproved plan")
	}

	// Approved but claude missing.
	root2 := approvedRunnerWorkspace(t)
	h := baseHooks(t, root2)
	h.ClaudeAvailable = func() bool { return false }
	if _, err := Run(RunOptions{Root: root2, Slug: "p", Hooks: h, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "claude") {
		t.Errorf("expected claude-missing failure, got %v", err)
	}

	// Drift after approval: preflight refuses.
	root3 := approvedRunnerWorkspace(t)
	writeFile(t, filepath.Join(Dir(root3, "p"), "plan.md"), runnerPlan+"\n<!-- edit -->\n")
	if _, err := Run(RunOptions{Root: root3, Slug: "p", Hooks: baseHooks(t, root3), Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Errorf("expected drift preflight failure, got %v", err)
	}

	// Doctor fails and the human declines the alert: the run closes.
	h2 := baseHooks(t, root2)
	h2.Doctor = func() SandboxReport {
		return SandboxReport{OK: false, Checks: []SandboxCheck{{Name: "firewall_active", OK: false, Detail: "control reachable"}}}
	}
	confirms := 0
	h2.Confirm = func(string) bool { confirms++; return false }
	if _, err := Run(RunOptions{Root: root2, Slug: "p", Hooks: h2, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("expected the run to close on a declined unverified-sandbox alert, got %v", err)
	}
	if confirms != 1 {
		t.Errorf("expected exactly one confirmation prompt, got %d", confirms)
	}

	// Doctor fails but the human accepts: the run proceeds bypass-mode.
	var out bytes.Buffer
	h3 := baseHooks(t, root2)
	h3.Doctor = func() SandboxReport { return SandboxReport{OK: false} }
	h3.Confirm = func(string) bool { return true }
	if _, err := Run(RunOptions{Root: root2, Slug: "p", Hooks: h3, MaxIterations: 1, Out: &out}); err != nil {
		t.Errorf("accepted alert should let the run proceed, got %v", err)
	}
	if !strings.Contains(out.String(), "WITHOUT") {
		t.Errorf("run log should state it is continuing without a verified sandbox:\n%s", out.String())
	}

	// Doctor fails with --yes: no prompt at all, the run proceeds.
	h4 := baseHooks(t, root2)
	h4.Doctor = func() SandboxReport { return SandboxReport{OK: false} }
	h4.Confirm = func(string) bool { t.Error("Confirm must not be called when AssumeYes is set"); return false }
	if _, err := Run(RunOptions{Root: root2, Slug: "p", AssumeYes: true, Hooks: h4, MaxIterations: 1, Out: &bytes.Buffer{}}); err != nil {
		t.Errorf("--yes should skip the prompt and proceed, got %v", err)
	}
}

// TestRunnerCompletesAllFeats: a session that declares done for each feat drives
// the plan to completion, one session per feat, recording each in the ledger.
func TestRunnerCompletesAllFeats(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: baseHooks(t, root), MaxIterations: 10, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed || sum.Outcome != OutcomeComplete {
		t.Fatalf("expected completion, got %+v", sum)
	}
	if sum.Sessions != 2 || sum.Steps != 2 {
		t.Errorf("expected 2 sessions / 2 feats delivered, got %+v", sum)
	}
	// The ledger records both feats.
	l := LoadLedger(root, "p")
	if !l.Done("a") || !l.Done("b") {
		t.Errorf("ledger should mark both feats done: %+v", l.Feats)
	}
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | done") || !strings.Contains(string(logData), "| b | done") {
		t.Errorf("both feats should be journaled done: %s", logData)
	}
}

// TestRunnerContinueHandsOff: a `continue` verdict is honest partial work — its
// summary becomes the successor's handoff, the same feat comes back, and a feat
// bigger than one context window still converges.
func TestRunnerContinueHandsOff(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	var briefs []string
	calls := 0
	h := baseHooks(t, root)
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat, brief := req.Feat, req.Brief
		briefs = append(briefs, brief)
		calls++
		if calls == 1 {
			if feat.Slug != "a" {
				t.Errorf("first session should be feat a, got %s", feat.Slug)
			}
			return SessionOutcome{Verdict: Verdict{Status: VerdictContinue, Summary: "parser is in; the CLI wiring remains"}}, nil
		}
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed {
		t.Fatalf("continue then done should complete, got %+v", sum)
	}
	// Session 2 works feat a again and reads the handoff.
	if !strings.Contains(briefs[1], "Handoff from the previous session") || !strings.Contains(briefs[1], "the CLI wiring remains") {
		t.Errorf("the second session should read the handoff:\n%s", briefs[1])
	}
	if sum.Failures != 0 {
		t.Errorf("continue is not a failure, got %d", sum.Failures)
	}
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | progress") {
		t.Errorf("continue not journaled as progress: %s", logData)
	}
}

// TestRunnerSessionErrorStalls: repeated session errors with no progress trip the
// stall guard, and the failure trail lands on disk for the next run.
func TestRunnerSessionErrorStalls(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	h := baseHooks(t, root)
	h.Session = func(SessionRequest) (SessionOutcome, error) {
		return SessionOutcome{}, errString("boom: compiler exploded")
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 20, Stall: 3, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeStalled {
		t.Fatalf("expected OutcomeStalled, got %d (%s)", sum.Outcome, sum.Reason)
	}
	if sum.Failures < 3 {
		t.Errorf("expected at least 3 failed iterations, got %d", sum.Failures)
	}
	// The failure log for feat a exists and carries the error.
	logPath := filepath.Join(root, ".csdd", "plan", "p", "failures", "a.log")
	data, rerr := os.ReadFile(logPath)
	if rerr != nil {
		t.Fatalf("expected a failure log at %s: %v", logPath, rerr)
	}
	if !strings.Contains(string(data), "compiler exploded") {
		t.Errorf("failure log should carry the session error: %s", data)
	}
}

// TestRunnerContinueRunsToCap: a session that only ever reports `continue` makes
// honest progress every iteration (never stalls) but never finishes, so the run
// ends at the iteration cap — the wallet guard, not the stall guard.
func TestRunnerContinueRunsToCap(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	h := baseHooks(t, root)
	h.Session = func(SessionRequest) (SessionOutcome, error) {
		return SessionOutcome{Verdict: Verdict{Status: VerdictContinue, Summary: "still going"}}, nil
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 3, Stall: 2, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeCapped {
		t.Errorf("expected OutcomeCapped (continue never stalls), got %d (%s)", sum.Outcome, sum.Reason)
	}
	if sum.Sessions != 3 {
		t.Errorf("expected 3 sessions before the cap, got %d", sum.Sessions)
	}
}

// TestRunnerLedgerResumes: a feat already recorded in the ledger is skipped, so a
// resumed run only spends sessions on what remains.
func TestRunnerLedgerResumes(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	l := LoadLedger(root, "p")
	l.MarkDone("a", "delivered earlier", fixedNow())
	if err := l.Save(root, "p"); err != nil {
		t.Fatal(err)
	}
	var worked []string
	h := baseHooks(t, root)
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat := req.Feat
		worked = append(worked, feat.Slug)
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed || sum.Sessions != 1 {
		t.Fatalf("resume should spend one session on b only, got %+v", sum)
	}
	if len(worked) != 1 || worked[0] != "b" {
		t.Errorf("only feat b should have been worked, got %v", worked)
	}
}

// TestRunnerReconcilesDiskDelivered: a feat delivered on disk before the run (all
// phases approved, all tasks checked) but absent from the ledger is imported at
// startup, so the loop spends no session on it and journals it reconciled. This is
// the resume path for a plan advanced by hand in a session.
func TestRunnerReconcilesDiskDelivered(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	// Feat a is fully delivered on disk; the ledger knows nothing about it.
	allApproved := map[string]bool{"requirements": true, "design": true, "tasks": true}
	writeSpec(t, root, "a", allApproved, true, "- [x] 1. done\n")

	var worked []string
	h := baseHooks(t, root)
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat := req.Feat
		worked = append(worked, feat.Slug)
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed || sum.Sessions != 1 {
		t.Fatalf("disk-delivered a should be reconciled, spending one session on b only, got %+v", sum)
	}
	if len(worked) != 1 || worked[0] != "b" {
		t.Errorf("only feat b should have been worked; a was already delivered on disk, got %v", worked)
	}
	if l := LoadLedger(root, "p"); !l.Done("a") {
		t.Errorf("reconcile should have recorded a in the ledger")
	}
	// One aggregate entry naming the feats, not one entry per feat: a plan whose
	// transient ledger is lost re-reconciles everything on the next run, and a line
	// each would bury the handoffs and failures the journal exists to preserve.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "1 feat(s) already delivered on disk: a") {
		t.Errorf("reconciliation should be journaled once, naming the feats: %s", logData)
	}
	if n := strings.Count(string(logData), "| reconciled"); n != 1 {
		t.Errorf("expected exactly one reconciliation entry, got %d: %s", n, logData)
	}
}

func TestRunnerJournalFormat(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	h := baseHooks(t, root)
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat := req.Feat
		deliverSpec(t, root, feat.Slug)
		return doneOutcome("shipped the thing"), nil
	}
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "## [2026-07-07] - | a | done") {
		t.Errorf("journal line format wrong: %s", logData)
	}
	if !strings.Contains(string(logData), "shipped the thing") {
		t.Errorf("done journal should carry the session summary: %s", logData)
	}
}

// TestDetectLimit covers recognizing the account-limit notice and resolving the
// reset moment from its "resets 10:10pm (America/Sao_Paulo)" clause.
func TestDetectLimit(t *testing.T) {
	// Not a limit notice.
	if _, ok := detectLimit("claude session failed: exit status 1"); ok {
		t.Errorf("a plain failure must not be read as a limit")
	}

	// Full notice with a parsable reset in an explicit zone.
	lim, ok := detectLimit("You've hit your session limit · resets 10:10pm (America/Sao_Paulo)")
	if !ok {
		t.Fatal("expected the notice to be detected as a limit")
	}
	if !lim.known || lim.Hour != 22 || lim.Minute != 10 {
		t.Errorf("expected reset 22:10, got %+v", lim)
	}
	// now is before 22:10 São Paulo, so the reset is the same day.
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	now := time.Date(2026, 7, 10, 15, 0, 0, 0, loc)
	reset, ok := lim.Reset(now)
	if !ok {
		t.Fatal("expected a resolvable reset time")
	}
	if reset.Hour() != 22 || reset.Minute() != 10 || reset.Day() != 10 {
		t.Errorf("reset should be same-day 22:10, got %s", reset)
	}
	// A reset already past today rolls to tomorrow.
	if r2, _ := lim.Reset(time.Date(2026, 7, 10, 23, 0, 0, 0, loc)); r2.Day() != 11 {
		t.Errorf("a past reset should roll to tomorrow, got %s", r2)
	}

	// A notice with no parsable reset still detects, but Reset falls back.
	bare, ok := detectLimit("You've hit your usage limit. Try again later.")
	if !ok || bare.known {
		t.Errorf("bare notice should detect without a known reset, got ok=%v %+v", ok, bare)
	}
	if _, ok := bare.Reset(fixedNow()); ok {
		t.Errorf("an unparsed reset must report ok=false")
	}
}

// TestRunnerLimitSleepsAndResumes: a session-limit stop is not a failure — the
// runner sleeps until the reset and retries the SAME feat, so the run completes
// without incrementing failures or the stall guard, and no failure log is written.
func TestRunnerLimitSleepsAndResumes(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	var slept []time.Duration
	calls := 0
	h := baseHooks(t, root)
	h.Sleep = func(d time.Duration) { slept = append(slept, d) }
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		feat := req.Feat
		calls++
		// Feat a hits the limit twice, then delivers; feat b delivers directly.
		if feat.Slug == "a" && calls <= 2 {
			return SessionOutcome{}, &LimitError{Raw: "hit your session limit", Hour: 22, Minute: 10, known: true}
		}
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, Stall: 2, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed || sum.Outcome != OutcomeComplete {
		t.Fatalf("limit stops should not fail the run, got %+v (%s)", sum, sum.Reason)
	}
	if sum.Failures != 0 {
		t.Errorf("a limit stop is not a failure, got %d", sum.Failures)
	}
	if sum.Sessions != 2 {
		t.Errorf("limit retries are not counted as sessions; expected 2, got %d", sum.Sessions)
	}
	if len(slept) != 2 {
		t.Errorf("expected two limit sleeps, got %d: %v", len(slept), slept)
	}
	for _, d := range slept {
		if d < limitMinWait {
			t.Errorf("every wait must be floored at %s, got %s", limitMinWait, d)
		}
	}
	// The limit stop must not pollute the failure log.
	if _, err := os.Stat(filepath.Join(root, ".csdd", "plan", "p", "failures", "a.log")); err == nil {
		t.Errorf("a limit stop must not write a failure log")
	}
	if !strings.Contains(out.String(), "session limit") {
		t.Errorf("the run log should announce the limit pause:\n%s", out.String())
	}
}

// errString is a tiny error type so the test needs no fmt/errors import churn.
type errString string

func (e errString) Error() string { return string(e) }
