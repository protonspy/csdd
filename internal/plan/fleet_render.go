package plan

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Rendering the live session.
//
// `plan run` runs both ways: watched in a terminal, and unattended with stdout
// redirected to a file or piped somewhere. Those want opposite things, so the
// renderer picks by destination:
//
//   - a terminal gets the fleet redrawn in place, so a long session reads as a
//     live tree rather than a scrolling wall;
//   - anything else gets append-only lines on real transitions, because cursor
//     control in a log file is garbage and a redraw every second would bury the
//     run's actual output.
//
// Both exist to answer the same question — "is this alive, and what is it
// doing?" — which a silent forty-minute session could not answer at all.

// fleetRefresh is how often the live view redraws. Fast enough to feel live,
// slow enough to cost nothing.
const fleetRefresh = 1 * time.Second

// heartbeatEvery is how often a non-terminal destination gets a "still working"
// line. Rare on purpose: in a log file this is a keepalive, not a display.
const heartbeatEvery = 30 * time.Second

// isTerminal reports whether w is a terminal that can take cursor control. A
// pipe, a file, or a test buffer is not, and gets the append-only renderer.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// sessionReporter renders a session's progress until stop closes.
type sessionReporter interface {
	run(s *sessionStream, stop <-chan struct{})
}

// newReporter picks the renderer that suits the destination. `feat` is the slug of
// the dispatch being reported; every line it emits is attributed to it, so a log
// that will one day interleave several sessions never leaves the reader guessing
// which feat a line belongs to.
func newReporter(out io.Writer, tty bool, now func() time.Time, feat string) sessionReporter {
	if now == nil {
		now = time.Now
	}
	if tty {
		return &liveReporter{out: out, now: now, feat: feat}
	}
	return &logReporter{out: out, now: now, feat: feat}
}

// featTag is the per-dispatch prefix every appended line carries. Fixed-shape and
// leading, so a reader — or a grep — can pick one feat out of an interleaved log
// without parsing the rest of the line.
func featTag(feat string) string {
	if feat == "" {
		return ""
	}
	return "[" + feat + "] "
}

// agentTag names one sub-agent: its kind plus a short handle from the stream's
// task_id. The handle is what makes three concurrent `implementer`s three
// different things rather than one repeated word.
func agentTag(a agentView) string {
	if a.ID == "" {
		return agentKind(a)
	}
	return agentKind(a) + "#" + a.ID
}

// mainLabel names the orchestrating session itself in the live view: the feat it
// is driving, or a bare "main" when there is no feat context.
func mainLabel(feat string) string {
	if feat == "" {
		return "main"
	}
	return feat
}

// --- terminal: redraw in place ----------------------------------------------

type liveReporter struct {
	out   io.Writer
	now   func() time.Time
	feat  string
	lines int // lines painted last frame, to erase before the next
}

func (r *liveReporter) run(s *sessionStream, stop <-chan struct{}) {
	start := r.now()
	t := time.NewTicker(fleetRefresh)
	defer t.Stop()
	for {
		select {
		case <-stop:
			r.erase()
			return
		case <-t.C:
			r.paint(s.view(), r.now().Sub(start))
		}
	}
}

func (r *liveReporter) paint(v fleetView, elapsed time.Duration) {
	var b strings.Builder
	r.eraseInto(&b)

	fmt.Fprintf(&b, "  ● %s  %s · %s (%d events)\n",
		mainLabel(r.feat), compactDuration(elapsed), v.Main, v.Events)
	for _, a := range v.Agents {
		fmt.Fprintf(&b, "  %s %s  %s%s\n", agentBullet(a), agentTag(a), agentLabel(a), agentDetail(a))
	}

	// One line for main, one per agent — what the next frame must erase.
	r.lines = 1 + len(v.Agents)
	_, _ = io.WriteString(r.out, b.String())
}

// erase clears the previous frame so the next one overwrites it.
func (r *liveReporter) erase() {
	var b strings.Builder
	r.eraseInto(&b)
	if b.Len() > 0 {
		_, _ = io.WriteString(r.out, b.String())
	}
	r.lines = 0
}

func (r *liveReporter) eraseInto(b *strings.Builder) {
	if r.lines == 0 {
		return
	}
	// Move up over the previous frame, then clear from the cursor down.
	fmt.Fprintf(b, "\033[%dA\033[J", r.lines)
}

func agentBullet(a agentView) string {
	if a.Running() {
		return "○"
	}
	return "✓"
}

func agentKind(a agentView) string {
	if a.Kind == "" {
		return "agent"
	}
	return a.Kind
}

// agentDetail is the trailing "· Bash · 11k tokens · 42s" annotation.
func agentDetail(a agentView) string {
	var parts []string
	if a.Tool != "" && a.Running() {
		parts = append(parts, a.Tool)
	}
	if a.Tokens > 0 {
		parts = append(parts, compactTokens(a.Tokens))
	}
	if a.Elapsed > 0 {
		parts = append(parts, compactDuration(a.Elapsed))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  · " + strings.Join(parts, " · ")
}

func compactTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.0fk tok", float64(n)/1000)
	}
	return fmt.Sprintf("%d tok", n)
}

// --- pipe/file: append-only transitions --------------------------------------

type logReporter struct {
	out  io.Writer
	now  func() time.Time
	feat string
	seen map[string]string // agent ID → last status reported
}

func (r *logReporter) run(s *sessionStream, stop <-chan struct{}) {
	start := r.now()
	r.seen = map[string]string{}
	// Transitions are checked often so a dispatch is reported promptly; the
	// periodic "still alive" line stays rare so logs do not fill up.
	tick := time.NewTicker(fleetRefresh)
	defer tick.Stop()
	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()

	for {
		select {
		case <-stop:
			r.report(s.view())
			return
		case <-tick.C:
			r.report(s.view())
		case <-beat.C:
			activity, events := s.snapshot()
			r.printf("· %s — %s (%d events)",
				compactDuration(r.now().Sub(start)), activity, events)
		}
	}
}

// report emits a line for each sub-agent whose status changed since last time.
//
// Keyed by agent ID, not by label. Two agents of the same kind routinely carry the
// same description — three `implementer`s all reading "Task 4" — and keying on that
// made them share one status slot, so the second one's dispatch and completion were
// silently swallowed as "already reported".
func (r *logReporter) report(v fleetView) {
	for _, a := range v.Agents {
		key := a.ID
		if key == "" {
			key = agentLabel(a)
		}
		if r.seen[key] == a.Status {
			continue
		}
		r.seen[key] = a.Status
		if a.Running() {
			r.printf("↳ %s dispatched: %s", agentTag(a), agentLabel(a))
		} else {
			r.printf("↳ %s %s: %s%s", agentTag(a), a.Status, agentLabel(a), agentDetail(a))
		}
	}
}

// printf writes one attributed line. The feat tag leads so the output stays
// greppable per dispatch, and the two-space indent keeps it visually nested under
// the runner's own "→ <feat>" heading.
func (r *logReporter) printf(format string, a ...any) {
	_, _ = fmt.Fprintf(r.out, "  "+featTag(r.feat)+format+"\n", a...)
}
