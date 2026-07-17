package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
)

// defaultInterval is how often the notifier polls the workspace when the config
// / flag leaves it unset. A few seconds is plenty for human-facing feedback and
// keeps the disk reads trivial.
const defaultInterval = 5 * time.Second

// Options configures a Notifier.
type Options struct {
	Root     string
	Client   *Client
	Interval time.Duration
	// Out receives local terminal feedback (relayed lines, warnings). Defaults to
	// os.Stderr when nil.
	Out io.Writer
}

// Notifier polls the workspace and relays two feeds to Telegram: new lines
// appended to each plan's run journal (docs/plans/<slug>/log.md) and changes to
// any spec's phase/approvals (specs/<feature>/spec.json).
type Notifier struct {
	root     string
	client   *Client
	interval time.Duration
	out      io.Writer

	offsets  map[string]int64     // log.md path → bytes already relayed
	seeded   bool                 // first poll seeds baselines instead of relaying
	lastSpec map[string]specState // last relayed spec snapshot
}

// NewNotifier builds a Notifier with sane defaults.
func NewNotifier(opts Options) *Notifier {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	return &Notifier{
		root:     opts.Root,
		client:   opts.Client,
		interval: interval,
		out:      out,
		offsets:  map[string]int64{},
	}
}

// Run seeds the baselines (so startup never replays history), sends a startup
// summary, then polls until ctx is cancelled.
func (n *Notifier) Run(ctx context.Context) error {
	n.planUpdates()                       // seed log offsets to EOF; discard history
	n.lastSpec = readSpecSnapshot(n.root) // seed spec baseline

	n.send(ctx, n.startupMessage())

	ticker := time.NewTicker(n.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			n.flush()
			return nil
		case <-ticker.C:
			n.tick(ctx)
		}
	}
}

// flush relays one final round after the context is cancelled, so a run that just
// finished still delivers the journal lines appended since the last poll (the
// embedded plan-run notifier cancels the moment the loop returns). It uses a
// fresh, short-lived context because the caller's is already done.
func (n *Notifier) flush() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n.tick(ctx)
}

// tick relays one round of both feeds.
func (n *Notifier) tick(ctx context.Context) {
	updates := n.planUpdates()
	for _, slug := range sortedKeys(updates) {
		n.send(ctx, fmt.Sprintf("▶️ plan %s\n%s", slug, updates[slug]))
	}
	cur := readSpecSnapshot(n.root)
	for _, msg := range diffSpecs(n.lastSpec, cur) {
		n.send(ctx, msg)
	}
	n.lastSpec = cur
}

// send relays one message and logs the outcome locally. A send failure is
// warned but never fatal — the notifier keeps polling.
func (n *Notifier) send(ctx context.Context, text string) {
	if err := n.client.Send(ctx, text); err != nil {
		_, _ = fmt.Fprintln(n.out, render.Yellow("!")+" telegram: "+err.Error())
		return
	}
	_, _ = fmt.Fprintln(n.out, render.Green("→")+" telegram: "+firstLine(text))
}

// planUpdates returns, per plan slug, the log.md text appended since the last
// poll. The first call seeds every existing log to EOF and returns nothing, so
// startup relays only future progress rather than the whole history. Logs that
// appear *after* startup are relayed from their first byte.
func (n *Notifier) planUpdates() map[string]string {
	out := map[string]string{}
	matches, _ := filepath.Glob(filepath.Join(paths.DocsPlans(n.root), "*", "log.md"))
	for _, path := range matches {
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := fi.Size()
		prev, seen := n.offsets[path]
		switch {
		case !seen && !n.seeded:
			n.offsets[path] = size // startup: skip pre-existing history
			continue
		case !seen:
			prev = 0 // plan that appeared after startup: relay from the top
		case size < prev:
			prev = 0 // log was truncated/rewritten: relay from the top
		case size == prev:
			continue
		}
		text := readFrom(path, prev)
		n.offsets[path] = size
		if strings.TrimSpace(text) != "" {
			out[slugOf(path)] = strings.TrimRight(text, "\n")
		}
	}
	n.seeded = true
	return out
}

// startupMessage is the one-shot banner + current spec snapshot sent on Run.
func (n *Notifier) startupMessage() string {
	var b strings.Builder
	fmt.Fprintf(&b, "🤖 csdd telegram ligado — %s\n", filepath.Base(n.root))
	if len(n.lastSpec) == 0 {
		b.WriteString("Nenhuma spec ainda. Aguardando plan run e spec status…")
		return b.String()
	}
	b.WriteString("Specs:\n")
	for _, f := range sortedSpecKeys(n.lastSpec) {
		fmt.Fprintf(&b, "• %s — %s\n", f, stateSummary(n.lastSpec[f]))
	}
	b.WriteString("Aguardando atualizações…")
	return b.String()
}

// --- spec snapshot + diff (pure, testable) ---------------------------------

// specState is the slice of a spec.json the notifier watches for change.
type specState struct {
	Phase    string
	Approved []string
	Ready    bool
}

// readSpecSnapshot reads specs/<feature>/spec.json for every feature and
// returns its watched state. A missing/unreadable specs dir yields an empty
// snapshot (the notifier simply idles until specs appear).
func readSpecSnapshot(root string) map[string]specState {
	snap := map[string]specState{}
	entries, err := os.ReadDir(paths.Specs(root))
	if err != nil {
		return snap
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(paths.Specs(root), e.Name(), "spec.json"))
		if err != nil {
			continue
		}
		var s struct {
			Phase                  string `json:"phase"`
			ReadyForImplementation bool   `json:"ready_for_implementation"`
			Approvals              map[string]struct {
				Approved bool `json:"approved"`
			} `json:"approvals"`
		}
		if json.Unmarshal(data, &s) != nil {
			continue
		}
		var approved []string
		for k, v := range s.Approvals {
			if v.Approved {
				approved = append(approved, k)
			}
		}
		sort.Strings(approved)
		snap[e.Name()] = specState{Phase: s.Phase, Approved: approved, Ready: s.ReadyForImplementation}
	}
	return snap
}

// diffSpecs returns one message per spec that was added, changed, or removed
// between two snapshots (in a stable order). An unchanged spec yields nothing.
func diffSpecs(old, cur map[string]specState) []string {
	var msgs []string
	for _, f := range sortedSpecKeys(cur) {
		c := cur[f]
		o, existed := old[f]
		switch {
		case !existed:
			msgs = append(msgs, fmt.Sprintf("🆕 spec %s — %s", f, stateSummary(c)))
		case !sameState(o, c):
			msgs = append(msgs, fmt.Sprintf("📋 spec %s — %s", f, stateSummary(c)))
		}
	}
	for _, f := range sortedSpecKeys(old) {
		if _, ok := cur[f]; !ok {
			msgs = append(msgs, fmt.Sprintf("🗑 spec %s removida", f))
		}
	}
	return msgs
}

// stateSummary renders a spec's watched state as one human line.
func stateSummary(s specState) string {
	approved := "—"
	if len(s.Approved) > 0 {
		approved = strings.Join(s.Approved, ",")
	}
	tag := ""
	if s.Ready {
		tag = " ✅ pronta p/ implementação"
	}
	return fmt.Sprintf("phase %s · aprovado: %s%s", s.Phase, approved, tag)
}

// sameState reports whether two spec states are equal (phase, ready, approvals).
func sameState(a, b specState) bool {
	if a.Phase != b.Phase || a.Ready != b.Ready || len(a.Approved) != len(b.Approved) {
		return false
	}
	for i := range a.Approved {
		if a.Approved[i] != b.Approved[i] {
			return false
		}
	}
	return true
}

// --- small helpers ---------------------------------------------------------

func slugOf(logPath string) string { return filepath.Base(filepath.Dir(logPath)) }

// readFrom returns the bytes of path from offset to EOF ("" on any error).
func readFrom(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return string(b)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedSpecKeys(m map[string]specState) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
