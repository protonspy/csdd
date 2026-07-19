package plan

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// feedFixture replays the recorded stream of a real session that dispatched two
// sub-agents. Recording it means the parser is pinned to the protocol the CLI
// actually emits, not to a shape invented here.
func feedFixture(t *testing.T) *sessionStream {
	t.Helper()
	f, err := os.Open("testdata/session_fleet.ndjson")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	s := newSessionStream(time.Now)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		s.line(sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return s
}

// The whole point of the fleet view: both dispatched agents must be tracked, named
// by the work they were given, and end up completed.
func TestFleetTracksDispatchedAgents(t *testing.T) {
	v := feedFixture(t).view()

	if len(v.Agents) != 2 {
		t.Fatalf("want 2 sub-agents tracked, got %d: %+v", len(v.Agents), v.Agents)
	}
	descs := []string{v.Agents[0].Desc, v.Agents[1].Desc}
	for _, want := range []string{"Count files in directory", "Print today's date"} {
		if !contains(descs, want) {
			t.Errorf("missing agent %q; got %v", want, descs)
		}
	}
	for _, a := range v.Agents {
		if a.Status != agentCompleted {
			t.Errorf("agent %q ended %q, want completed", a.Desc, a.Status)
		}
		if a.Kind == "" {
			t.Errorf("agent %q lost its subagent_type", a.Desc)
		}
		if a.Tokens == 0 {
			t.Errorf("agent %q lost its token usage", a.Desc)
		}
	}
	if v.activeAgents() != 0 {
		t.Errorf("no agent should still be running, got %d", v.activeAgents())
	}
}

// The dispatch description must survive task_progress, which restates it as
// "Running <tool thing>" — otherwise the tree would rename itself mid-flight.
func TestFleetKeepsDispatchDescriptionOverProgress(t *testing.T) {
	s := newSessionStream(time.Now)
	s.line(`{"type":"system","subtype":"task_started","task_id":"t1","description":"Task 4 TelegramFileIdCache","subagent_type":"implementer"}`)
	s.line(`{"type":"system","subtype":"task_progress","task_id":"t1","description":"Running some bash","last_tool_name":"Bash","usage":{"total_tokens":2048,"tool_uses":3}}`)

	a := s.view().Agents[0]
	if a.Desc != "Task 4 TelegramFileIdCache" {
		t.Errorf("description drifted to %q", a.Desc)
	}
	if a.Tool != "Bash" || a.Tokens != 2048 || a.ToolUses != 3 {
		t.Errorf("progress not applied: %+v", a)
	}
	if !a.Running() {
		t.Error("agent should still be running")
	}
}

// While sub-agents are in flight they are the headline, since that is what the
// session is actually spending time on.
func TestSnapshotPrefersActiveAgents(t *testing.T) {
	s := newSessionStream(time.Now)
	s.line(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read"}]}}`)
	s.line(`{"type":"system","subtype":"task_started","task_id":"t1","description":"Task 4","subagent_type":"implementer"}`)

	got, _ := s.snapshot()
	if !strings.Contains(got, "1 agents") || !strings.Contains(got, "Task 4") {
		t.Errorf("want the active agent surfaced, got %q", got)
	}
}

// A non-terminal destination must never receive cursor-control escapes: this
// output routinely lands in a redirected log file.
func TestLogReporterEmitsPlainTransitions(t *testing.T) {
	var buf bytes.Buffer
	r := &logReporter{out: &buf, now: time.Now, seen: map[string]string{}}

	s := newSessionStream(time.Now)
	s.line(`{"type":"system","subtype":"task_started","task_id":"t1","description":"Task 4","subagent_type":"implementer"}`)
	r.report(s.view())
	s.line(`{"type":"system","subtype":"task_updated","task_id":"t1","patch":{"status":"completed"}}`)
	r.report(s.view())

	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Errorf("log output must not contain ANSI escapes: %q", out)
	}
	if !strings.Contains(out, "dispatched implementer: Task 4") {
		t.Errorf("missing the dispatch line: %q", out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("missing the completion line: %q", out)
	}
	// A status that has not changed must not be reported twice.
	before := buf.Len()
	r.report(s.view())
	if buf.Len() != before {
		t.Errorf("repeated report duplicated output: %q", buf.String()[before:])
	}
}

// The live view must erase exactly the frame it drew, or the tree smears down the
// terminal instead of updating in place.
func TestLiveReporterRedrawsInPlace(t *testing.T) {
	var buf bytes.Buffer
	r := &liveReporter{out: &buf, now: time.Now}

	s := newSessionStream(time.Now)
	s.line(`{"type":"system","subtype":"task_started","task_id":"t1","description":"Task 4","subagent_type":"implementer"}`)

	r.paint(s.view(), 90*time.Second)
	first := buf.String()
	if strings.Contains(first, "\033[") {
		t.Errorf("the first frame has nothing to erase yet: %q", first)
	}
	if !strings.Contains(first, "● main") || !strings.Contains(first, "○ implementer  Task 4") {
		t.Errorf("frame does not render the tree: %q", first)
	}

	buf.Reset()
	r.paint(s.view(), 91*time.Second)
	// main + one agent == 2 lines to walk back over.
	if !strings.HasPrefix(buf.String(), "\033[2A\033[J") {
		t.Errorf("second frame must erase 2 lines first, got %q", buf.String())
	}
}

// A test buffer, a pipe, and a file are all non-terminals; only a character
// device may receive cursor control.
func TestIsTerminalRejectsNonTTYDestinations(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if isTerminal(f) {
		t.Error("a regular file is not a terminal")
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
