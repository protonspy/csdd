package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// greenReport is a recorded evidence file the gate accepts.
const greenReport = `{"feature":"a","updatedAt":"2026-07-07T00:00:00Z",` +
	`"command":"go test ./...","tests":{"total":3,"passed":3,"failed":0,"skipped":0}}`

func writeReport(t *testing.T, root, slug, body string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "specs", slug, "test-report.json"), body)
}

// TestGateDoneChecks covers every way a `done` verdict can be refused. Each case
// is one obligation the session contracted to meet and did not, and the gate must
// name the specific one — a generic "not ready" would leave the next session
// guessing, which is exactly the loop the bound exists to stop.
func TestGateDoneChecks(t *testing.T) {
	all := map[string]bool{"requirements": true, "design": true, "tasks": true}
	feat := Feat{Slug: "a"}

	cases := []struct {
		name      string
		setup     func(t *testing.T, root string)
		wantCheck string // substring of the failed check's name
	}{
		{
			name:      "no spec at all",
			setup:     func(*testing.T, string) {},
			wantCheck: "spec exists",
		},
		{
			name: "phases not approved",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", map[string]bool{"requirements": true}, false, "- [x] 1. done\n")
				writeReport(t, root, "a", greenReport)
			},
			wantCheck: "spec phases approved",
		},
		{
			name: "approved but not flagged ready",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", all, false, "- [x] 1. done\n")
				writeReport(t, root, "a", greenReport)
			},
			wantCheck: "spec ready for implementation",
		},
		{
			name: "no tasks broken down",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", all, true, "# Tasks\n\nnothing yet\n")
				writeReport(t, root, "a", greenReport)
			},
			wantCheck: "tasks are broken down",
		},
		{
			name: "a task box is still open",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", all, true, "- [x] 1. done\n- [ ] 2. not done\n")
				writeReport(t, root, "a", greenReport)
			},
			wantCheck: "every task is checked",
		},
		{
			name: "no recorded evidence",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", all, true, "- [x] 1. done\n")
			},
			wantCheck: "test evidence recorded",
		},
		{
			name: "evidence records no tests",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", all, true, "- [x] 1. done\n")
				writeReport(t, root, "a", `{"feature":"a","updatedAt":"x"}`)
			},
			wantCheck: "test evidence records tests",
		},
		{
			name: "evidence is red",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", all, true, "- [x] 1. done\n")
				writeReport(t, root, "a", `{"feature":"a","updatedAt":"x","tests":{"total":3,"passed":2,"failed":1}}`)
			},
			wantCheck: "test evidence is green",
		},
		{
			// R11.2 enforcing itself: a report written by a command that excluded
			// tests carries an attention, and an attention can never satisfy `done`.
			name: "evidence carries an open attention",
			setup: func(t *testing.T, root string) {
				writeSpec(t, root, "a", all, true, "- [x] 1. done\n")
				writeReport(t, root, "a", `{"feature":"a","updatedAt":"x","tests":{"total":3,"passed":3,"failed":0},`+
					`"attentions":["the test command excludes tests: --ignore=tests/unit/test_pinned.py"]}`)
			},
			wantCheck: "no open attentions",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			findings := gateDone(root, feat)
			if len(findings) == 0 {
				t.Fatalf("expected the gate to refuse this `done`")
			}
			if !strings.Contains(gateCheckNames(findings), tc.wantCheck) {
				t.Errorf("want a %q finding, got %q", tc.wantCheck, gateCheckNames(findings))
			}
			// Every finding must carry an actionable detail, not just a label.
			for _, f := range findings {
				if strings.TrimSpace(f.detail) == "" {
					t.Errorf("finding %q has no detail for the next session to act on", f.check)
				}
			}
		})
	}
}

// TestGateDoneAcceptsADeliveredFeat is the other half: a feat whose artifacts all
// agree passes with no findings. Without this, a gate that refused everything
// would still satisfy the table above.
func TestGateDoneAcceptsADeliveredFeat(t *testing.T) {
	root := t.TempDir()
	deliverSpec(t, root, "a")
	if findings := gateDone(root, Feat{Slug: "a"}); len(findings) != 0 {
		t.Errorf("a fully delivered feat must pass the gate, got %q", gateCheckNames(findings))
	}
}

// TestGateReportsEveryFailedCheckAtOnce: a session that missed two obligations is
// told both, so it does not discover the second only after fixing the first.
func TestGateReportsEveryFailedCheckAtOnce(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "a", map[string]bool{"requirements": true, "design": true, "tasks": true}, true,
		"- [x] 1. done\n- [ ] 2. open\n")
	// tasks unchecked AND no evidence recorded.
	findings := gateDone(root, Feat{Slug: "a"})
	if len(findings) != 2 {
		t.Fatalf("expected both failures reported together, got %d: %q", len(findings), gateCheckNames(findings))
	}
	h := gateHandoff(Feat{Slug: "a"}, findings, "I finished everything")
	for _, want := range []string{"every task is checked", "test evidence recorded", "I finished everything"} {
		if !strings.Contains(h, want) {
			t.Errorf("handoff should carry %q:\n%s", want, h)
		}
	}
	if !strings.Contains(h, "return `continue`") {
		t.Errorf("the handoff must tell the session what to do if it truly cannot comply:\n%s", h)
	}
}

// TestRunnerGateConvertsFalseDone: the loop-level behavior. A session that claims
// `done` without leaving the artifacts does NOT advance the plan — the verdict
// becomes a `continue` carrying the gate's handoff, and the next session reads it.
// This is the hole the plan was written to close: before the gate, this session
// marked the feat delivered and the run moved on.
func TestRunnerGateConvertsFalseDone(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	var briefs []string
	calls := 0
	h := baseHooks(t, root)
	h.Session = func(feat Feat, brief string, _ float64) (SessionOutcome, error) {
		briefs = append(briefs, brief)
		calls++
		if calls == 1 {
			// The realistic false-done: the session really did the spec work and
			// checked the boxes, but never recorded the test evidence — and then
			// declared the feat delivered anyway.
			writeSpec(t, root, feat.Slug,
				map[string]bool{"requirements": true, "design": true, "tasks": true}, true,
				"- [x] 1. Deliver the behavior\n")
			return doneOutcome("everything is finished, I promise"), nil
		}
		deliverSpec(t, root, feat.Slug)
		return doneOutcome("actually delivered this time"), nil
	}
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Gated != 1 {
		t.Errorf("expected exactly one gated verdict, got %d", sum.Gated)
	}
	if !sum.Completed {
		t.Fatalf("the run should still complete once the feat is really delivered: %+v", sum)
	}
	// The second session for feat a must have been handed the gate's handoff.
	if len(briefs) < 2 || !strings.Contains(briefs[1], "on-disk checks refused it") {
		t.Errorf("session 2 should read the gate handoff, got:\n%s", briefs[1])
	}
	if !strings.Contains(briefs[1], "test evidence recorded") {
		t.Errorf("the handoff must name which check failed:\n%s", briefs[1])
	}
	// A refused `done` is journaled as progress, never as delivered.
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "verdict gate refused `done`") {
		t.Errorf("the refusal should be journaled: %s", logData)
	}
	if !strings.Contains(out.String(), "claimed done but the on-disk checks refused it") {
		t.Errorf("the run log should surface the refusal:\n%s", out.String())
	}
}

// TestFeatAttemptBoundStopsAnUnconvergingFeat (R10.4, R10.5): a session that
// insists it is done but never produces the artifacts would `continue` forever —
// the stall guard cannot catch it, because `continue` resets the stall counter.
// The per-feat bound stops that feat, surfaces it, and lets the rest of the plan
// finish. §5.5 is explicit that this cannot ship separately from the gate.
func TestFeatAttemptBoundStopsAnUnconvergingFeat(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	worked := map[string]int{}
	h := baseHooks(t, root)
	h.Session = func(feat Feat, _ string, _ float64) (SessionOutcome, error) {
		worked[feat.Slug]++
		if feat.Slug == "a" {
			return doneOutcome("done (it is not)"), nil // never delivers
		}
		deliverSpec(t, root, feat.Slug)
		return doneOutcome("really delivered"), nil
	}
	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 50, FeatAttempts: 3, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if worked["a"] != 3 {
		t.Errorf("feat a should get exactly its 3 attempts, got %d", worked["a"])
	}
	if sum.Outcome != OutcomeBlocked {
		t.Fatalf("expected OutcomeBlocked, got %d (%s)", sum.Outcome, sum.Reason)
	}
	if len(sum.Blocked) != 1 || sum.Blocked[0] != "a" {
		t.Errorf("feat a should be reported blocked, got %v", sum.Blocked)
	}
	if sum.Completed {
		t.Errorf("a run with a blocked feat is not complete")
	}
	// The bound stops the FEAT, not the run: feat b still gets delivered.
	if worked["b"] != 1 || sum.Steps != 1 {
		t.Errorf("the rest of the plan should still run: b worked %d times, %d steps", worked["b"], sum.Steps)
	}
	logData, _ := os.ReadFile(filepath.Join(Dir(root, "p"), "log.md"))
	if !strings.Contains(string(logData), "| a | blocked") {
		t.Errorf("a blocked feat must be journaled: %s", logData)
	}
	if !strings.Contains(out.String(), "moving on") {
		t.Errorf("the run log should say the loop moved on:\n%s", out.String())
	}
}

// TestSessionRecordsEveryAttempt (R9.1, R9.2): every attempt gets a row with its
// cost — the failures and the refused verdicts included, because those are the
// attempts an optimization is trying to remove. A log of only the successes would
// hide exactly what the plan set out to measure.
func TestSessionRecordsEveryAttempt(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	calls := 0
	metrics := SessionMetrics{
		Duration:    90 * time.Second,
		APIDuration: 60 * time.Second,
		CostUSD:     1.25,
		NumTurns:    7,
		Tokens:      SessionTokens{Input: 100, Output: 200, CacheRead: 3000},
		Models:      []string{"claude-opus-4-8"},
	}
	h := baseHooks(t, root)
	h.Session = func(feat Feat, _ string, _ float64) (SessionOutcome, error) {
		calls++
		switch calls {
		case 1:
			return SessionOutcome{Metrics: metrics}, errString("boom")
		case 2:
			return SessionOutcome{Verdict: Verdict{Status: VerdictContinue, Summary: "half way"}, Metrics: metrics}, nil
		case 3:
			return SessionOutcome{Verdict: Verdict{Status: VerdictDone}, Metrics: metrics}, nil // gated: nothing on disk
		}
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone, Summary: "shipped"}, Metrics: metrics}, nil
	}
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}

	recs := LoadSessionRecords(root, "p")
	if len(recs) != calls {
		t.Fatalf("expected one record per session attempt (%d), got %d", calls, len(recs))
	}
	wantStatus := []string{"failed", "continue", "continue", "done", "done"}
	for i, want := range wantStatus {
		if recs[i].Status != want {
			t.Errorf("record %d status = %q, want %q", i, recs[i].Status, want)
		}
	}
	// The third attempt is the gate's conversion, and is marked as such so a
	// refused verdict is distinguishable from an honest `continue`.
	if !recs[2].Gated {
		t.Errorf("the gate-converted verdict should be flagged Gated: %+v", recs[2])
	}
	if recs[1].Gated {
		t.Errorf("an honest continue must not be flagged Gated: %+v", recs[1])
	}
	// Attempt numbering is per feat, and the FAILED attempt is counted too.
	if recs[0].Attempt != 1 || recs[1].Attempt != 2 || recs[2].Attempt != 3 {
		t.Errorf("attempts should number per feat: %d, %d, %d", recs[0].Attempt, recs[1].Attempt, recs[2].Attempt)
	}
	// Cost rides on every row, including the one that failed.
	for i, r := range recs {
		if r.CostUSD != 1.25 || r.DurationMS != 90_000 || r.NumTurns != 7 {
			t.Errorf("record %d lost its metrics: %+v", i, r)
		}
		if r.Tokens.Total() != 3300 {
			t.Errorf("record %d token total = %d, want 3300", i, r.Tokens.Total())
		}
		if r.At == "" || r.Feat == "" {
			t.Errorf("record %d is missing its stamp: %+v", i, r)
		}
	}
}

// TestParseSessionMetrics reads the real `result` event captured from a live
// session, so a rename of any envelope field upstream fails here rather than
// silently zeroing every cost figure the plan is meant to compare (§7).
func TestParseSessionMetrics(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "session_fleet.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	var result string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"type":"result"`) {
			result = line
		}
	}
	if result == "" {
		t.Fatal("the fixture has no result event")
	}

	m := parseSessionMetrics([]byte(result))
	if m.Duration != 12926*time.Millisecond {
		t.Errorf("duration = %s, want 12.926s", m.Duration)
	}
	if m.APIDuration != 18328*time.Millisecond {
		t.Errorf("api duration = %s, want 18.328s", m.APIDuration)
	}
	if m.NumTurns != 4 {
		t.Errorf("turns = %d, want 4", m.NumTurns)
	}
	if m.CostUSD < 0.235 || m.CostUSD > 0.236 {
		t.Errorf("cost = %f, want ~0.2352", m.CostUSD)
	}
	if m.Tokens.Input != 155 || m.Tokens.Output != 590 ||
		m.Tokens.CacheRead != 36921 || m.Tokens.CacheCreation != 7004 {
		t.Errorf("token split wrong: %+v", m.Tokens)
	}
	// The loop tiers its models; which ones billed is part of comparing runs.
	if len(m.Models) != 2 || m.Models[0] != "claude-haiku-4-5-20251001" {
		t.Errorf("models = %v, want both, sorted", m.Models)
	}
	if m.Empty() {
		t.Errorf("a populated result event must not read as empty")
	}
	// Junk must degrade to zero, never panic or fail a run.
	if !parseSessionMetrics([]byte("not json")).Empty() {
		t.Errorf("unparseable input should yield empty metrics")
	}
}
