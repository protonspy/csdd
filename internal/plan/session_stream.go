package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// sessionStream consumes a `claude --output-format stream-json` NDJSON stream and
// keeps a live picture of what the session is doing.
//
// It exists for three reasons the old buffered `--output-format json` could not
// serve. Every event is a liveness tick for the watchdog, so a working session is
// distinguishable from a hung one. The verdict still arrives, in the final
// `result` event. And — because the session orchestrates rather than types — the
// stream carries a first-class task protocol for the sub-agents it dispatches,
// which is what lets the runner show the fleet instead of one opaque arrow.
//
// The task protocol, as emitted by the CLI (see testdata/session_fleet.ndjson):
//
//	system/task_started      task_id, tool_use_id, description, subagent_type
//	system/task_progress     task_id, description, last_tool_name, usage{...}
//	system/task_updated      task_id, patch{status, end_time}
//	system/task_notification task_id, status
//
// Sub-agent work also surfaces as ordinary assistant/user events carrying
// parent_tool_use_id; those are ignored here, since the task events say the same
// thing in a form that needs no correlation.
type sessionStream struct {
	mu     sync.Mutex
	result string // the last `result` event, verbatim
	main   string // what the orchestrating session itself is doing
	events int
	order  []string // task ids in dispatch order
	agents map[string]*agentState
	now    func() time.Time
}

// agentState is one dispatched sub-agent's live state.
type agentState struct {
	kind     string // subagent_type, e.g. "implementer"
	desc     string // description, e.g. "Task 4 TelegramFileIdCache"
	tool     string // last tool it ran
	tokens   int
	toolUses int
	status   string // "running", "completed", "failed"
	started  time.Time
	ended    time.Time
}

func newSessionStream(now func() time.Time) *sessionStream {
	if now == nil {
		now = time.Now
	}
	return &sessionStream{agents: map[string]*agentState{}, now: now}
}

// Sub-agent lifecycle statuses.
const (
	agentRunning   = "running"
	agentCompleted = "completed"
)

// line feeds one NDJSON line from the session's stdout.
func (s *sessionStream) line(l string) {
	l = strings.TrimSpace(l)
	if l == "" || l[0] != '{' {
		return
	}
	// `message` and `patch` stay raw: message.content is a block array for
	// assistant events but a bare string for some user events, and decoding both
	// through one struct would fail and drop the event entirely.
	var ev struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		TaskID    string `json:"task_id"`
		Desc      string `json:"description"`
		Kind      string `json:"subagent_type"`
		LastTool  string `json:"last_tool_name"`
		Status    string `json:"status"`
		ParentUse string `json:"parent_tool_use_id"`
		Usage     struct {
			TotalTokens int `json:"total_tokens"`
			ToolUses    int `json:"tool_uses"`
		} `json:"usage"`
		Patch struct {
			Status string `json:"status"`
		} `json:"patch"`
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal([]byte(l), &ev) != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.events++

	switch ev.Type {
	case "result":
		s.result = l
		return
	case "assistant":
		// Only the orchestrator's own tool calls describe the main line; a
		// sub-agent's are reported through its task events instead.
		if ev.ParentUse != "" {
			return
		}
		if name := toolNameOf(ev.Message); name != "" {
			s.main = name
		} else {
			s.main = "thinking"
		}
		return
	case "system":
		s.systemEvent(ev.Subtype, ev.TaskID, ev.Desc, ev.Kind, ev.LastTool,
			firstNonEmpty(ev.Patch.Status, ev.Status), ev.Usage.TotalTokens, ev.Usage.ToolUses)
		return
	}
}

// systemEvent applies one system/* event to the fleet. Callers hold s.mu.
func (s *sessionStream) systemEvent(subtype, taskID, desc, kind, tool, status string, tokens, toolUses int) {
	switch subtype {
	case "init":
		s.main = "starting"
		return
	case "task_started", "task_progress", "task_updated", "task_notification":
	default:
		return
	}
	if taskID == "" {
		return
	}
	a := s.agents[taskID]
	if a == nil {
		a = &agentState{status: agentRunning, started: s.now()}
		s.agents[taskID] = a
		s.order = append(s.order, taskID)
	}
	if kind != "" {
		a.kind = kind
	}
	// task_progress restates description as "Running <what>"; keep the dispatch
	// description, which names the unit of work, and take the tool separately.
	if desc != "" && subtype == "task_started" {
		a.desc = desc
	}
	if tool != "" {
		a.tool = tool
	}
	if tokens > 0 {
		a.tokens = tokens
	}
	if toolUses > 0 {
		a.toolUses = toolUses
	}
	if status != "" && status != agentRunning {
		a.status = status
		if a.ended.IsZero() {
			a.ended = s.now()
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// toolNameOf returns the tool a message invokes, or "" when it invokes none.
func toolNameOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var msg struct {
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return "" // content was a bare string, not a block array
	}
	for _, c := range msg.Content {
		if c.Type == "tool_use" && c.Name != "" {
			return c.Name
		}
	}
	return ""
}

// --- snapshots ---------------------------------------------------------------

// fleetView is an immutable picture of the session for rendering.
type fleetView struct {
	Main   string
	Events int
	Agents []agentView
}

type agentView struct {
	// ID is the stream's task_id, shortened. It is what distinguishes two agents
	// of the same kind working on the same-looking thing — without it a log line
	// says "implementer" and leaves the reader to guess which of three.
	ID       string
	Kind     string
	Desc     string
	Tool     string
	Tokens   int
	ToolUses int
	Status   string
	Elapsed  time.Duration
}

// Running reports whether the sub-agent is still working.
func (a agentView) Running() bool { return a.Status == agentRunning }

// view snapshots the fleet in dispatch order.
func (s *sessionStream) view() fleetView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := fleetView{Main: s.main, Events: s.events}
	if v.Main == "" {
		v.Main = "starting"
	}
	for _, id := range s.order {
		a := s.agents[id]
		end := a.ended
		if end.IsZero() {
			end = s.now()
		}
		v.Agents = append(v.Agents, agentView{
			ID:   shortAgentID(id),
			Kind: a.kind, Desc: a.desc, Tool: a.tool,
			Tokens: a.tokens, ToolUses: a.toolUses, Status: a.status,
			Elapsed: end.Sub(a.started),
		})
	}
	return v
}

// activeAgents counts sub-agents still running.
func (v fleetView) activeAgents() int {
	n := 0
	for _, a := range v.Agents {
		if a.Running() {
			n++
		}
	}
	return n
}

// snapshot reports a one-line activity label and the event count. When sub-agents
// are in flight they are the story, so they win over the orchestrator's own tool.
func (s *sessionStream) snapshot() (activity string, events int) {
	v := s.view()
	if n := v.activeAgents(); n > 0 {
		names := make([]string, 0, n)
		for _, a := range v.Agents {
			if a.Running() {
				names = append(names, agentLabel(a))
			}
		}
		sort.Strings(names)
		return fmt.Sprintf("%d agents · %s", n, strings.Join(names, ", ")), v.Events
	}
	return v.Main, v.Events
}

// agentLabel names a sub-agent as compactly as it can: its work if the dispatch
// described it, else its type.
func agentLabel(a agentView) string {
	if a.Desc != "" {
		return a.Desc
	}
	if a.Kind != "" {
		return a.Kind
	}
	return "agent"
}

// shortAgentID trims a task_id to a readable handle. The full id is a long opaque
// token; eight characters is enough to tell concurrent agents apart in a log and
// short enough that the line still reads as prose.
func shortAgentID(id string) string {
	const width = 8
	if i := strings.LastIndexByte(id, '_'); i >= 0 && i+1 < len(id) {
		id = id[i+1:]
	}
	if len(id) > width {
		return id[:width]
	}
	return id
}

// verdictSource returns the final `result` event when the stream produced one.
func (s *sessionStream) verdictSource() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result, s.result != ""
}

// compactDuration renders d as "2m30s"/"1h04m", dropping the noise time.Duration
// prints at second resolution.
func compactDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if h := int(d.Hours()); h > 0 {
		return fmt.Sprintf("%dh%02dm", h, int(d.Minutes())%60)
	}
	if m := int(d.Minutes()); m > 0 {
		return fmt.Sprintf("%dm%02ds", m, int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
