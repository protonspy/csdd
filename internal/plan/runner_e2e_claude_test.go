package plan

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestExecClaudeSessionAgainstRealCLI drives execClaudeSession against a real
// `claude` process. It is opt-in (CSDD_E2E_CLAUDE=1) because it spends money and
// needs network, but it guards the one thing unit tests cannot: that the flags the
// runner pins still produce a stream whose final event yields a verdict. A drift in
// the CLI's envelope would otherwise fail every session at runtime with
// "could not parse a verdict", and nothing in the suite would catch it.
func TestExecClaudeSessionAgainstRealCLI(t *testing.T) {
	if os.Getenv("CSDD_E2E_CLAUDE") != "1" {
		t.Skip("set CSDD_E2E_CLAUDE=1 to run the live claude session check")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}

	var view bytes.Buffer
	env := sessionEnv{
		idle: 5 * time.Minute,
		out:  &view, // a buffer is not a TTY, so this exercises the log renderer
		now:  time.Now,
	}
	brief := "Reply with the verdict only. Do not use any tools. " +
		"Report status \"done\" and a one-sentence summary."

	v, err := execClaudeSession(Feat{Slug: "e2e"}, brief, 0, "", "", env)
	if err != nil {
		t.Fatalf("live session failed: %v", err)
	}
	if v.Verdict.Status != VerdictDone && v.Verdict.Status != VerdictContinue {
		t.Fatalf("want a done|continue verdict, got %q", v.Verdict.Status)
	}
	// The same `result` event that carried the verdict must also have yielded the
	// session's cost (R9.1). A live session that reports no duration and no tokens
	// means the envelope's field names drifted — which the fixture tests cannot see.
	if v.Metrics.Empty() {
		t.Errorf("a completed live session must report metrics, got %+v", v.Metrics)
	}
	t.Logf("verdict=%s summary=%q metrics=%+v view=%q",
		v.Verdict.Status, strings.TrimSpace(v.Verdict.Summary), v.Metrics, view.String())
}

// TestFleetViewAgainstRealDispatch proves the live view end-to-end: a real session
// that dispatches sub-agents must surface them as tracked agents, not as one opaque
// "working" line. It guards the task-event protocol the fleet view depends on —
// a rename of task_started/task_progress upstream would silently empty the tree,
// and the fixture-based tests could not notice.
func TestFleetViewAgainstRealDispatch(t *testing.T) {
	if os.Getenv("CSDD_E2E_CLAUDE") != "1" {
		t.Skip("set CSDD_E2E_CLAUDE=1 to run the live fleet check")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}

	var view bytes.Buffer
	brief := "Dispatch two sub-agents in parallel with the Task tool: one to print the " +
		"working directory, one to print today's date. Then report status \"done\"."

	v, err := execClaudeSession(Feat{Slug: "e2e-fleet"}, brief, 0, "", "",
		sessionEnv{idle: 5 * time.Minute, out: &view, now: time.Now})
	if err != nil {
		t.Fatalf("live session failed: %v", err)
	}
	if v.Verdict.Status == "" {
		t.Fatal("no verdict from the dispatching session")
	}
	got := view.String()
	if !strings.Contains(got, "dispatched") {
		t.Fatalf("the live view never reported a dispatch; the task protocol may have drifted.\nview:\n%s", got)
	}
	t.Logf("verdict=%s\nlive view:\n%s", v.Verdict.Status, got)
}
