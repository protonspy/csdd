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
// which attempt goes green. briefs captures what each session actually read.
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

func TestRepairSessionRescuesStepWithFullHistory(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	// Two retries fail; the repair session (the 3rd gate call) goes green.
	h := failingGates(t, root, 3, &briefs)

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, MaxRetries: 1, MaxRepairs: 1, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Repairs != 1 || sum.Repaired != 1 {
		t.Errorf("expected exactly one repair session that rescued the step, got %+v", sum)
	}
	if !sum.Completed {
		t.Errorf("the repaired step should let the plan complete, got %+v", sum)
	}
	if _, blocked := ReadBlock(root, "p", "a"); blocked {
		t.Errorf("a repaired feat must not be left blocked")
	}
	if len(briefs) != 3 {
		t.Fatalf("expected 2 attempts + 1 repair session, got %d briefs", len(briefs))
	}
	// The retry brief carries only the last failure; the repair brief carries both.
	repair := briefs[2]
	for _, want := range []string{"Repair session", "ROOT CAUSE", "Attempt 1", "Attempt 2", "FAIL attempt 1", "FAIL attempt 2"} {
		if !strings.Contains(repair, want) {
			t.Errorf("repair brief missing %q:\n%s", want, repair)
		}
	}
	if strings.Contains(briefs[1], "Attempt 1 —") {
		t.Errorf("a plain retry brief should carry the last failure, not the rendered history:\n%s", briefs[1])
	}
	// The journal records the rescue rather than a bare "done".
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | done (repaired)") {
		t.Errorf("repair not journaled: %s", logData)
	}
}

func TestMechanicalBlockIsTypedAndKeepsItsLog(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	h := failingGates(t, root, 0, &briefs) // never passes

	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, MaxRetries: 1, MaxRepairs: 1, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Outcome != OutcomeNothing {
		t.Errorf("expected OutcomeNothing once a is blocked and b is done, got %d", sum.Outcome)
	}
	b, blocked := ReadBlock(root, "p", "a")
	if !blocked {
		t.Fatal("feat a should be blocked")
	}
	if b.Kind != BlockGateFailure || !b.Mechanical() {
		t.Errorf("a gate failure must be a mechanical block, got kind %q", b.Kind)
	}
	if b.Repairs != 1 {
		t.Errorf("the spent repair session should be recorded on the marker, got %d", b.Repairs)
	}
	if b.Attempts != 3 {
		t.Errorf("expected 2 retries + 1 repair recorded as attempts, got %d", b.Attempts)
	}
	if b.Step != "task 1" {
		t.Errorf("marker should name the step it failed on, got %q", b.Step)
	}

	// The failure log survives the run with every attempt's untruncated output —
	// the whole point: the retry brief only ever carried a tail.
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
	// make the feat read as done the moment anything cleared the marker.
	tasks, _ := os.ReadFile(filepath.Join(root, "specs", "a", "tasks.md"))
	if got := strings.TrimSpace(string(tasks)); got != "- [ ] 1. do it" {
		t.Errorf("a refuted task claim must be retracted, got %q", got)
	}

	// A mechanical block is not a deviation: it must not show up as work the human
	// owes the plan.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "blocked (gate-failure)") {
		t.Errorf("mechanical block not journaled with its kind: %s", logData)
	}
	if n := countDeviations(Dir(root, "p")); n != 0 {
		t.Errorf("a gate failure must not count as an unprocessed deviation, got %d", n)
	}
}

func TestMechanicalBlockRetriedOnNextRunUntilBudgetSpent(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string

	// Run 1: everything fails, one repair session is spent.
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: failingGates(t, root, 0, &briefs), MaxIterations: 5, MaxRetries: 1, MaxRepairs: 2, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	b, _ := ReadBlock(root, "p", "a")
	if b.Repairs != 1 {
		t.Fatalf("run 1 should have spent 1 repair session, got %d", b.Repairs)
	}

	// Run 2: budget remains, so the marker is cleared and the feat retried. This
	// time the first gate passes — the run recovers with no human in the loop.
	var briefs2 []string
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: failingGates(t, root, 1, &briefs2), MaxIterations: 5, MaxRetries: 1, MaxRepairs: 2, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if len(briefs2) == 0 {
		t.Fatal("run 2 should have retried the blocked feat")
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
}

func TestMechanicalBlockSticksOnceRepairBudgetIsSpent(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	// MaxRepairs 1: run 1 spends the only repair session.
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: failingGates(t, root, 0, &briefs), MaxIterations: 5, MaxRetries: 1, MaxRepairs: 1, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}

	var briefs2 []string
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: failingGates(t, root, 1, &briefs2), MaxIterations: 5, MaxRetries: 1, MaxRepairs: 1, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if len(briefs2) != 0 {
		t.Errorf("an exhausted feat must not burn another session, got %d", len(briefs2))
	}
	if sum.Outcome != OutcomeNothing {
		t.Errorf("expected OutcomeNothing, got %d", sum.Outcome)
	}
	if !strings.Contains(out.String(), "repair sessions spent") || !strings.Contains(out.String(), "plan unblock p a") {
		t.Errorf("the run must say how to resume:\n%s", out.String())
	}
}

func TestDeviationBlockSurvivesRunsAndBindsToThePlan(t *testing.T) {
	root := oneOpenFeat(t)
	h := baseHooks()
	sessions := 0
	h.Session = func(step Step, _ string, _ float64) (Verdict, error) {
		sessions++
		return Verdict{Status: VerdictBlocked, Summary: "needs a queue", Revision: "split feat a"}, nil
	}
	var out bytes.Buffer
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, MaxRetries: 2, MaxRepairs: 2, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Errorf("a deviation ends the feat immediately: no retries, no repair; got %d sessions", sessions)
	}
	b, blocked := ReadBlock(root, "p", "a")
	if !blocked || b.Kind != BlockDeviation || b.Mechanical() {
		t.Fatalf("expected a non-mechanical deviation block, got %+v", b)
	}
	if b.Revision != "split feat a" {
		t.Errorf("the marker should carry the session's proposal, got %q", b.Revision)
	}
	hash, _ := HashPlan(Dir(root, "p"))
	if b.PlanHash != hash {
		t.Errorf("the deviation must bind to the plan it objected to")
	}
	if !strings.Contains(out.String(), "plan approve p") {
		t.Errorf("the run must point at re-approval as the way out:\n%s", out.String())
	}

	// A second run leaves it alone: only a revised plan retires a deviation.
	sessions = 0
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 5, MaxRepairs: 2, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Errorf("a deviation must not be auto-retried, got %d sessions", sessions)
	}
	if n := countDeviations(Dir(root, "p")); n != 1 {
		t.Errorf("the deviation should read as one unprocessed objection, got %d", n)
	}
}

func TestClearStaleDeviationsOnlyWhenThePlanChanged(t *testing.T) {
	root := oneOpenFeat(t)
	h := baseHooks()
	h.Session = func(Step, string, float64) (Verdict, error) {
		return Verdict{Status: VerdictBlocked, Summary: "needs a queue", Revision: "split feat a"}, nil
	}
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 3, Out: &bytes.Buffer{}}); err != nil {
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
		t.Errorf("a legacy marker is not safely auto-clearable, got kind %q mechanical=%v", b.Kind, b.Mechanical())
	}
	if b.Reason != "gates failed after 2 retries: boom" {
		t.Errorf("legacy reason lost: %q", b.Reason)
	}
	// The runner leaves it alone rather than guessing which kind it was.
	var out bytes.Buffer
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: baseHooks(), MaxIterations: 3, MaxRepairs: 2, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if _, blocked := ReadBlock(root, "p", "a"); !blocked {
		t.Errorf("a legacy marker must not be auto-cleared")
	}
	if !strings.Contains(out.String(), "stays blocked [unknown]") {
		t.Errorf("the run should say why it skipped the feat:\n%s", out.String())
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
		t.Errorf("a spec-phase block has no task claim to retract")
	}
}

func TestBlockedAtIsStamped(t *testing.T) {
	root := oneOpenFeat(t)
	var briefs []string
	h := failingGates(t, root, 0, &briefs)
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 3, MaxRetries: 0, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	b, _ := ReadBlock(root, "p", "a")
	if _, err := time.Parse(time.RFC3339, b.BlockedAt); err != nil {
		t.Errorf("blocked_at should be an RFC3339 stamp, got %q", b.BlockedAt)
	}
}
