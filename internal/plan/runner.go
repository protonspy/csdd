package plan

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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
// runner never touches the workspace's specs: authoring, gates, branch, commit, and
// the PR all happen inside the session. Its only git responsibility is the isolation
// the squad needs — it gives each feat a worktree to work in and merges the branch
// back into the run's base once a `done` clears the verdict gate (tree.go). Beyond
// that it spawns the session, records what it declared, and carries context to the
// next one.
type Hooks struct {
	// Session runs one claude session for a feat and returns what it produced:
	// the verdict it declared plus the metrics its `result` event reported. The
	// outcome is returned even alongside an error, so a failed attempt's cost is
	// still recorded (R9.2). Every session runs bypass-mode
	// (--dangerously-skip-permissions), in the feat's own worktree.
	Session func(SessionRequest) (SessionOutcome, error)
	// Trees owns the isolated worktree each feat's sessions run in. Filled by
	// installRealHooks with the git-backed keeper once preflight has resolved the
	// run's base branch.
	Trees treeKeeper
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
	// Enrich runs the cheap discovery pass that produces a feat's context pack,
	// returning the raw JSON its model answered with. Nil disables enrichment: the
	// brief then carries only what the plan states, which is the behavior every run
	// had before the pass existed.
	Enrich func(EnrichRequest) (string, error)
}

// SessionRequest is everything one dispatch hands the Session hook. It is a struct
// rather than a parameter list because Dir arrived late and matters most: a session
// that runs anywhere but its feat's own worktree is a session sharing an index with
// its peers, which is the failure the squad exists to avoid.
type SessionRequest struct {
	Feat      Feat
	Brief     string  // the mission brief plus the rolling context
	BudgetUSD float64 // per-session --max-budget-usd; 0 = the account's own limits
	Dir       string  // the feat's worktree — the session's working directory
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
	MaxIterations int // sessions the run may spend; default 30
	// Stall ends the run after this many consecutive iterations that FAILED (a
	// session error or an unparseable verdict) with no forward motion. Honest
	// partial work (`continue`) resets it, so the stall guard catches a broken
	// loop, not a large feat that legitimately spans many sessions; the iteration
	// cap is the real wallet guard. Default 10.
	Stall int
	// FeatAttempts bounds how many sessions ONE feat may consume before the runner
	// stops handing it out and surfaces it as blocked (R10.4, R10.5).
	//
	// It exists because the verdict gate converts a refused `done` into a
	// `continue`, and `continue` resets the stall guard — so without this bound a
	// session that is confidently wrong about being finished would be re-dispatched
	// until the global iteration cap, turning a silent false-done into a silent
	// infinite loop. The two ship together. Default 4.
	FeatAttempts int
	// SessionIdle bounds how long a session may make no progress at all — no
	// output on its event stream and no CPU anywhere in its process group — before
	// the runner kills it as hung. It is NOT a time limit on a session: honest work
	// of any duration keeps resetting it. Default 15 minutes (--session-idle).
	SessionIdle time.Duration
	// SquadLimit is how many claude sessions may run at once, each on its own feat.
	// Bounded 1..6 by the CLI: the ceiling is the widest topological wave a real
	// plan admitted, and a plan cannot use more concurrency than its own dependency
	// graph allows. Default 1 — a serial run, which is what every plan gets unless
	// it asks for more.
	//
	// Feats run together whenever the graph allows it; each works in its own git
	// worktree, so they never share a working tree (tree.go). Everything the loop
	// does with what a session returns still happens on one goroutine, so the
	// ledger, the journal and the run's bookkeeping are written in exactly the order
	// they were before — only the sessions themselves overlap.
	SquadLimit int
	// EnrichModel is the claude --model the context-enrichment pass runs on. It is a
	// separate knob from Model because it is a separate job: the pass reads the tree
	// and fills a bounded schema, which a cheap model does well, and it exists to
	// keep that reading OFF the orchestrator's model. Empty disables enrichment
	// entirely — the brief then carries only what the plan states. The CLI defaults
	// it to sonnet (--enrich-model, `none` to turn it off).
	EnrichModel string
	// WorktreeEntry is the CLAUDE.md written into each feat's worktree, replacing
	// the repository's own for the life of the run. Empty leaves the repository's
	// file in place. The CLI fills it from the plan-session template; the runner
	// only carries it to the tree keeper.
	WorktreeEntry string
	Out           io.Writer
	Hooks         Hooks
}

// Run outcomes, chosen so the CLI can surface them as distinct exit codes.
const (
	OutcomeComplete    = 0  // every feat is delivered
	OutcomeCapped      = 6  // hit the iteration cap without finishing
	OutcomeStalled     = 8  // the stall guard tripped: consecutive failures with no progress
	OutcomeBlocked     = 9  // a feat exhausted its attempt bound; the run finished around it
	OutcomeSpawnFailed = 10 // `claude` could not be started at all; the environment is broken
)

// RunSummary is the totals reported on exit (R9.7).
type RunSummary struct {
	Sessions  int      // claude sessions spent (= iterations that reached a session)
	Steps     int      // feats delivered (the ledger gained a done row)
	Failures  int      // iterations that failed and became context for the next one
	Gated     int      // `done` verdicts the gate refused and converted to `continue` (R10.3)
	Blocked   []string // feats that exhausted their attempt bound (R10.5)
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
	// Where the feats work. Skipped when a treeKeeper was injected: the loop's own
	// tests drive the whole run against plain directories and no git at all.
	if opts.Hooks.Trees == nil {
		keeper, err := resolveTrees(opts, logf)
		if err != nil {
			return RunSummary{}, err
		}
		opts.Hooks.Trees = keeper
		h = opts.Hooks
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
	// Rebuild what the loop knew when it was last interrupted. A fresh plan yields
	// an empty state, so this is invisible unless there is genuinely something to
	// resume (R3).
	st := restoreRunState(opts.Root, opts.Slug, opts.FeatAttempts, ledger)
	// Dependency edges earlier runs discovered. They are unioned with the plan's own
	// by the scheduler, so a feat that was found to need a peer stays parked across
	// runs instead of being re-dispatched to rediscover the same wall.
	st.extraDeps = LoadDiscoveredDeps(opts.Root, opts.Slug)
	if n := len(st.extraDeps); n > 0 {
		logf("carrying %d discovered dependency edge(s) from earlier runs (%s)", n, DiscoveredDepsFile)
	}
	if s := st.resumeSummary(); s != "" {
		logf("%s", s)
		// Durable, not just on stdout. A run that resumed — and especially one that
		// resumed over an attempt which died mid-session — is exactly the event the
		// journal exists to preserve; printing it only to a terminal nobody was
		// watching leaves no trace of why a feat's budget was already partly spent.
		journal(opts, "-", "resumed", s)
	}
	if opts.SquadLimit > 1 {
		logf("squad: up to %d sessions at once — a feat runs alone unless it and every peer in flight is marked (P)", opts.SquadLimit)
	}

	var sum RunSummary
	stall := 0
	// iter counts sessions dispatched, not loop turns: with a squad in flight
	// several are open at once, and --max-iterations bounds how many the run may
	// spend in total.
	iter := 0
	inflight := map[string]Feat{}
	results := make(chan *dispatch, opts.SquadLimit)
	// stopping means the run's closing verdict is decided and no further feat is
	// dispatched. The loop keeps draining what is already in flight, because a
	// session that is mid-flight has already been paid for — abandoning its verdict
	// would lose the feat it may have just delivered.
	stopping := false
	stop := func(outcome int, reason string) {
		if stopping {
			return
		}
		stopping = true
		sum.Outcome, sum.Reason = outcome, reason
		logf("%s", reason)
		if len(inflight) > 0 {
			logf("  … waiting for %d session(s) still in flight", len(inflight))
		}
	}

	// note folds one iteration's outcome into the run's bookkeeping. Both paths that
	// produce one — a dispatch that never reached a session, and a settled session —
	// go through it, so the stall guard and the attempt bound have a single policy.
	note := func(feat Feat, res iterResult) {
		switch res {
		case iterAdvanced, iterContinue:
			stall = 0 // a delivered feat or honest partial work is forward motion
		case iterParked:
			// Parking is neither progress nor failure. It must not reset the stall
			// guard — a plan whose feats all park would otherwise spin forever — and
			// it must not trip it either, since the feat has not failed at anything.
		case iterFailed:
			sum.Failures++
			stall++
		case iterSpawnAborted:
			// Not a feat failure and not a stall: `claude` itself will not start, so
			// every remaining feat would fail identically. End loudly (R1.4).
			stop(OutcomeSpawnFailed, fmt.Sprintf("could not start the claude session %d times in a row — the environment is broken, not the plan; "+
				"check that `claude` runs from this shell, then `csdd plan run %s` resumes from here", maxSpawnRetries, opts.Slug))
			return
		}
		// R10.5: an unfinished feat that has spent its whole attempt budget is
		// surfaced, not retried forever. This is checked outside the stall guard on
		// purpose — the guard only counts FAILURES, and the loop a rejected `done`
		// creates is made of `continue`s, which reset it.
		if res != iterAdvanced && st.attempts[feat.Slug] >= opts.FeatAttempts {
			st.blocked[feat.Slug] = true
			sum.Blocked = append(sum.Blocked, feat.Slug)
			reason := fmt.Sprintf("blocked after %d attempts without delivering", opts.FeatAttempts)
			journal(opts, feat.Slug, "blocked", reason)
			logf("  ⛔ %s %s — moving on; it needs a human or a revised plan", feat.Slug, reason)
		}
		if stall >= opts.Stall {
			stop(OutcomeStalled, fmt.Sprintf("stalled: %d consecutive sessions failed without progress — the failure log says where", stall))
		}
	}

	for {
		// Fill the squad: dispatch every feat that may start right now, up to the
		// limit. With SquadLimit 1 this admits exactly one feat per turn and the loop
		// below waits for it, which is the serial run unchanged.
		for !stopping && len(inflight) < opts.SquadLimit {
			if iter >= opts.MaxIterations {
				if len(inflight) == 0 {
					stop(OutcomeCapped, fmt.Sprintf("reached the iteration cap (%d) — `csdd plan run %s` resumes from here", opts.MaxIterations, opts.Slug))
				}
				break
			}
			// The done set drives readiness; `unavailable` is what must not be handed
			// out — only the feats that exhausted their attempt bound, since re-handing
			// one would burn the remaining iterations on a feat that has already proven
			// it cannot converge. The feats in flight are excluded by admitFeat itself.
			//
			// A PARKED feat is deliberately NOT excluded here. It is held back by the
			// dependency it discovered, which lives in extraDeps and gates readiness
			// through depsSatisfied — so it comes back on its own the moment that peer
			// lands. Listing it as unavailable as well made the exclusion permanent:
			// nothing ever cleared it, so a parked feat was never re-dispatched and the
			// run went on to report itself complete without it.
			done := ledger.doneSet()
			feat, ok := admitFeat(doc, done, st.blocked, inflight, st.extraDeps)
			if !ok {
				// Nothing can start. With sessions still in flight that is ordinary —
				// what they deliver may open the next wave — so only an empty squad
				// ends the run. Then it is completion if nothing is left; otherwise
				// what remains is stranded behind something that will not finish, and
				// the run must name the root cause rather than report a quiet exit.
				if len(inflight) > 0 {
					break
				}
				strand := stranded(doc, done, st.blocked, st.extraDeps)
				// Completion is asserted against the LEDGER, not inferred from the
				// scheduler having nothing to offer. Those two agreeing is the normal
				// case; when they disagree the scheduler is wrong, and "plan complete"
				// over an undelivered feat is the one report that must never be
				// possible — it is what the parked bug produced.
				left := undelivered(doc, done)
				sum.Completed = len(sum.Blocked) == 0 && len(strand) == 0 && len(left) == 0
				switch {
				case sum.Completed:
					stop(OutcomeComplete, "plan complete: every feat is delivered")
				case len(sum.Blocked) > 0:
					stop(OutcomeBlocked, fmt.Sprintf("stopped with %d feat(s) blocked after %d attempts each: %s%s",
						len(sum.Blocked), opts.FeatAttempts, strings.Join(sum.Blocked, ", "), strandedSuffix(strand)))
				case len(strand) > 0:
					stop(OutcomeBlocked, "stopped: nothing is workable"+strandedSuffix(strand))
				default:
					// Nothing is blocked, nothing is stranded, and yet feats remain
					// undelivered: the scheduler declined to offer a workable feat.
					// Name it as the defect it is rather than dressing it as an outcome.
					stop(OutcomeBlocked, fmt.Sprintf("stopped: %d feat(s) are neither delivered, blocked nor stranded, "+
						"and the scheduler offered none of them — this is a scheduling defect, please report it: %s",
						len(left), strings.Join(left, ", ")))
				}
				break
			}

			iter++
			logf("→ %s (session %d/%d)", feat.Slug, iter, opts.MaxIterations)
			d := openDispatch(opts, doc, feat, st, iter)
			if d == nil {
				// The session never opened (the brief would not assemble), so there is
				// nothing to wait for: settle it here and let the loop try again.
				note(feat, iterFailed)
				continue
			}
			inflight[feat.Slug] = feat
			go func(d *dispatch) {
				d.outcome, d.err = sessionOrWait(opts, d.request(opts.SessionBudget), st)
				results <- d
			}(d)
		}

		if len(inflight) == 0 {
			break
		}
		d := <-results
		delete(inflight, d.feat.Slug)
		note(d.feat, settleDispatch(opts, doc, d, ledger, st, &sum))
	}

	return summarize(out, sum), nil
}

// iterResult is what one iteration (one session) produced.
type iterResult int

const (
	iterFailed       iterResult = iota // context for the next iteration
	iterAdvanced                       // the feat was delivered (ledger recorded it)
	iterContinue                       // honest partial work, handoff recorded
	iterParked                         // waiting on a peer feat; no attempt spent (R6.3)
	iterSpawnAborted                   // `claude` would not start; the run must end (R1.4)
)

// dispatch is one feat handed to one session: everything the runner decided before
// spawning, and what the session came back with. It exists because the two halves
// no longer run on the same goroutine — the session runs concurrently with its
// peers, while opening and settling it stay on the loop's own goroutine, where the
// ledger and the run state live.
type dispatch struct {
	feat    Feat
	iter    int    // the run's session counter, assigned at dispatch
	attempt int    // this feat's attempt number (R10.4)
	brief   string // the mission brief plus the rolling context, frozen at dispatch
	dir     string // the feat's worktree: where the session runs and what the gate reads

	outcome SessionOutcome // written by the session goroutine
	err     error          // written by the session goroutine
}

// request is what the Session hook is handed. Assembled from the dispatch so the
// worktree the runner prepared and the directory the session runs in cannot drift
// apart.
func (d *dispatch) request(budgetUSD float64) SessionRequest {
	return SessionRequest{Feat: d.feat, Brief: d.brief, BudgetUSD: budgetUSD, Dir: d.dir}
}

// openDispatch prepares one session for a feat and charges the attempt: it
// assembles the brief with the rolling context its predecessors left, and opens the
// attempt on disk BEFORE the session is spawned. If the host dies mid-session that
// `started` row is the only evidence the attempt ever happened, and restoreRunState
// counts it (R2.1, R2.2).
//
// It returns nil when the brief will not assemble, since no session can be spawned
// from it. That failure costs no attempt — assembling the brief is the runner's job,
// not the feat's — and the caller counts it as an ordinary iterFailed.
func openDispatch(opts RunOptions, doc *PlanDoc, feat Feat, st *runState, iter int) *dispatch {
	logf := runLogf(opts)
	key := feat.Slug

	// The worktree is prepared before the attempt is charged: a feat that could not
	// be given a tree to work in has not been tried, and spending one of its bounded
	// attempts on the runner's own failure would surface it as unable to converge
	// (R1.2, R10.4). It also comes first because both steps below read it — the
	// enricher describes the tree the session will actually work in, which for a feat
	// with dependencies is only correct after they have been merged into it.
	dir, err := opts.Hooks.Trees.Ensure(feat.Slug)
	if err != nil {
		st.hist(key).add("worktree setup failed", err.Error())
		journal(opts, feat.Slug, "failed", "worktree setup: "+firstLine(err.Error()))
		logf("  ✗ could not prepare an isolated worktree for %s: %v", feat.Slug, err)
		return nil
	}
	// Discovery, once per feat rather than once per attempt: EnsurePack reuses the
	// stored pack whenever the feat row that produced it has not changed. It cannot
	// fail the dispatch — enrichment is an optimization over the deterministic brief,
	// so every failure inside it is logged and the brief renders without it.
	EnsurePack(opts.Root, doc, feat, dir, opts.Hooks.Enrich, logf)

	base, err := FeatBrief(opts.Root, doc, feat)
	if err != nil {
		st.hist(key).add("brief assembly failed", err.Error())
		journal(opts, feat.Slug, "failed", "brief assembly: "+firstLine(err.Error()))
		logf("  ✗ brief error: %v", err)
		return nil
	}

	d := &dispatch{feat: feat, iter: iter, attempt: st.nextAttempt(key), brief: base + runContext(st, key), dir: dir}
	recordSession(opts, d, SessionStarted, "", false)
	return d
}

// recordSession persists one row of a dispatch's attempt with whatever the session
// has cost so far (R9.2). Instrumentation must never end a run, so a write failure
// is reported and swallowed.
func recordSession(opts RunOptions, d *dispatch, status, detail string, gated bool) {
	rec := newSessionRecord(d.feat.Slug, d.iter, d.attempt, status, detail, d.outcome.Metrics, opts.Hooks.Now())
	rec.Gated = gated
	if err := AppendSessionRecord(opts.Root, opts.Slug, rec); err != nil {
		runLogf(opts)("  ⚠ could not record this session's metrics: %v", err)
	}
}

// settleDispatch records what a finished session declared. The loop still trusts the
// session's JUDGMENT — it does not review the code, run the suite, or second-guess
// the reasoning — but it no longer trusts unchecked CLAIMS about files it can read
// in milliseconds: a `done` verdict passes through the verdict gate first (R10,
// §5.5). A gate rejection becomes a `continue` carrying a handoff that names each
// failed check, so the next session self-heals. A `continue` stores its handoff for
// the successor; a session error lands on the feat's rolling history, which the next
// iteration's brief carries.
//
// It runs on the run loop's own goroutine, never a session's, so the ledger, the
// run state and the journal are still written by exactly one writer no matter how
// many sessions were in flight.
//
// Every path records the attempt with its cost (R9.2) before returning.
func settleDispatch(opts RunOptions, doc *PlanDoc, d *dispatch, ledger *Ledger, st *runState, sum *RunSummary) iterResult {
	h := opts.Hooks
	logf := runLogf(opts)
	feat := d.feat
	key := feat.Slug
	hist := st.hist(key)
	attempt := d.attempt
	outcome, err := d.outcome, d.err
	record := func(status, detail string, gated bool) { recordSession(opts, d, status, detail, gated) }

	// A spawn failure that survived every retry is the environment, not the feat.
	// Give the attempt back (the session never ran), settle the `started` row so a
	// later resume does not read it as a crash, and end the run — grinding the rest
	// of the plan against a `claude` that will not start only manufactures blocked
	// feats, which is exactly what happened in the `violet` run (R1.2, R1.4).
	var spawn *SpawnError
	if errors.As(err, &spawn) {
		st.undoAttempt(key)
		record(SessionInfra, firstLine(err.Error()), false)
		journal(opts, feat.Slug, "infra",
			fmt.Sprintf("spawn failed %d× in a row: %s", maxSpawnRetries, firstLine(spawn.Err.Error())))
		logf("  ✗ could not start the claude session after %d attempts: %v", maxSpawnRetries, spawn.Err)
		return iterSpawnAborted
	}

	sum.Sessions++
	if err != nil {
		record(SessionFailed, firstLine(err.Error()), false)
		hist.add("session error", err.Error())
		st.logs[key] = writeFailureLog(opts, feat, hist)
		journal(opts, feat.Slug, "failed", "session error: "+firstLine(err.Error()))
		logf("  ✗ session error: %v", err)
		return iterFailed
	}

	verdict := outcome.Verdict
	switch verdict.Status {
	case VerdictBlocked:
		// The session says it needs a peer the plan never declared. Believe it only
		// if the claim survives the same on-disk check a `done` gets (R6.2): the named
		// feats must exist and must not already be delivered. A refused claim is
		// demoted to ordinary partial work, so a session cannot dodge a hard feat by
		// naming a blocker that isn't one.
		deps, refusal := gateBlocked(doc, ledger.doneSet(), feat, verdict)
		if refusal != "" {
			record(SessionContinue, "blocked verdict refused: "+refusal, true)
			st.handoffs[key] = blockedRefusalHandoff(feat, refusal, verdict.Summary)
			journal(opts, feat.Slug, "progress", "blocked verdict refused: "+refusal)
			logf("  ⚠ %s declared itself blocked but the claim did not hold (%s) — handing back as partial work", feat.Slug, refusal)
			return iterContinue
		}
		// Park it and remember the edge. Parking costs no attempt: the feat is
		// waiting, not failing, and charging it would spend the budget of a feat that
		// has not been allowed to try.
		st.undoAttempt(key)
		merged, err := RecordDiscoveredDeps(opts.Root, opts.Slug, feat.Slug, deps)
		st.extraDeps = merged
		if err != nil {
			logf("  ⚠ could not persist the discovered dependency: %v", err)
		}
		record(SessionBlocked, strings.Join(deps, ", "), false)
		st.handoffs[key] = strings.TrimSpace(verdict.Summary)
		journal(opts, feat.Slug, "blocked-on", strings.Join(deps, ", "))
		logf("  ⏸ %s is waiting on %s — parked until it lands (no attempt spent)", feat.Slug, strings.Join(deps, ", "))
		return iterParked
	case VerdictContinue:
		record(SessionContinue, oneLine(verdict.Summary), false)
		st.handoffs[key] = strings.TrimSpace(verdict.Summary)
		journal(opts, feat.Slug, "progress", "handoff: "+oneLine(verdict.Summary))
		logf("  … progress — handing off to the next session")
		return iterContinue
	case VerdictDone:
		// R10.1: the three on-disk checks, before the ledger records anything. They
		// read the feat's WORKTREE, not the workspace root: that is where the session
		// just wrote the spec, the checked tasks and the test evidence, and the root
		// will not see any of it until the merge below lands.
		if findings := gateDone(d.dir, feat); len(findings) > 0 {
			sum.Gated++
			names := gateCheckNames(findings)
			record(SessionContinue, "verdict gate refused `done`: "+names, true)
			st.handoffs[key] = gateHandoff(feat, findings, verdict.Summary)
			journal(opts, feat.Slug, "progress", "verdict gate refused `done`: "+names)
			logf("  ⚠ %s claimed done but the on-disk checks refused it (%s) — handing back for %d more attempt(s)",
				feat.Slug, names, opts.FeatAttempts-attempt)
			for _, f := range findings {
				logf("      ✗ %s: %s", f.check, f.detail)
			}
			return iterContinue
		}
		// The feat is finished in its own tree; now it has to land on the run's base,
		// because that is what every feat dispatched after it will be cut from. This
		// runs BEFORE the ledger records anything: a feat marked delivered on a merge
		// that did not happen is a lie the rest of the run would build on.
		if err := opts.Hooks.Trees.Integrate(feat.Slug); err != nil {
			var uncommitted *UncommittedWorkError
			if errors.As(err, &uncommitted) {
				// Finished on disk, absent from git. Handed back like a gate rejection,
				// because that is what it is: an artifact the session contracted to
				// produce — a commit — that is not there.
				sum.Gated++
				record(SessionContinue, "uncommitted work: "+oneLine(uncommitted.Error()), true)
				st.handoffs[key] = uncommittedHandoff(feat, uncommitted, verdict.Summary)
				journal(opts, feat.Slug, "progress", "uncommitted work: "+oneLine(uncommitted.Error()))
				logf("  ⚠ %s claimed done but left %d path(s) uncommitted — only committed work reaches the base",
					feat.Slug, len(uncommitted.Paths))
				return iterContinue
			}
			var conflict *MergeConflictError
			if errors.As(err, &conflict) {
				// Finished work that no longer applies to a base that moved under it.
				// Same shape as a gate rejection: hand it back naming the conflict so
				// the next session rebases, rather than failing a feat that is done.
				sum.Gated++
				record(SessionContinue, "merge conflict: "+oneLine(conflict.Error()), true)
				st.handoffs[key] = mergeConflictHandoff(feat, conflict, verdict.Summary)
				hist.add("merge conflict", conflict.Detail)
				st.logs[key] = writeFailureLog(opts, feat, hist)
				journal(opts, feat.Slug, "progress", "merge conflict: "+oneLine(conflict.Error()))
				logf("  ⚠ %s is finished but conflicts with the run base (%s) — handing back to rebase",
					feat.Slug, strings.Join(conflict.Files, ", "))
				return iterContinue
			}
			record(SessionFailed, "merge failed: "+firstLine(err.Error()), false)
			hist.add("merge failed", err.Error())
			journal(opts, feat.Slug, "failed", "merge: "+firstLine(err.Error()))
			logf("  ✗ could not land %s on the run base: %v", feat.Slug, err)
			return iterFailed
		}
		ledger.MarkDone(feat.Slug, oneLine(verdict.Summary), h.Now())
		if err := ledger.Save(opts.Root, opts.Slug); err != nil {
			record(SessionFailed, "ledger save: "+firstLine(err.Error()), false)
			hist.add("ledger save failed", err.Error())
			journal(opts, feat.Slug, "failed", "ledger save: "+firstLine(err.Error()))
			logf("  ✗ could not record %s as delivered: %v", feat.Slug, err)
			return iterFailed
		}
		// The work is on the base and in the ledger, so the tree has nothing left to
		// hold. The branch stays — it is what the PR is opened from. A tree that will
		// not go away is untidy, never wrong, so it must not fail a delivered feat.
		if err := opts.Hooks.Trees.Discard(feat.Slug); err != nil {
			logf("  ⚠ %s is delivered but its worktree could not be removed: %v", feat.Slug, err)
		}
		record(SessionDone, oneLine(verdict.Summary), false)
		journalDone(opts, feat.Slug, oneLine(verdict.Summary))
		st.clearFeat(key)
		sum.Steps++
		logf("  ✓ %s delivered", feat.Slug)
		return iterAdvanced
	default:
		// parseVerdict enforces done|continue, so this is defensive only.
		record(SessionFailed, "unknown verdict status: "+verdict.Status, false)
		hist.add("unknown verdict", verdict.Status)
		journal(opts, feat.Slug, "failed", "unknown verdict status: "+verdict.Status)
		logf("  ✗ unknown verdict status %q", verdict.Status)
		return iterFailed
	}
}

// sessionOrWait runs one session, transparently absorbing the two INFRASTRUCTURE
// stops — an account limit and a failed spawn — so neither is mistaken for the
// feat failing.
//
// An account limit sleeps until the reset window reopens and retries the SAME
// feat. A spawn failure backs off briefly and retries the same feat, up to
// maxSpawnRetries consecutive times; past that the environment is broken rather
// than flaky, and the SpawnError is returned so the run can end honestly instead
// of grinding every feat to `blocked` against a `claude` that will not start.
//
// From executeFeat's view both are invisible while they are being absorbed: they
// never count as a failure, burn the stall guard, or pollute the failure log, and
// the iteration is not consumed since the caller only advances once this returns.
// It runs on the session's own goroutine, so it touches nothing on the run state
// but the spawn counter, through the methods that guard it.
func sessionOrWait(opts RunOptions, req SessionRequest, st *runState) (SessionOutcome, error) {
	h := opts.Hooks
	feat := req.Feat
	for {
		out, err := h.Session(req)
		var lim *LimitError
		if errors.As(err, &lim) {
			st.sessionRan() // the child ran; only the account said no
			waitForLimit(opts, feat, lim)
			continue
		}
		var spawn *SpawnError
		if errors.As(err, &spawn) {
			consecutive := st.spawnFailed()
			if consecutive >= maxSpawnRetries {
				return out, err
			}
			waitForSpawn(opts, feat, spawn, consecutive)
			continue
		}
		// Any session that actually ran — success or failure — proves the exec path
		// works, so a later spawn failure starts its own count.
		st.sessionRan()
		return out, err
	}
}

// Spawn-retry tuning. A failed exec is usually environmental (a too-long command
// line, a binary mid-upgrade, an exhausted handle table), so a short backoff is
// worth trying; a persistent one is an operator problem and must surface fast.
const (
	// maxSpawnRetries bounds consecutive failed spawns before the run gives up.
	// Deliberately smaller than FeatAttempts: five identical exec failures say the
	// environment is broken, and the `violet` run showed what waiting longer costs
	// — eight and ten in a row, on two feats, both wrongly marked blocked.
	maxSpawnRetries = 5
	// spawnBackoff is the first wait between spawn retries; it doubles each time.
	spawnBackoff = 5 * time.Second
)

// waitForSpawn reports a failed spawn and backs off before the next try. The
// backoff doubles per consecutive failure, which keeps a transient fault cheap
// without spinning on a permanent one.
func waitForSpawn(opts RunOptions, feat Feat, spawn *SpawnError, consecutive int) {
	logf := runLogf(opts)
	// Clamped rather than trusted: a negative shift is a runtime panic, and this
	// would be a graceless way to lose a run if a future caller ever passed 0.
	if consecutive < 1 {
		consecutive = 1
	}
	wait := spawnBackoff << (consecutive - 1)
	logf("  ⚠ could not start the claude session (%d/%d) — retrying %s in %s: %v",
		consecutive, maxSpawnRetries, feat.Slug, wait.Round(time.Second), spawn.Err)
	journal(opts, feat.Slug, "infra", "spawn failed: "+firstLine(spawn.Err.Error()))
	opts.Hooks.Sleep(wait)
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
	var reconciled []string
	for _, f := range doc.Feats {
		if done[f.Slug] {
			continue
		}
		if deriveFeatStatus(opts.Root, f, done).State != StateDone {
			continue
		}
		ledger.MarkDone(f.Slug, "reconciled: spec fully delivered on disk before this run", opts.Hooks.Now())
		logf("  ✓ %s already delivered on disk — recorded in the ledger", f.Slug)
		reconciled = append(reconciled, f.Slug)
	}
	if len(reconciled) == 0 {
		return
	}
	// One journal entry, not one per feat. The ledger lives in the transient state
	// dir, so a plan that loses it re-reconciles EVERY delivered feat on the next
	// run: in the `violet` plan that made 40 of 94 journal entries identical
	// "reconciled from disk" lines, burying the eight handoffs and twenty failures
	// the journal exists to preserve. The feat names are the detail (R9.1).
	journal(opts, "-", "reconciled",
		fmt.Sprintf("%d feat(s) already delivered on disk: %s", len(reconciled), strings.Join(reconciled, ", ")))
	if err := ledger.Save(opts.Root, opts.Slug); err != nil {
		logf("  ⚠ could not persist the reconciled ledger: %v", err)
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
	// attempts counts every session a feat has consumed this run, whatever it
	// returned. Unlike the failure history it also counts `continue`s — including
	// the ones the verdict gate manufactured — because those are exactly the
	// attempts the bound (R10.4) exists to cap.
	attempts map[string]int
	// blocked is the set of feats that exhausted the bound. The sequencer skips
	// them so the rest of the plan can still run (R10.5).
	blocked map[string]bool
	// mu guards spawnFailures, the one field a session goroutine touches. Every
	// other field is read and written only by the run loop, which settles one
	// dispatch at a time — the squad overlaps sessions, not bookkeeping.
	mu sync.Mutex
	// spawnFailures counts CONSECUTIVE failed execs of `claude`. Any session that
	// actually ran resets it, so this measures "the exec path is broken right now",
	// not "the run has had trouble". It lives here rather than in sessionOrWait so
	// the count survives across feats — five failures spread over five feats is the
	// same broken environment as five on one — and, with a squad, across the
	// sessions running side by side.
	spawnFailures int
	// A feat that reported itself blocked on a peer is NOT tracked here. It used to
	// be, in a `parked` set the scheduler treated as unavailable — and nothing ever
	// removed it, so the feat was never offered again and the run reported itself
	// complete without it. What actually holds a parked feat back is the edge it
	// discovered: it goes into extraDeps, depsSatisfied gates on it, and the feat
	// becomes ready again by itself the moment that peer is delivered. One mechanism,
	// which clears on its own, instead of two, one of which did not.
	//
	// extraDeps are those edges: discovered at run time, unioned
	// with the plan's own by the scheduler and persisted to the sidecar so a later
	// run does not have to rediscover them.
	extraDeps map[string][]string
	// crashed names the feats with an attempt recovered from disk that opened but
	// never settled — a host that died mid-session. Feat slugs rather than a bare
	// count because the journal entry has to be actionable: "something crashed" is
	// not a thing anyone can follow up on. Sorted and deduplicated (R2.2, R3.5).
	crashed []string
}

func newRunState() *runState {
	return &runState{
		hists:     map[string]*failureHistory{},
		handoffs:  map[string]string{},
		logs:      map[string]string{},
		attempts:  map[string]int{},
		blocked:   map[string]bool{},
		extraDeps: map[string][]string{},
	}
}

// strandedSuffix renders the stranded map as an explanatory tail for the run's
// closing reason, or "" when nothing is stranded. Sorted, because a run summary
// that reorders itself between identical runs is a summary nobody trusts.
func strandedSuffix(strand map[string]string) string {
	if len(strand) == 0 {
		return ""
	}
	feats := make([]string, 0, len(strand))
	for f := range strand {
		feats = append(feats, f)
	}
	sort.Strings(feats)
	parts := make([]string, 0, len(feats))
	for _, f := range feats {
		parts = append(parts, f+" (waiting on "+strand[f]+")")
	}
	return "; unreachable: " + strings.Join(parts, ", ")
}

func (s *runState) hist(key string) *failureHistory {
	if s.hists[key] == nil {
		s.hists[key] = &failureHistory{}
	}
	return s.hists[key]
}

// nextAttempt counts one more session against a feat and returns its number,
// 1-based.
func (s *runState) nextAttempt(key string) int {
	s.attempts[key]++
	return s.attempts[key]
}

// undoAttempt gives back an attempt that was opened but never spent, because the
// session never started. Without it a broken exec path would spend a feat's whole
// attempt budget without the feat ever being tried once (R1.2).
func (s *runState) undoAttempt(key string) {
	if s.attempts[key] > 0 {
		s.attempts[key]--
	}
}

// spawnFailed counts one more consecutive failed exec of `claude` and returns the
// new total. Guarded: with a squad in flight, several sessions can fail to spawn at
// the same instant, and the whole point of the counter is that those failures are
// one environment problem rather than one per feat.
func (s *runState) spawnFailed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawnFailures++
	return s.spawnFailures
}

// sessionRan resets the spawn counter: a child that actually started proves the exec
// path works, whatever it then went on to do.
func (s *runState) sessionRan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawnFailures = 0
}

// clearFeat forgets a feat's trail once it is delivered.
func (s *runState) clearFeat(key string) {
	delete(s.hists, key)
	delete(s.handoffs, key)
	delete(s.logs, key)
	delete(s.attempts, key)
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
	rel := failureLogRel(opts.Slug, feat.Slug)
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
	return rel
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

// resolveTrees picks where the run's feats work, and the squad size is what decides
// it.
//
// A worktree per feat exists to keep concurrent sessions off one shared index. That
// is a real hazard with a squad and no hazard at all with one session — while the
// cost is the same either way, and it is not small: a worktree holds only tracked
// files, so a serial run was reinstalling every gitignored dependency the suite needs
// before it could run a single gate, per feat and again per attempt. So a serial run
// (the default) works in the repository itself, and only a squad pays for isolation.
//
// The git preflight moves with the worktrees, deliberately. It resolves the base
// feats are cut from and merged back into, and refuses a repository where that would
// be ambiguous — questions an in-place run does not ask, because it lands nothing:
// the session commits on the branch the human left it on.
func resolveTrees(opts RunOptions, logf func(string, ...any)) (treeKeeper, error) {
	if opts.SquadLimit <= 1 {
		logf("worktrees: off — one session at a time, working in %s itself", opts.Root)
		logf("  (--squad-limit >1 isolates each feat in its own worktree, cut from and merged back into the base)")
		return inPlaceTrees{root: opts.Root}, nil
	}
	base, err := preflightGit(opts.Root)
	if err != nil {
		return nil, err
	}
	logf("worktrees: one per feat, branched from and merged back into %s", base)
	if specsAreIgnored(opts.Root) {
		logf("⚠ specs/ is gitignored: each feat's spec will stay in the worktree that authored it and be")
		logf("  discarded with it. The code lands on %s; the artifacts justifying it will not, so", base)
		logf("  `csdd plan status` and the graph will disagree with the ledger after this run.")
	}
	return gitTrees{root: opts.Root, slug: opts.Slug, base: base, logf: logf, entry: opts.WorktreeEntry}, nil
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
		opts.MaxIterations = 30
	}
	if opts.Stall <= 0 {
		opts.Stall = 10
	}
	if opts.FeatAttempts <= 0 {
		opts.FeatAttempts = 4
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.SessionIdle <= 0 {
		opts.SessionIdle = defaultSessionIdle
	}
	if opts.SquadLimit <= 0 {
		opts.SquadLimit = 1
	}
	// A squad puts several sessions on one stream, which changes what that stream
	// can be. The in-place live view assumes it is the only writer — two sessions
	// redrawing over each other paint garbage — so a squad gets the append-only
	// renderer even in a terminal, where every line already carries its feat tag.
	// Whole writes are serialized for the same reason: a line from one session must
	// never land inside another's.
	tty := isTerminal(opts.Out) && opts.SquadLimit == 1
	if opts.SquadLimit > 1 {
		opts.Out = &syncWriter{w: opts.Out}
	}
	// Hooks.Now may still be nil here; the reporter resolves that itself, and
	// installRealHooks fills the hook right after.
	installRealHooks(&opts.Hooks, opts.Model, opts.Effort, sessionEnv{
		idle: opts.SessionIdle,
		out:  opts.Out,
		tty:  tty,
		now:  opts.Hooks.Now,
	})
	installEnrichHook(&opts.Hooks, opts.EnrichModel)
}

// syncWriter serializes writes to one destination so concurrent sessions cannot
// interleave inside a single line. It wraps opts.Out only for a squad run: a serial
// run has exactly one writer and needs no lock, and the wrapper would also hide the
// *os.File the live view checks for.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func summarize(out io.Writer, sum RunSummary) RunSummary {
	_, _ = fmt.Fprintf(out, "totals: %d sessions, %d feats delivered, %d failed iterations, %d gated verdicts\n",
		sum.Sessions, sum.Steps, sum.Failures, sum.Gated)
	if len(sum.Blocked) > 0 {
		_, _ = fmt.Fprintf(out, "blocked: %s\n", strings.Join(sum.Blocked, ", "))
	}
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
