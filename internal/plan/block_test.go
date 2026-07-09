package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// failingGates returns hooks whose session always authors the change (checking the
// task box) and whose gate fails until the nth call, so a test can decide exactly
// which iteration goes green. briefs captures what each session actually read.
func failingGates(t *testing.T, root string, passOnCall int, briefs *[]string) Hooks {
	t.Helper()
	h := baseHooks()
	h.Session = func(_ Step, brief string, _ float64) (Verdict, error) {
		*briefs = append(*briefs, brief)
		writeFile(t, filepath.Join(root, "specs", "a", "tasks.md"), "- [x] 1. do it\n")
		return Verdict{Status: VerdictDone}, nil
	}
	calls := 0
	h.Gate = func(string, string) (bool, string) {
		calls++
		if passOnCall > 0 && calls >= passOnCall {
			return true, ""
		}
		return false, "FAIL attempt " + itoa(calls) + ": expected 3 got 4"
	}
	return h
}

// oneOpenFeat approves the plan, leaves feat a with one unchecked task, and marks
// feat b done — so the run has exactly one feat to work.
func oneOpenFeat(t *testing.T) string {
	t.Helper()
	root := approvedRunnerWorkspace(t)
	writeSpec(t, root, "a", allApproved, true, "- [ ] 1. do it\n")
	writeSpec(t, root, "b", allApproved, true, "- [x] 1. done\n")
	return root
}

// TestFailureContextAccumulatesAcrossIterations pins the loop's self-correction
// seam: there is no inner retry and no repair budget — the next iteration IS the
// retry, and it reads the whole trail of the attempts before it.
func TestFailureContextAccumulatesAcrossIterations(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	// Iterations 1 and 2 fail the gate; iteration 3 goes green.
	h := failingGates(t, root, 3, &briefs)

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed {
		t.Fatalf("the loop should self-correct to completion, got %+v", sum)
	}
	if sum.Failures != 2 || sum.Sessions != 3 {
		t.Errorf("expected 2 failed iterations across 3 sessions, got %+v", sum)
	}
	if len(briefs) != 3 {
		t.Fatalf("expected 3 session briefs, got %d", len(briefs))
	}
	if strings.Contains(briefs[0], "Autonomous run context") {
		t.Errorf("a first attempt has no trail to carry:\n%s", briefs[0])
	}
	for _, want := range []string{"Autonomous run context", "Attempt 1", "FAIL attempt 1"} {
		if !strings.Contains(briefs[1], want) {
			t.Errorf("iteration 2 should read attempt 1's failure, missing %q", want)
		}
	}
	for _, want := range []string{"failed 2 time(s)", "Attempt 1", "Attempt 2", "FAIL attempt 1", "FAIL attempt 2", "ROOT CAUSE"} {
		if !strings.Contains(briefs[2], want) {
			t.Errorf("iteration 3 should read the whole trail, missing %q", want)
		}
	}
	if _, blocked := ReadBlock(root, "p", "a"); blocked {
		t.Errorf("a step that advanced must not leave a marker")
	}
}

// TestStallGuardParksTheRunWithItsEvidence: a step that never converges ends the
// run early (before the iteration cap) with a typed marker pointing at the full
// failure log — the wallet guard is the cap, the convergence guard is the stall.
func TestStallGuardParksTheRunWithItsEvidence(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	h := failingGates(t, root, 0, &briefs) // never passes

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 50, Stall: 3, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeStalled {
		t.Fatalf("expected OutcomeStalled, got %d (%s)", sum.Outcome, sum.Reason)
	}
	if sum.Sessions != 3 {
		t.Errorf("the stall guard should stop after exactly 3 sessions, got %d", sum.Sessions)
	}
	b, blocked := ReadBlock(root, "p", "a")
	if !blocked {
		t.Fatal("a stalled run must leave a marker explaining where it was stuck")
	}
	if b.Kind != BlockGateFailure || !b.Mechanical() {
		t.Errorf("a stall over gate failures is a mechanical marker, got kind %q", b.Kind)
	}
	if b.Attempts != 3 {
		t.Errorf("expected 3 attempts on the marker, got %d", b.Attempts)
	}
	if b.Step != "task 1" {
		t.Errorf("marker should name the step it stalled on, got %q", b.Step)
	}
	if !strings.Contains(b.Reason, "stalled") {
		t.Errorf("marker should say it stalled, got %q", b.Reason)
	}

	// The failure log survives with every attempt's untruncated output.
	if b.Log == "" {
		t.Fatal("marker should point at a failure log")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(b.Log)))
	if err != nil {
		t.Fatalf("failure log not written at %s: %v", b.Log, err)
	}
	for _, want := range []string{"attempt 1", "attempt 2", "attempt 3", "FAIL attempt 1", "FAIL attempt 3"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("failure log missing %q:\n%s", want, data)
		}
	}

	// The session checked the box; the gates refuted it. Leaving it checked would
	// make the feat read as done the moment the next run clears the marker.
	tasks, _ := os.ReadFile(filepath.Join(root, "specs", "a", "tasks.md"))
	if got := strings.TrimSpace(string(tasks)); got != "- [ ] 1. do it" {
		t.Errorf("a refuted task claim must be retracted, got %q", got)
	}

	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "blocked (gate-failure)") {
		t.Errorf("the stall marker should be journaled with its kind: %s", logData)
	}
}

// TestNextRunRetriesEverythingAndReadsTheOldLog: markers never survive a run
// start — the loop retries every feat — and the first session on a previously-
// failed step is pointed at the failure log its predecessor left.
func TestNextRunRetriesEverythingAndReadsTheOldLog(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string

	// Run 1: never passes, stalls quickly, leaves the marker + log.
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: failingGates(t, root, 0, &briefs), MaxIterations: 10, Stall: 2, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if _, blocked := ReadBlock(root, "p", "a"); !blocked {
		t.Fatal("run 1 should have left a marker")
	}

	// Run 2: the first gate passes — the run recovers with no human in the loop.
	var briefs2 []string
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: failingGates(t, root, 1, &briefs2), MaxIterations: 10, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if len(briefs2) == 0 {
		t.Fatal("run 2 should have retried the parked feat")
	}
	if !strings.Contains(briefs2[0], "failure log") || !strings.Contains(briefs2[0], "failures/a/task-1.log") {
		t.Errorf("the first session should be pointed at the previous run's log:\n%s", briefs2[0])
	}
	if !sum.Completed {
		t.Errorf("run 2 should complete, got %+v (%s)", sum, out.String())
	}
	if !strings.Contains(out.String(), "↻ retrying a") {
		t.Errorf("run 2 should announce the retry:\n%s", out.String())
	}
	if _, blocked := ReadBlock(root, "p", "a"); blocked {
		t.Errorf("a feat that advanced must not stay blocked")
	}
	// The journal carries both halves: the unblock and the eventual done.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | unblocked") || !strings.Contains(string(logData), "autonomous loop retries every feat") {
		t.Errorf("the startup clear should be journaled: %s", logData)
	}
}

// TestStartupClearsMarkersOfEveryKind: even a deviation left by an older csdd is
// retried — in the autonomous loop, decisions are the session's to make, so no
// marker kind parks a feat anymore.
func TestStartupClearsMarkersOfEveryKind(t *testing.T) {
	root := oneOpenFeat(t)
	hash, _ := HashPlan(root, "p")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(WriteBlock(root, "p", Block{Feat: "a", Step: "task 1", Kind: BlockDeviation,
		Reason: "session blocked: needs a queue", Revision: "split feat a", PlanHash: hash}))

	h := baseHooks()
	sessions := 0
	h.Session = func(Step, string, float64) (Verdict, error) {
		sessions++
		writeFile(t, filepath.Join(root, "specs", "a", "tasks.md"), "- [x] 1. do it\n")
		return Verdict{Status: VerdictDone}, nil
	}
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if sessions == 0 {
		t.Fatal("the feat behind an old deviation marker must be retried")
	}
	if !sum.Completed {
		t.Errorf("run should complete, got %+v", sum)
	}
	if !strings.Contains(out.String(), "was blocked [deviation]") {
		t.Errorf("the retry should say what it cleared:\n%s", out.String())
	}
}

// TestLegacyBlockedVerdictIsCoachedNotParked: the legacy `blocked` verdict no
// longer ends a feat. It fails the iteration, and the next session on that step
// is told the decision is its own to make and record.
func TestLegacyBlockedVerdictIsCoachedNotParked(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	h := baseHooks()
	calls := 0
	h.Session = func(_ Step, brief string, _ float64) (Verdict, error) {
		briefs = append(briefs, brief)
		calls++
		if calls == 1 {
			return Verdict{Status: VerdictBlocked, Summary: "no frontend linter in stack.md", Revision: "add a stack row"}, nil
		}
		// The coached session decides, records, and finishes the step.
		writeFile(t, filepath.Join(root, "specs", "a", "tasks.md"), "- [x] 1. do it\n")
		return Verdict{Status: VerdictDone, Summary: "done", Decisions: []string{"Frontend lint = ESLint — Vite react-ts default"}}, nil
	}
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed {
		t.Fatalf("the coached loop should complete, got %+v (%s)", sum, sum.Reason)
	}
	if sum.Decisions != 1 {
		t.Errorf("the recorded decision should be counted, got %d", sum.Decisions)
	}
	if len(briefs) < 2 || !strings.Contains(briefs[1], "MAKE and RECORD") || !strings.Contains(briefs[1], "no frontend linter") {
		t.Errorf("the second session should be coached with the objection:\n%s", briefs[len(briefs)-1])
	}
	if _, blocked := ReadBlock(root, "p", "a"); blocked {
		t.Errorf("a legacy blocked verdict must not leave a marker")
	}
	// Both the failure and the decision are journaled.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	for _, want := range []string{"legacy blocked verdict", "| a | decision", "Frontend lint = ESLint"} {
		if !strings.Contains(string(logData), want) {
			t.Errorf("journal missing %q: %s", want, logData)
		}
	}
}

// TestHaltVerdictEndsTheRunWithATypedMarker: halt is the session's one legitimate
// way to end the loop — an impediment outside the workspace — and it must be
// legible afterwards.
func TestHaltVerdictEndsTheRunWithATypedMarker(t *testing.T) {
	root := oneOpenFeat(t)
	h := baseHooks()
	sessions := 0
	h.Session = func(Step, string, float64) (Verdict, error) {
		sessions++
		return Verdict{Status: VerdictHalt, Summary: "DATABASE_URL secret is not provisioned"}, nil
	}
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeHalted {
		t.Fatalf("expected OutcomeHalted, got %d (%s)", sum.Outcome, sum.Reason)
	}
	if sessions != 1 {
		t.Errorf("a halt ends the run immediately, got %d sessions", sessions)
	}
	b, blocked := ReadBlock(root, "p", "a")
	if !blocked || b.Kind != BlockHalt || b.Mechanical() {
		t.Fatalf("expected a non-mechanical halt marker, got %+v", b)
	}
	if !strings.Contains(b.Reason, "DATABASE_URL") {
		t.Errorf("the marker should carry the impediment, got %q", b.Reason)
	}
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | halted") {
		t.Errorf("halt not journaled: %s", logData)
	}
}

// TestFailureRotationLetsSiblingsAdvance: a failing feat steps aside for one
// selection so the rest of the plan keeps moving, and comes back when nothing
// else is eligible — no disk marker, no starvation in either direction.
func TestFailureRotationLetsSiblingsAdvance(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	writeSpec(t, root, "a", allApproved, true, "- [ ] 1. do it\n")
	writeSpec(t, root, "b", allApproved, true, "- [ ] 1. do it\n")

	var worked []string
	h := baseHooks()
	h.Session = func(step Step, _ string, _ float64) (Verdict, error) {
		worked = append(worked, step.Feat)
		if step.Feat == "a" {
			// Claims done without checking the box: the materialization check
			// refutes it, so a keeps failing while b is free to advance.
			return Verdict{Status: VerdictDone, Summary: "claims done"}, nil
		}
		writeFile(t, filepath.Join(root, "specs", "b", "tasks.md"), "- [x] 1. do it\n")
		return Verdict{Status: VerdictDone}, nil
	}

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 6, Stall: 4, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Steps < 1 {
		t.Errorf("feat b should have advanced despite a failing, got %+v (worked %v)", sum, worked)
	}
	// a fails first, then b gets its turn, then a is retried — the rotation.
	if len(worked) < 3 || worked[0] != "a" || worked[1] != "b" || worked[2] != "a" {
		t.Errorf("expected a → b → a rotation, got %v", worked)
	}
}

// TestClearStaleDeviationsOnlyWhenThePlanChanged pins the `plan approve` half of
// the deviation story, which survives for markers written by older runners: only
// a genuinely revised plan retires an objection.
func TestClearStaleDeviationsOnlyWhenThePlanChanged(t *testing.T) {
	root := oneOpenFeat(t)
	hash, err := HashPlan(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBlock(root, "p", Block{Feat: "a", Step: "spec-design", Kind: BlockDeviation,
		Reason: "session blocked: needs a queue", Revision: "split feat a", PlanHash: hash}); err != nil {
		t.Fatal(err)
	}

	// Re-approving the SAME plan resolves nothing: the objection still stands.
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApprovePlan(root, doc, fixedNow()); err != nil {
		t.Fatal(err)
	}
	cleared, err := ClearStaleDeviations(root, "p", fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 0 {
		t.Errorf("re-approving an unchanged plan must not unblock, got %v", cleared)
	}
	if _, blocked := ReadBlock(root, "p", "a"); !blocked {
		t.Fatal("feat a should still be blocked")
	}

	// Revise the plan, re-approve: that IS the unblock.
	writeFile(t, filepath.Join(Dir(root, "p"), "plan.md"), runnerPlan+"\n## Executor Notes\n\nUse a queue.\n")
	doc, err = Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApprovePlan(root, doc, fixedNow()); err != nil {
		t.Fatal(err)
	}
	cleared, err = ClearStaleDeviations(root, "p", fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 1 || cleared[0] != "a" {
		t.Fatalf("revising the plan should unblock feat a, got %v", cleared)
	}
	if _, blocked := ReadBlock(root, "p", "a"); blocked {
		t.Errorf("the deviation marker should be gone")
	}
	// The journal keeps both halves of the story, and the outstanding count clears.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | unblocked") || !strings.Contains(string(logData), "the plan was revised") {
		t.Errorf("unblock not journaled: %s", logData)
	}
	if n := countDeviations(Dir(root, "p")); n != 0 {
		t.Errorf("an answered deviation must stop counting as unprocessed, got %d", n)
	}
}

func TestReadBlockTreatsLegacyMarkerAsUnknown(t *testing.T) {
	root := oneOpenFeat(t)
	// What every csdd before typed markers wrote: a bare reason line.
	writeFile(t, filepath.Join(stateDir(root, "p"), "blocked", "a"), "gates failed after 2 retries: boom\n")

	b, blocked := ReadBlock(root, "p", "a")
	if !blocked {
		t.Fatal("a legacy marker must still block")
	}
	if b.Kind != BlockUnknown || b.Mechanical() {
		t.Errorf("a legacy marker is not mechanically classified, got kind %q mechanical=%v", b.Kind, b.Mechanical())
	}
	if b.Reason != "gates failed after 2 retries: boom" {
		t.Errorf("legacy reason lost: %q", b.Reason)
	}
	// The autonomous runner clears even unknown markers at startup and retries.
	h := baseHooks()
	h.Session = func(Step, string, float64) (Verdict, error) {
		writeFile(t, filepath.Join(root, "specs", "a", "tasks.md"), "- [x] 1. do it\n")
		return Verdict{Status: VerdictDone}, nil
	}
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 3, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Completed {
		t.Errorf("the feat behind an unknown marker should be retried to completion, got %+v", sum)
	}
	if !strings.Contains(out.String(), "was blocked [unknown]") {
		t.Errorf("the run should say what it cleared:\n%s", out.String())
	}
}

func TestUnblockClearsAndJournals(t *testing.T) {
	root := oneOpenFeat(t)
	if err := WriteBlock(root, "p", Block{Feat: "a", Step: "task 1", Kind: BlockGateFailure, Reason: "boom"}); err != nil {
		t.Fatal(err)
	}
	b, _ := ReadBlock(root, "p", "a")
	if err := Unblock(root, "p", b, "csdd plan unblock", fixedNow()); err != nil {
		t.Fatal(err)
	}
	if _, blocked := ReadBlock(root, "p", "a"); blocked {
		t.Errorf("marker should be gone")
	}
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "## [2026-07-07] task 1 | a | unblocked") {
		t.Errorf("unblock not journaled: %s", logData)
	}
}

func TestListBlocksIsOrderedAndClearIsIdempotent(t *testing.T) {
	root := oneOpenFeat(t)
	for _, f := range []string{"b", "a"} {
		if err := WriteBlock(root, "p", Block{Feat: f, Kind: BlockGateFailure, Reason: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	blocks := ListBlocks(root, "p")
	if len(blocks) != 2 || blocks[0].Feat != "a" || blocks[1].Feat != "b" {
		t.Errorf("ListBlocks should be feat-ordered, got %+v", blocks)
	}
	if err := ClearBlock(root, "p", "a"); err != nil {
		t.Fatal(err)
	}
	if err := ClearBlock(root, "p", "a"); err != nil {
		t.Errorf("clearing an absent marker is not an error, got %v", err)
	}
	if ListBlocks(root, "nope") != nil {
		t.Errorf("an unknown plan has no blocks")
	}
}

func TestTailCapKeepsTheTailOnARuneBoundary(t *testing.T) {
	if got := tailCap("short", 100); got != "short" {
		t.Errorf("under the cap the text is untouched, got %q", got)
	}
	// Multi-byte runes straddling the cut must not be sliced in half.
	s := strings.Repeat("ç", 100) // 200 bytes
	got := tailCap(s, 51)
	body := got[strings.IndexByte(got, '\n')+1:]
	if !strings.HasSuffix(s, body) {
		t.Errorf("tailCap should keep a suffix of the input")
	}
	for _, r := range body {
		if r != 'ç' {
			t.Fatalf("tailCap sliced a rune: %q", body)
		}
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("a truncated tail should say so, got %q", got)
	}
}

func TestRenderTailOmitsOldAttemptsButSaysSo(t *testing.T) {
	h := &failureHistory{}
	for i := 1; i <= 8; i++ {
		h.add("gate failed", "boom "+itoa(i))
	}
	out := h.renderTail(5, 100)
	if !strings.Contains(out, "3 older attempt(s) omitted") {
		t.Errorf("the omission must be explicit, got:\n%s", out)
	}
	if strings.Contains(out, "boom 3") || !strings.Contains(out, "boom 4") || !strings.Contains(out, "boom 8") {
		t.Errorf("renderTail should keep exactly the last 5 attempts:\n%s", out)
	}
}

func TestStepFileName(t *testing.T) {
	cases := map[string]string{
		"task 1.2":         "task-1.2",
		"spec-design":      "spec-design",
		"task 1/../../etc": "task-1-..-..-etc", // separators die; the name stays a basename
		"../../etc":        "etc",              // leading dots trimmed: never a parent dir
		"..":               "step",
		"":                 "step",
		"///":              "step",
	}
	for in, want := range cases {
		if got := stepFileName(in); got != want {
			t.Errorf("stepFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRetractTaskClaimTouchesOnlyTheFailedTask(t *testing.T) {
	root := oneOpenFeat(t)
	// Sibling tasks, a fenced example that looks like a task, and CRLF endings —
	// none of which the retraction may disturb.
	tasksMD := "- [x] 1. do it\r\n- [x] 2. keep me\r\n\r\n```\r\n- [x] 1. fenced example\r\n```\r\n"
	writeFile(t, filepath.Join(root, "specs", "a", "tasks.md"), tasksMD)

	retractTaskClaim(RunOptions{Root: root, Slug: "p"}, Step{Feat: "a", Step: "task 1"})

	got, _ := os.ReadFile(filepath.Join(root, "specs", "a", "tasks.md"))
	want := "- [ ] 1. do it\r\n- [x] 2. keep me\r\n\r\n```\r\n- [x] 1. fenced example\r\n```\r\n"
	if string(got) != want {
		t.Errorf("retraction rewrote too much:\n got %q\nwant %q", got, want)
	}
}

func TestRetractTaskClaimIgnoresSpecPhaseSteps(t *testing.T) {
	root := oneOpenFeat(t)
	before, _ := os.ReadFile(filepath.Join(root, "specs", "a", "tasks.md"))
	retractTaskClaim(RunOptions{Root: root, Slug: "p"}, Step{Feat: "a", Step: StepSpecDesign})
	after, _ := os.ReadFile(filepath.Join(root, "specs", "a", "tasks.md"))
	if string(before) != string(after) {
		t.Errorf("a spec-phase failure has no task claim to retract")
	}
}

func TestBlockedAtIsStamped(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	h := failingGates(t, root, 0, &briefs)
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, Stall: 2, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	b, _ := ReadBlock(root, "p", "a")
	if _, err := time.Parse(time.RFC3339, b.BlockedAt); err != nil {
		t.Errorf("blocked_at should be an RFC3339 stamp, got %q", b.BlockedAt)
	}
}
