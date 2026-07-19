package plan

import (
	"strings"
	"testing"
	"time"
)

// Event lines shaped after a real `claude -p --output-format stream-json --verbose`
// run, so this locks the parser to the envelope the CLI actually emits.
const (
	evInit      = `{"type":"system","subtype":"init","cwd":"/w","session_id":"s1","model":"claude-opus-4-8"}`
	evRateLimit = `{"type":"rate_limit_event","rate_limit_info":{},"session_id":"s1"}`
	evToolUse   = `{"type":"assistant","message":{"content":[{"type":"text","text":"ok"},{"type":"tool_use","name":"Bash","input":{}}]},"session_id":"s1"}`
	evToolBack  = `{"type":"user","message":{"content":[{"type":"tool_result","content":"done"}]},"session_id":"s1"}`
	evResult    = `{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"done\",\"summary\":\"shipped it\"}","structured_output":{"status":"done","summary":"shipped it"},"session_id":"s1"}`
)

// The verdict must survive the move from --output-format json to stream-json: it
// now rides in the final `result` event, and parseVerdict has to read it there.
func TestSessionStreamYieldsVerdictFromResultEvent(t *testing.T) {
	s := newSessionStream(time.Now)
	for _, l := range []string{evInit, evRateLimit, evToolUse, evToolBack, evResult} {
		s.line(l)
	}
	src, ok := s.verdictSource()
	if !ok {
		t.Fatal("no result event captured")
	}
	v, err := parseVerdict([]byte(src))
	if err != nil {
		t.Fatalf("parseVerdict on the result event: %v", err)
	}
	if v.Status != VerdictDone {
		t.Errorf("want status %q, got %q", VerdictDone, v.Status)
	}
	if v.Summary != "shipped it" {
		t.Errorf("want the summary carried through, got %q", v.Summary)
	}
}

// The heartbeat reports the tool the session is running, which is what makes a
// long session legible instead of looking frozen.
func TestSessionStreamTracksActivity(t *testing.T) {
	s := newSessionStream(time.Now)
	s.line(evInit)
	if a, _ := s.snapshot(); a != "starting" {
		t.Errorf("want starting, got %q", a)
	}
	s.line(evToolUse)
	if a, n := s.snapshot(); a != "Bash" || n != 2 {
		t.Errorf("want Bash after 2 events, got %q after %d", a, n)
	}
}

// A `user` event's content is sometimes a bare string rather than a block array.
// Parsing must not choke on it — a dropped event is a missed liveness tick, and a
// dropped result event would fail the whole session.
func TestSessionStreamToleratesStringContent(t *testing.T) {
	s := newSessionStream(time.Now)
	s.line(`{"type":"user","message":{"content":"plain string"},"session_id":"s1"}`)
	s.line(evResult)
	if _, ok := s.verdictSource(); !ok {
		t.Fatal("a string-content event must not derail the stream")
	}
}

// Non-JSON noise on stdout must be ignored rather than counted or crashed on.
func TestSessionStreamIgnoresNonJSONLines(t *testing.T) {
	s := newSessionStream(time.Now)
	for _, l := range []string{"", "   ", "not json at all", "warning: something"} {
		s.line(l)
	}
	if _, n := s.snapshot(); n != 0 {
		t.Errorf("noise should not count as events, got %d", n)
	}
}

func TestCompactDuration(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{150 * time.Second, "2m30s"},
		{3900 * time.Second, "1h05m"},
	} {
		if got := compactDuration(tc.in); got != tc.want {
			t.Errorf("compactDuration(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The session must ask for the streaming envelope; falling back to the buffered
// one would silently restore the unbounded, output-less wait this replaced.
func TestSessionArgsRequestStreamingEnvelope(t *testing.T) {
	args := strings.Join(sessionArgs("brief", 0, "", ""), " ")
	for _, want := range []string{"--output-format stream-json", "--verbose", "--json-schema"} {
		if !strings.Contains(args, want) {
			t.Errorf("session args missing %q: %s", want, args)
		}
	}
}
