package plan

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/protonspy/csdd/internal/paths"
)

// Account-limit wait tuning. When a session stops because the Claude account hit
// its limit, the runner sleeps until the window reopens instead of counting the
// stop as a failure.
const (
	// limitResetBuffer pads the parsed reset moment so the runner wakes just after
	// the window reopens, never a hair before it.
	limitResetBuffer = 1 * time.Minute
	// limitMinWait floors every limit sleep so a reset that resolves into the past
	// (clock skew, an already-elapsed window) can't spin the loop hot.
	limitMinWait = 1 * time.Minute
	// limitFallbackWait is how long the runner waits when the notice carried no
	// parsable reset time.
	limitFallbackWait = 30 * time.Minute
)

// Hooks are the runner's seams onto the outside world. Every field has a real
// default (installed by Run); tests inject stubs so the whole loop — verdict
// handling, ledger, journal — runs with no `claude` or subprocess at all. The
// runner never touches git or the workspace's specs: authoring, gates, branch,
// commit, and the PR all happen inside the session. The runner only spawns the
// session, records what it declared, and carries context to the next one.
type Hooks struct {
	// Session runs one claude session for a feat and returns its parsed verdict.
	// Every session runs bypass-mode (--dangerously-skip-permissions).
	Session func(feat Feat, brief string, budgetUSD float64) (Verdict, error)
	// Doctor proves sandbox isolation before the bypass-mode loop starts.
	Doctor func() SandboxReport
	// Confirm asks the human a yes/no question (the unverified-sandbox alert)
	// and reports whether they accepted. The real hook reads stdin, so a
	// non-interactive run (EOF) declines by default.
	Confirm func(prompt string) bool
	// ClaudeAvailable reports whether the `claude` CLI is on PATH.
	ClaudeAvailable func() bool
	// Now returns the current time (journal/ledger timestamps; injected for tests).
	Now func() time.Time
	// Sleep pauses the loop for d. The only caller is the account-limit wait, which
	// sleeps until the session window reopens; tests inject a stub that records the
	// duration and returns immediately.
	Sleep func(d time.Duration)
}

// RunOptions configures a `csdd plan run` invocation. The loop is deliberately
// dumb (Ralph-style): one iteration is exactly one claude session handed one whole
// feat, and the only ways it ends are the plan completing, the stall guard, or the
// iteration cap. The session — trusted, driving csdd/git itself — owns the flow;
// the runner owns only the ledger and the context handed between sessions.
type RunOptions struct {
	Root          string
	Slug          string
	AssumeYes     bool    // accept the unverified-sandbox alert without prompting (--yes)
	SessionBudget float64 // per-session --max-budget-usd; 0 = no cap (Claude account limits)
	// Model and Effort are the claude --model / --effort each orchestrating session
	// runs on. The orchestrator authors and decides (spec phases); it delegates task
	// implementation to the `implementer` sub-agent, which runs on that agent's own
	// (cheaper, faster) model. Empty means "inherit the ambient default" — the flag
	// is omitted. The CLI defaults these to opus / high.
	Model         string
	Effort        string
	MaxIterations int // sessions the run may spend; default 100
	// Stall ends the run after this many consecutive iterations that FAILED (a
	// session error or an unparseable verdict) with no forward motion. Honest
	// partial work (`continue`) resets it, so the stall guard catches a broken
	// loop, not a large feat that legitimately spans many sessions; the iteration
	// cap is the real wallet guard. Default 10.
	Stall int
	// SessionIdle bounds how long a session may make no progress at all — no
	// output on its event stream and no CPU anywhere in its process group — before
	// the runner kills it as hung. It is NOT a time limit on a session: honest work
	// of any duration keeps resetting it. Default 15 minutes (--session-idle).
	SessionIdle time.Duration
	Out         io.Writer
	Hooks       Hooks
}

// Run outcomes, chosen so the CLI can surface them as distinct exit codes.
const (
	OutcomeComplete = 0 // every feat is delivered
	OutcomeCapped   = 6 // hit the iteration cap without finishing
	OutcomeStalled  = 8 // the stall guard tripped: consecutive failures with no progress
)

// RunSummary is the totals reported on exit (R9.7).
type RunSummary struct {
	Sessions  int // claude sessions spent (= iterations that reached a session)
	Steps     int // feats delivered (the ledger gained a done row)
	Failures  int // iterations that failed and became context for the next one
	Completed bool
	Reason    string
	Outcome   int
}

// Run drives the autonomous plan loop (R9): preflight, then one session per
// iteration — each handed one whole feat's mission brief plus the rolling context
// its predecessors left — until every feat is delivered. The session is trusted:
// when it declares `done`, the runner records the feat in the ledger and moves on;
// when it declares `continue`, the same feat comes back with the handoff. A failure
// (session error) is never a dead end — it lands on the feat's rolling history,
// which the next session's brief carries, and the loop self-corrects.
func Run(opts RunOptions) (RunSummary, error) {
	fillRunDefaults(&opts)
	h := opts.Hooks
	out := opts.Out
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }

	doc, err := Load(opts.Root, opts.Slug)
	if err != nil {
		return RunSummary{}, err
	}

	// Preflight (R9.1): approval + drift, claude present, sandbox. Approval is a
	// single go/no-go stamp — the loop does not police plan.md edits mid-run.
	approved, drift, err := IsApproved(opts.Root, opts.Slug)
	if err != nil {
		return RunSummary{}, err
	}
	if !approved {
		return RunSummary{}, fmt.Errorf("plan %q is not approved; run `csdd plan approve %s` first", opts.Slug, opts.Slug)
	}
	if drift {
		return RunSummary{}, fmt.Errorf("plan %q has drifted since approval; re-approve before running", opts.Slug)
	}
	if !h.ClaudeAvailable() {
		return RunSummary{}, fmt.Errorf("the `claude` CLI is not available on PATH; the runner needs it to spawn sessions")
	}
	// Every session runs bypass-mode (--dangerously-skip-permissions), so a
	// verified sandbox is the expected home (principle 7). When doctor fails, the
	// run only proceeds on an explicit human accept — declining closes the run.
	if rep := h.Doctor(); rep.OK {
		logf("sandbox verified: isolation is enforced")
	} else {
		logf("⚠ sandbox NOT verified — plan run drives every session with --dangerously-skip-permissions:")
		for _, c := range rep.Checks {
			if !c.OK {
				logf("  ✗ %-18s %s", c.Name, c.Detail)
			}
		}
		if !opts.AssumeYes && !h.Confirm("Continue WITHOUT sandbox isolation? [y/N] ") {
			return RunSummary{}, fmt.Errorf("run closed: sandbox not verified and continuing was declined — fix isolation (`csdd sandbox init` + `csdd sandbox doctor`) or pass --yes to accept the risk")
		}
		logf("continuing WITHOUT a verified sandbox (explicitly accepted)")
	}
	logf("plan run %s (bypass mode, %s, %s, max %d sessions, stall guard %d)",
		opts.Slug, sessionModelLabel(opts.Model, opts.Effort), budgetLabel(opts.SessionBudget), opts.MaxIterations, opts.Stall)

	ledger := LoadLedger(opts.Root, opts.Slug)
	// Resume honestly: a feat already delivered on disk — developed by hand in a
	// session, or shipped by an earlier run whose transient ledger was cleaned —
	// is imported into the ledger so the loop skips it instead of redoing finished
	// work. This never unmarks; it only records completion the ledger did not know.
	reconcileLedgerFromDisk(opts, doc, ledger, logf)
	st := newRunState()
	var sum RunSummary
	stall := 0

	for iter := 1; iter <= opts.MaxIterations; iter++ {
		feat, ok := nextFeat(doc, ledger.doneSet())
		if !ok {
			sum.Completed = true
			sum.Outcome = OutcomeComplete
			sum.Reason = "plan complete: every feat is delivered"
			logf(sum.Reason)
			return summarize(out, sum), nil
		}

		logf("→ %s (session %d/%d)", feat.Slug, iter, opts.MaxIterations)
		switch executeFeat(opts, doc, feat, ledger, st, &sum) {
		case iterAdvanced, iterContinue:
			stall = 0 // a delivered feat or honest partial work is forward motion
		case iterFailed:
			sum.Failures++
			stall++
		}
		if stall >= opts.Stall {
			sum.Outcome = OutcomeStalled
			sum.Reason = fmt.Sprintf("stalled: %d consecutive sessions failed without progress — the failure log says where", stall)
			logf(sum.Reason)
			return summarize(out, sum), nil
		}
	}

	sum.Outcome = OutcomeCapped
	sum.Reason = fmt.Sprintf("reached the iteration cap (%d) — `csdd plan run %s` resumes from here", opts.MaxIterations, opts.Slug)
	logf(sum.Reason)
	return summarize(out, sum), nil
}

// iterResult is what one iteration (one session) produced.
type iterResult int

const (
	iterFailed   iterResult = iota // context for the next iteration
	iterAdvanced                   // the feat was delivered (ledger recorded it)
	iterContinue                   // honest partial work, handoff recorded
)

// executeFeat runs exactly one session for the feat and records what it declared.
// Nothing here verifies the work — the loop trusts the session (Ralph-style). A
// `done` verdict marks the feat in the ledger and advances; a `continue` stores the
// handoff for the successor; a session error lands on the feat's rolling history,
// which the next iteration's brief carries.
func executeFeat(opts RunOptions, doc *PlanDoc, feat Feat, ledger *Ledger, st *runState, sum *RunSummary) iterResult {
	h := opts.Hooks
	logf := runLogf(opts)
	key := feat.Slug
	hist := st.hist(key)

	base, err := FeatBrief(opts.Root, doc, feat)
	if err != nil {
		hist.add("brief assembly failed", err.Error())
		journal(opts, feat.Slug, "failed", "brief assembly: "+firstLine(err.Error()))
		logf("  ✗ brief error: %v", err)
		return iterFailed
	}

	verdict, err := sessionOrWait(opts, feat, base+runContext(st, key))
	sum.Sessions++
	if err != nil {
		hist.add("session error", err.Error())
		st.logs[key] = writeFailureLog(opts, feat, hist)
		journal(opts, feat.Slug, "failed", "session error: "+firstLine(err.Error()))
		logf("  ✗ session error: %v", err)
		return iterFailed
	}

	switch verdict.Status {
	case VerdictContinue:
		st.handoffs[key] = strings.TrimSpace(verdict.Summary)
		journal(opts, feat.Slug, "progress", "handoff: "+oneLine(verdict.Summary))
		logf("  … progress — handing off to the next session")
		return iterContinue
	case VerdictDone:
		ledger.MarkDone(feat.Slug, oneLine(verdict.Summary), h.Now())
		if err := ledger.Save(opts.Root, opts.Slug); err != nil {
			hist.add("ledger save failed", err.Error())
			journal(opts, feat.Slug, "failed", "ledger save: "+firstLine(err.Error()))
			logf("  ✗ could not record %s as delivered: %v", feat.Slug, err)
			return iterFailed
		}
		journalDone(opts, feat.Slug, oneLine(verdict.Summary))
		st.clearFeat(key)
		sum.Steps++
		logf("  ✓ %s delivered", feat.Slug)
		return iterAdvanced
	default:
		// parseVerdict enforces done|continue, so this is defensive only.
		hist.add("unknown verdict", verdict.Status)
		journal(opts, feat.Slug, "failed", "unknown verdict status: "+verdict.Status)
		logf("  ✗ unknown verdict status %q", verdict.Status)
		return iterFailed
	}
}

// sessionOrWait runs one session, transparently absorbing account-limit stops:
// when the session reports "you've hit your session limit", the runner sleeps
// until the reset window reopens and retries the SAME feat, looping until it gets
// a real result. From executeFeat's view a limit is invisible — it never counts as
// a failure, burns the stall guard, or pollutes the failure log. The iteration is
// not consumed either, since the caller only advances once this returns.
func sessionOrWait(opts RunOptions, feat Feat, brief string) (Verdict, error) {
	h := opts.Hooks
	for {
		v, err := h.Session(feat, brief, opts.SessionBudget)
		var lim *LimitError
		if errors.As(err, &lim) {
			waitForLimit(opts, feat, lim)
			continue
		}
		return v, err
	}
}

// waitForLimit logs the account-limit pause and sleeps until the session window
// reopens. The reset moment comes from the notice ("resets 10:10pm
// (America/Sao_Paulo)"); when it can't be parsed the runner falls back to a fixed
// wait, and every wait is floored so a stale/past reset can't spin the loop.
func waitForLimit(opts RunOptions, feat Feat, lim *LimitError) {
	logf := runLogf(opts)
	now := opts.Hooks.Now()
	wait, label := limitFallbackWait, "an unknown reset time"
	if reset, ok := lim.Reset(now); ok {
		wait = reset.Sub(now) + limitResetBuffer
		label = reset.Format("2006-01-02 15:04 MST")
	}
	if wait < limitMinWait {
		wait = limitMinWait
	}
	logf("  ⏸ hit the Claude session limit — sleeping %s until %s, then resuming %s",
		wait.Round(time.Second), label, feat.Slug)
	journal(opts, feat.Slug, "waiting", "session limit — resuming after "+label)
	sleepWithCountdown(opts, wait, label)
}

// limitTick is how often the account-limit wait reports the time still remaining.
const limitTick = 5 * time.Minute

// sleepWithCountdown sleeps for total, reporting what is left while it waits.
//
// A limit wait can legitimately run for hours. Spending it in one silent Sleep is
// what made a rate-limited run indistinguishable from a crashed one: the runner
// was behaving exactly as designed and the operator had no way to know that.
//
// The countdown runs on its own ticker rather than by chopping the wait into
// several Sleep calls, which keeps the Sleep hook's contract intact — one call per
// limit, carrying the full duration — so it stays a faithful seam for tests. With
// an instant Sleep stub the ticker simply never fires. The countdown reads the wall
// clock directly for the same reason: it reports real waiting, not loop time.
func sleepWithCountdown(opts RunOptions, total time.Duration, label string) {
	logf := runLogf(opts)
	deadline := time.Now().Add(total)
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(limitTick)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if left := time.Until(deadline); left > 0 {
					logf("    · still waiting on the session limit — %s left (until %s)",
						compactDuration(left), label)
				}
			}
		}
	}()
	opts.Hooks.Sleep(total)
	close(stop)
}

// reconcileLedgerFromDisk seeds the ledger from disk reality before the loop so a
// plan advanced outside this run resumes correctly. For every feat whose spec is
// fully delivered on disk (StateDone: all phases approved and all tasks checked)
// but that the ledger does not yet record, it marks the feat done and journals it
// as reconciled. It never unmarks a feat — the ledger stays the loop's source of
// truth; this only imports completion the loop could not have observed (hand
// development in a session, or an earlier run whose transient ledger was cleaned).
func reconcileLedgerFromDisk(opts RunOptions, doc *PlanDoc, ledger *Ledger, logf func(string, ...any)) {
	done := ledger.doneSet()
	changed := false
	for _, f := range doc.Feats {
		if done[f.Slug] {
			continue
		}
		if deriveFeatStatus(opts.Root, f, done).State != StateDone {
			continue
		}
		ledger.MarkDone(f.Slug, "reconciled: spec fully delivered on disk before this run", opts.Hooks.Now())
		journal(opts, f.Slug, "done", "reconciled from disk (spec fully delivered before this run)")
		logf("  ✓ %s already delivered on disk — recorded in the ledger", f.Slug)
		changed = true
	}
	if changed {
		if err := ledger.Save(opts.Root, opts.Slug); err != nil {
			logf("  ⚠ could not persist the reconciled ledger: %v", err)
		}
	}
}

// --- run state: the loop's memory across iterations ---------------------------

// runState is what one iteration hands the next. Sessions are fresh processes;
// anything not carried here (or written to disk) is lost between them. Keyed by
// feat slug, since a feat is now the whole unit of work.
type runState struct {
	hists    map[string]*failureHistory // feat → failed attempts
	handoffs map[string]string          // feat → continue-verdict handoff
	logs     map[string]string          // feat → workspace-relative failure log
}

func newRunState() *runState {
	return &runState{hists: map[string]*failureHistory{}, handoffs: map[string]string{}, logs: map[string]string{}}
}

func (s *runState) hist(key string) *failureHistory {
	if s.hists[key] == nil {
		s.hists[key] = &failureHistory{}
	}
	return s.hists[key]
}

// clearFeat forgets a feat's trail once it is delivered.
func (s *runState) clearFeat(key string) {
	delete(s.hists, key)
	delete(s.handoffs, key)
	delete(s.logs, key)
}

// maxContextAttempts bounds how many failed attempts the rolling context replays.
// The full history is always in the on-disk failure log; the brief carries the
// recent tail because a session that cannot converge after five replayed attempts
// needs a different angle, not a longer transcript.
const maxContextAttempts = 5

// runContext renders the rolling context one iteration hands the next: the
// predecessor's handoff and the feat's failure trail. Appended after the
// deterministic FeatBrief, so the mission part of the prompt stays byte-identical
// (R7.3) and only the memory varies.
func runContext(st *runState, key string) string {
	hist := st.hists[key]
	histLen := 0
	if hist != nil {
		histLen = hist.len()
	}
	handoff := st.handoffs[key]
	logRel := st.logs[key]
	if handoff == "" && histLen == 0 && logRel == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Autonomous run context\n\n")
	b.WriteString("You are one session of a self-correcting loop working this feat; previous\n")
	b.WriteString("sessions left the state below. Diagnose the ROOT CAUSE before editing — never\n")
	b.WriteString("replay an approach that already failed.\n")
	if handoff != "" {
		b.WriteString("\n### Handoff from the previous session (it reported progress)\n\n" + handoff + "\n")
	}
	if histLen > 0 {
		fmt.Fprintf(&b, "\n### This feat already had %d failed session(s)\n\n", histLen)
		if logRel != "" {
			fmt.Fprintf(&b, "Full untruncated output: %s\n\n", logRel)
		}
		b.WriteString(hist.renderTail(maxContextAttempts, briefFailureCap))
	} else if logRel != "" {
		fmt.Fprintf(&b, "\n### A previous run left a failure log for this feat\n\nRead %s before starting.\n", logRel)
	}
	return b.String()
}

// --- failure history ----------------------------------------------------------

// briefFailureCap bounds how much of one attempt's output the context carries. A
// failing session can print a lot; the model needs the gist, not the transcript,
// and the untruncated text is on disk in the failure log regardless.
const briefFailureCap = 4000

// attempt is one failed try at a feat: which stage failed, and its full output.
type attempt struct {
	n      int
	stage  string
	output string
}

// failureHistory accumulates every failed attempt at a feat across the run. The
// rolling context replays the recent tail — a root cause is usually visible across
// attempts and invisible within one.
type failureHistory struct{ attempts []attempt }

func (h *failureHistory) add(stage, output string) {
	h.attempts = append(h.attempts, attempt{n: len(h.attempts) + 1, stage: stage, output: strings.TrimRight(output, "\n")})
}

func (h *failureHistory) len() int { return len(h.attempts) }

// renderTail lays out the last n attempts, each capped at cap bytes; anything older
// stays in the on-disk failure log.
func (h *failureHistory) renderTail(n, cap int) string {
	var b strings.Builder
	start := 0
	if len(h.attempts) > n {
		start = len(h.attempts) - n
		fmt.Fprintf(&b, "(%d older attempt(s) omitted — see the failure log)\n\n", start)
	}
	for _, a := range h.attempts[start:] {
		fmt.Fprintf(&b, "### Attempt %d — %s\n\n```\n%s\n```\n\n", a.n, a.stage, tailCap(a.output, cap))
	}
	return b.String()
}

// tailCap keeps the last n bytes of s, aligned to a rune boundary. The tail is the
// informative end of a build or test failure — the summary lives after the noise.
func tailCap(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return "…(truncated — the full output is in the failure log)…\n" + s[i:]
}

// writeFailureLog persists every attempt's untruncated output under the runner's
// state dir and returns its workspace-relative path, which the rolling context
// points at. It is rewritten (not appended) so the file always mirrors the history
// exactly. An I/O failure yields "" — a lost log must never take the run down.
func writeFailureLog(opts RunOptions, feat Feat, hist *failureHistory) string {
	rel := filepath.Join(paths.StateDir, "plan", opts.Slug, "failures", feat.Slug+".log")
	var b strings.Builder
	fmt.Fprintf(&b, "# plan %s · feat %s\n", opts.Slug, feat.Slug)
	fmt.Fprintf(&b, "# %s · %d attempt(s)\n\n", opts.Hooks.Now().UTC().Format(time.RFC3339), hist.len())
	for _, a := range hist.attempts {
		fmt.Fprintf(&b, "## attempt %d — %s\n\n%s\n\n", a.n, a.stage, a.output)
	}
	path := filepath.Join(opts.Root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// --- journal + summary --------------------------------------------------------

// journal appends one mechanical line to the run journal (log.md), plus a detail
// line per detail (§5.6). The step column is a historical artifact — whole-feat
// runs pass "-".
func journal(opts RunOptions, feat, outcome string, details ...string) {
	appendJournal(Dir(opts.Root, opts.Slug), opts.Hooks.Now(), "-", feat, outcome, details...)
}

// journalDone records a delivered feat, carrying the session's summary as a detail
// only when it left one.
func journalDone(opts RunOptions, feat, summary string) {
	if summary == "" {
		journal(opts, feat, "done")
		return
	}
	journal(opts, feat, "done", summary)
}

// runLogf is the runner's line-oriented logger onto opts.Out.
func runLogf(opts RunOptions) func(string, ...any) {
	return func(format string, a ...any) { _, _ = fmt.Fprintf(opts.Out, format+"\n", a...) }
}

// --- small helpers ------------------------------------------------------------

func fillRunDefaults(opts *RunOptions) {
	// SessionBudget <= 0 means "no per-session cap" — the session runs under the
	// Claude account's own limits (the default). A positive value pins
	// --max-budget-usd for a tighter ceiling.
	if opts.SessionBudget < 0 {
		opts.SessionBudget = 0
	}
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 100
	}
	if opts.Stall <= 0 {
		opts.Stall = 10
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.SessionIdle <= 0 {
		opts.SessionIdle = defaultSessionIdle
	}
	// Hooks.Now may still be nil here; the reporter resolves that itself, and
	// installRealHooks fills the hook right after.
	installRealHooks(&opts.Hooks, opts.Model, opts.Effort, sessionEnv{
		idle: opts.SessionIdle,
		out:  opts.Out,
		tty:  isTerminal(opts.Out),
		now:  opts.Hooks.Now,
	})
}

func summarize(out io.Writer, sum RunSummary) RunSummary {
	_, _ = fmt.Fprintf(out, "totals: %d sessions, %d feats delivered, %d failed iterations\n",
		sum.Sessions, sum.Steps, sum.Failures)
	return sum
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// budgetLabel describes the per-session budget for the run log: the Claude
// account's own limit when unset, else the explicit USD cap.
func budgetLabel(budgetUSD float64) string {
	if budgetUSD <= 0 {
		return "budget: account limit"
	}
	return fmt.Sprintf("budget: $%.2f/session", budgetUSD)
}

// sessionModelLabel describes the orchestrating session's model/effort for the run
// log. Either field may be empty ("inherit the ambient default"), so it renders
// only what was pinned and falls back to "session default" when neither is set.
func sessionModelLabel(model, effort string) string {
	m, e := strings.TrimSpace(model), strings.TrimSpace(effort)
	switch {
	case m != "" && e != "":
		return "orchestrator: " + m + "/" + e
	case m != "":
		return "orchestrator: " + m
	case e != "":
		return "orchestrator: effort " + e
	default:
		return "orchestrator: session default"
	}
}
