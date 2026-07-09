package plan

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/textutil"
	"github.com/protonspy/csdd/internal/validator"
)

// Hooks are the runner's seams onto the outside world. Every field has a real
// default (installed by Run); tests inject stubs so the whole loop — verdict
// parsing, gate pipeline, rotation, journal — runs with no `claude`, `csdd`, or
// subprocess at all. The runner never touches git: branch, commit, and the PR
// are the csdd dev-cycle's job, driven inside the session.
type Hooks struct {
	// Session runs one claude session for a step and returns its parsed verdict.
	// Every session runs bypass-mode (--dangerously-skip-permissions).
	Session func(step Step, brief string, budgetUSD float64) (Verdict, error)
	// CSDD runs `csdd <args...>` in root, returning success and combined output.
	CSDD func(root string, args ...string) (bool, string)
	// Gate runs a shell gate command in root, returning success and output.
	Gate func(root, command string) (bool, string)
	// Doctor proves sandbox isolation before the bypass-mode loop starts.
	Doctor func() SandboxReport
	// Confirm asks the human a yes/no question (the unverified-sandbox alert)
	// and reports whether they accepted. The real hook reads stdin, so a
	// non-interactive run (EOF) declines by default.
	Confirm func(prompt string) bool
	// ClaudeAvailable reports whether the `claude` CLI is on PATH.
	ClaudeAvailable func() bool
	// Now returns the current time (journal timestamps; injected for determinism).
	Now func() time.Time
}

// RunOptions configures a `csdd plan run` invocation. The loop is deliberately
// dumb: one iteration is exactly one claude session, and the only ways it ends
// are the plan completing, the session declaring a halt, the stall guard, or
// the iteration cap. Claude — driving csdd inside the session — owns the flow;
// the runner owns verification and bookkeeping.
type RunOptions struct {
	Root          string
	Slug          string
	AssumeYes     bool    // accept the unverified-sandbox alert without prompting (--yes)
	SessionBudget float64 // per-session --max-budget-usd; 0 = no cap (Claude account limits)
	MaxIterations int     // sessions the run may spend; default 100
	// Stall ends the run after this many consecutive iterations in which no step
	// advanced (default 10). The iteration cap protects the wallet; the stall
	// guard protects against burning it on a failure the loop is not converging
	// on — self-correction gets a real window, not an unbounded one.
	Stall int
	Out   io.Writer
	Hooks Hooks
}

// Run outcomes, chosen so the CLI can surface them as distinct exit codes.
const (
	OutcomeComplete = 0 // every feat is done
	OutcomeNothing  = 4 // nothing unblocked (mirrors the sequencer)
	OutcomeDrift    = 5 // plan.md or seeds/ changed mid-run
	OutcomeCapped   = 6 // hit the iteration cap without finishing
	OutcomeHalted   = 7 // a session declared an impediment outside the workspace
	OutcomeStalled  = 8 // the stall guard tripped: no advancement for Stall iterations
)

// RunSummary is the totals reported on exit (R9.7).
type RunSummary struct {
	Sessions  int // claude sessions spent (= iterations that reached a session)
	Steps     int // steps that advanced (gates green, approval performed)
	Decisions int // open decisions sessions resolved and recorded
	Failures  int // iterations that ended as failure context for the next one
	Completed bool
	Reason    string
	Outcome   int
}

// Run drives the autonomous plan loop (R9): preflight, then one session per
// iteration — brief + rolling context → session → runner-owned gates →
// advance/feed-back — until the plan completes. Failures never park a feat:
// they become the next iteration's context, and the loop self-corrects inside
// the iteration budget. A mid-run change to the Decided stack rows is a
// recorded decision and re-binds the approval; a change to plan.md or seeds/
// is real drift and stops autonomy.
func Run(opts RunOptions) (RunSummary, error) {
	fillRunDefaults(&opts)
	h := opts.Hooks
	out := opts.Out
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }

	doc, err := Load(opts.Root, opts.Slug)
	if err != nil {
		return RunSummary{}, err
	}
	st := newRunState()

	// Preflight (R9.1): approval + drift, claude present, sandbox.
	approved, drift, err := IsApproved(opts.Root, opts.Slug)
	if err != nil {
		return RunSummary{}, err
	}
	if !approved {
		return RunSummary{}, fmt.Errorf("plan %q is not approved; run `csdd plan approve %s` first", opts.Slug, opts.Slug)
	}
	if drift {
		// A decision recorded between runs (new Decided rows, same plan.md) is not
		// the human's drift to answer — fold it in and go. Anything else still is.
		rebound, note, rerr := rebindContract(opts, doc, "")
		if rerr != nil {
			return RunSummary{}, rerr
		}
		if !rebound {
			return RunSummary{}, fmt.Errorf("plan %q has drifted since approval; re-approve before running", opts.Slug)
		}
		st.contractNote = note
		logf("contract re-bound: recorded stack decisions folded into the approval")
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
	logf("plan run %s (bypass mode, %s, max %d sessions, stall guard %d)",
		opts.Slug, budgetLabel(opts.SessionBudget), opts.MaxIterations, opts.Stall)

	// The core hash is the trusted mid-run reference when plan.json predates
	// CoreHash. Computed after the preflight settled that nothing has drifted.
	startCore, err := HashPlanCore(opts.Root, opts.Slug)
	if err != nil {
		return RunSummary{}, err
	}

	// A marker is only ever the record of how a previous run ended; this run
	// retries everything, seeded with the failure logs those markers point at.
	clearAllBlocks(opts, st, logf)

	var sum RunSummary
	stall := 0
	lastFailedFeat := ""
	var lastStep Step
	haveLast := false

	for iter := 1; iter <= opts.MaxIterations; iter++ {
		// Contract reconciliation, every iteration (R9.5 rethought): a recorded
		// decision re-binds and continues; an edited plan still stops autonomy.
		if stop, reason, err := reconcileContract(opts, doc, startCore, st, logf); err != nil {
			return sum, err
		} else if stop {
			sum.Reason = reason
			sum.Outcome = OutcomeDrift
			logf(sum.Reason)
			return summarize(out, sum), nil
		}

		step, outcome, err := nextWithRotation(opts, doc, lastFailedFeat)
		if err != nil {
			return sum, err
		}
		switch outcome {
		case SeqComplete:
			sum.Completed = true
			sum.Outcome = OutcomeComplete
			sum.Reason = "plan complete: every feat is done"
			logf(sum.Reason)
			return summarize(out, sum), nil
		case SeqNothing:
			// Defensive: mid-run nothing is ever marker-blocked, so this means the
			// dependency graph itself has nothing actionable (e.g. an unknown dep).
			sum.Outcome = OutcomeNothing
			sum.Reason = "nothing actionable: remaining feats wait on dependencies that cannot complete"
			logf(sum.Reason)
			reportBlocks(opts, logf)
			return summarize(out, sum), nil
		case SeqNotReady:
			sum.Outcome = OutcomeDrift
			sum.Reason = "plan not approved or drifted"
			logf(sum.Reason)
			return summarize(out, sum), nil
		}

		logf("→ %s / %s (session %d/%d)", step.Feat, step.Step, iter, opts.MaxIterations)
		lastStep, haveLast = step, true
		switch executeStep(opts, doc, step, st, &sum) {
		case iterAdvanced:
			stall = 0
			lastFailedFeat = ""
		case iterProgress:
			stall++
			lastFailedFeat = "" // honest partial work: keep the feat in front
		case iterFailed:
			sum.Failures++
			stall++
			lastFailedFeat = step.Feat // rotate: siblings advance while this cools off
		case iterHalted:
			sum.Outcome = OutcomeHalted
			sum.Reason = "halted by the session: " + orDash(st.haltSummary)
			logf(sum.Reason)
			return summarize(out, sum), nil
		}
		if stall >= opts.Stall {
			reason := fmt.Sprintf("stalled: %d consecutive sessions without a step advancing", stall)
			parkRun(opts, step, st, reason)
			sum.Outcome = OutcomeStalled
			sum.Reason = reason + " — the marker and failure log say where it was stuck"
			logf(sum.Reason)
			return summarize(out, sum), nil
		}
	}

	// The cap is the wallet guard, not a verdict on the plan: park the in-flight
	// step (if one was failing) so `plan status` explains, and let the next
	// `plan run` pick everything back up.
	if haveLast {
		if hist := st.hists[stepKey(lastStep)]; hist != nil && hist.len() > 0 {
			parkRun(opts, lastStep, st, fmt.Sprintf("iteration cap (%d) hit while this step was failing", opts.MaxIterations))
		}
	}
	sum.Outcome = OutcomeCapped
	sum.Reason = fmt.Sprintf("reached the iteration cap (%d) — `csdd plan run %s` resumes from here", opts.MaxIterations, opts.Slug)
	logf(sum.Reason)
	return summarize(out, sum), nil
}

// nextWithRotation is Next plus the in-memory failure rotation: the feat whose
// step just failed steps aside for one selection so its siblings keep moving,
// and comes straight back when nothing else is eligible. Without this the
// sequencer would re-pick the same failing feat forever and starve the rest —
// the disk-marker version of "skip" died with the parking runner.
func nextWithRotation(opts RunOptions, doc *PlanDoc, lastFailedFeat string) (Step, int, error) {
	if lastFailedFeat != "" {
		step, outcome, err := Next(opts.Root, doc, true, lastFailedFeat)
		if err != nil || outcome != SeqNothing {
			return step, outcome, err
		}
	}
	return Next(opts.Root, doc, true)
}

// iterResult is what one iteration (one session) produced.
type iterResult int

const (
	iterFailed   iterResult = iota // context for the next iteration
	iterAdvanced                   // gates green, approval performed
	iterProgress                   // honest partial work, handoff recorded
	iterHalted                     // the session declared an external impediment
)

// executeStep runs exactly one session for the step and, when the session claims
// done, the runner-owned verification: gates, then the approval a session must
// never perform. Nothing here parks a feat — every failure lands on the step's
// rolling history, which the next iteration's brief carries. The retry IS the
// next iteration; self-correction is the session reading its own trail.
func executeStep(opts RunOptions, doc *PlanDoc, step Step, st *runState, sum *RunSummary) iterResult {
	h := opts.Hooks
	logf := runLogf(opts)
	key := stepKey(step)
	hist := st.hist(key)

	baseBrief, err := Brief(opts.Root, doc, step)
	if err != nil {
		hist.add("brief assembly failed", err.Error())
		journal(opts, step, "failed", "brief assembly: "+firstLine(err.Error()))
		logf("  ✗ brief error: %v", err)
		return iterFailed
	}

	// Scaffold a spec before its requirements step (R8; runner-owned). A scaffold
	// failure is not a block: it lands on the history, and the session — which
	// has the csdd CLI — inherits the job of clearing whatever broke it.
	if step.Step == StepSpecRequirements {
		specDir := filepath.Join(opts.Root, "specs", step.Feat)
		if _, statErr := os.Stat(specDir); statErr != nil {
			if ok, o := h.CSDD(opts.Root, "plan", "generate", opts.Slug, step.Feat, "--require-approved"); !ok {
				hist.add("scaffold (plan generate) failed — run `csdd plan generate` yourself or fix what broke it", o)
				logf("  scaffold failed; handing it to the session")
			}
		}
	}

	sum.Sessions++
	verdict, err := h.Session(step, baseBrief+runContext(st, key), opts.SessionBudget)
	if err != nil {
		hist.add("session error", err.Error())
		st.logs[key] = writeFailureLog(opts, step, hist)
		journal(opts, step, "failed", "session error: "+firstLine(err.Error()))
		logf("  ✗ session error: %v", err)
		return iterFailed
	}

	// Decisions are journaled no matter how the iteration ends: the audit trail
	// of autonomy is the whole deal (the human ratifies by reading log.md).
	for _, d := range verdict.Decisions {
		journal(opts, step, "decision", oneLine(d))
		st.decisions = append(st.decisions, oneLine(d))
		sum.Decisions++
		logf("  ⚖ decision recorded: %s", oneLine(d))
	}

	switch verdict.Status {
	case VerdictHalt:
		st.haltSummary = oneLine(verdict.Summary)
		journal(opts, step, "halted", "reason: "+orDash(st.haltSummary))
		_ = WriteBlock(opts.Root, opts.Slug, Block{
			Feat: step.Feat, Step: step.Step, Kind: BlockHalt,
			Reason:    "session halted the run: " + orDash(st.haltSummary),
			BlockedAt: h.Now().UTC().Format(time.RFC3339),
		})
		logf("  ⛔ halted by the session: %s", firstLine(verdict.Summary))
		return iterHalted
	case VerdictBlocked:
		// Legacy verdict from an older brief. In this loop a decision is the
		// session's to make and record, so coach the successor instead of parking.
		detail := oneLine(verdict.Summary)
		if verdict.Revision != "" {
			detail += " | proposal: " + oneLine(verdict.Revision)
		}
		hist.add("legacy `blocked` verdict", detail+
			"\nOpen decisions are the session's to MAKE and RECORD (docs/stack.md Decided row / ADR, declared in the verdict's `decisions`) — decide it and continue; do not stop on it.")
		journal(opts, step, "failed", "legacy blocked verdict: "+oneLine(verdict.Summary))
		logf("  ✗ legacy 'blocked' verdict — the next session is told to decide and record")
		return iterFailed
	case VerdictProgress:
		st.handoffs[key] = strings.TrimSpace(verdict.Summary)
		journal(opts, step, "progress", "handoff: "+oneLine(verdict.Summary))
		logf("  … progress — handing off to the next session")
		return iterProgress
	}

	// done: the runner verifies, then performs the approval a session never does.
	// Persisting the step (branch, commit, PR) stays the session's dev-cycle job.
	if !taskClaimMaterialized(opts, step) {
		hist.add("done claim not materialized",
			"the verdict says done but the box for "+step.Step+" in specs/"+step.Feat+"/tasks.md is unchecked — checking it is part of the step's contract")
		st.logs[key] = writeFailureLog(opts, step, hist)
		journal(opts, step, "failed", "done claim not materialized (task box unchecked)")
		logf("  ✗ done claimed but the task box is unchecked")
		return iterFailed
	}
	if ok, output := runGates(opts, step); !ok {
		// The session checked the task box on a claim the gates refuted; retract it
		// so the sequencer re-selects this task instead of walking past it.
		retractTaskClaim(opts, step)
		hist.add("gate failed", output)
		st.logs[key] = writeFailureLog(opts, step, hist)
		journal(opts, step, "failed", "gates refuted the done claim (attempt "+itoa(hist.len())+")", "log: "+st.logs[key])
		logf("  ✗ gates failed (attempt %d) — the output feeds the next session", hist.len())
		return iterFailed
	}
	if err := advance(opts, step); err != nil {
		hist.add("advance failed", err.Error())
		st.logs[key] = writeFailureLog(opts, step, hist)
		journal(opts, step, "failed", "advance: "+firstLine(err.Error()))
		logf("  ✗ advance failed: %v", err)
		return iterFailed
	}
	journal(opts, step, "done")
	st.clearStep(key)
	sum.Steps++
	logf("  ✓ %s", step.Step)
	return iterAdvanced
}

// taskClaimMaterialized reports whether a task step's box is actually checked on
// disk. A done verdict whose box is still unchecked is a claim that never
// happened: without this check the runner would journal an advance while the
// sequencer re-selects the same task forever — a green-looking infinite loop the
// stall guard cannot see, because nothing ever fails. Non-task steps have their
// materialization checked by their own gates (spec validate reads the artifact).
func taskClaimMaterialized(opts RunOptions, step Step) bool {
	if !strings.HasPrefix(step.Step, StepTaskPrefix) {
		return true
	}
	id := strings.TrimSpace(strings.TrimPrefix(step.Step, StepTaskPrefix))
	data, err := os.ReadFile(filepath.Join(paths.Specs(opts.Root), step.Feat, "tasks.md"))
	if err != nil {
		return false
	}
	for _, r := range parseTaskRows(string(data)) {
		if r.id == id {
			return r.done
		}
	}
	return false
}

// runGates runs the gates for a step: the spec validator for a spec phase, or the
// plan's Quality Gates plus graph traceability for an implementation task (R9.2).
func runGates(opts RunOptions, step Step) (bool, string) {
	h := opts.Hooks
	if _, ok := specPhaseOf(step.Step); ok {
		return h.CSDD(opts.Root, "spec", "validate", step.Feat)
	}
	// Implementation task: plan Quality Gates first, then graph analyze.
	doc, err := Load(opts.Root, opts.Slug)
	if err != nil {
		return false, err.Error()
	}
	for _, g := range doc.Gates {
		if ok, o := h.Gate(opts.Root, g.Command); !ok {
			return false, fmt.Sprintf("[%s] %s\n%s", g.Label, g.Command, o)
		}
	}
	if ok, o := h.CSDD(opts.Root, "graph", "analyze", "--strict"); !ok {
		return false, "graph analyze --strict:\n" + o
	}
	return true, ""
}

// advance performs the runner-owned green-path action a session must never do:
// approving the spec phase, standing in for the human gate (R9.2). Persisting the
// step is not the runner's concern — the session's csdd dev-cycle commits.
func advance(opts RunOptions, step Step) error {
	phase, ok := specPhaseOf(step.Step)
	if !ok {
		return nil // implementation task: nothing to approve
	}
	if approved, o := opts.Hooks.CSDD(opts.Root, "spec", "approve", step.Feat, "--phase", phase); !approved {
		return fmt.Errorf("spec approve %s --phase %s: %s", step.Feat, phase, firstLine(o))
	}
	return nil
}

// specPhaseOf maps a spec-phase step to its phase name (requirements/design/
// tasks) and reports whether the step is a spec phase.
func specPhaseOf(step string) (string, bool) {
	switch step {
	case StepSpecRequirements:
		return "requirements", true
	case StepSpecDesign:
		return "design", true
	case StepSpecTasks:
		return "tasks", true
	}
	return "", false
}

// --- contract reconciliation --------------------------------------------------

// reconcileContract keeps the approval honest every iteration: no drift is a
// no-op, a stack-rows-only drift is a recorded decision (re-bind and continue),
// and anything that moved plan.md or seeds/ stops autonomy — the approved plan
// is not the loop's to rewrite.
func reconcileContract(opts RunOptions, doc *PlanDoc, startCore string, st *runState, logf func(string, ...any)) (stop bool, reason string, err error) {
	approved, drift, err := IsApproved(opts.Root, opts.Slug)
	if err != nil {
		return false, "", err
	}
	if !approved {
		return true, "stopped: the plan is no longer approved", nil
	}
	if !drift {
		return false, "", nil
	}
	rebound, note, rerr := rebindContract(opts, doc, startCore)
	if rerr != nil {
		return false, "", rerr
	}
	if !rebound {
		return true, "stopped: plan.md or seeds/ changed mid-run (re-approve to continue)", nil
	}
	st.contractNote = note
	logf("  ⚖ contract re-bound: a recorded stack decision was folded into the approval")
	return false, "", nil
}

// rebindContract re-approves the plan when — and only when — the drift is a
// recorded decision: the Decided rows moved but plan.md + seeds/** did not.
// fallbackCore is the trusted core hash for a plan.json that predates CoreHash
// (an approval written by an older csdd); empty means "no fallback: refuse".
// It returns whether it re-bound, plus a note carrying any validation findings
// the changed contract introduced — the next brief hands that note to the
// session to fix, so a corrupted stack table cannot strand the loop.
func rebindContract(opts RunOptions, doc *PlanDoc, fallbackCore string) (bool, string, error) {
	pj, ok, err := LoadPlanJSON(Dir(opts.Root, opts.Slug))
	if err != nil || !ok {
		return false, "", err
	}
	coreRef := pj.Approvals.CoreHash
	if coreRef == "" {
		coreRef = fallbackCore
	}
	if coreRef == "" {
		return false, "", nil
	}
	core, err := HashPlanCore(opts.Root, opts.Slug)
	if err != nil {
		return false, "", err
	}
	if core != coreRef {
		return false, "", nil
	}
	note := ""
	if issues := ValidatePlan(doc, opts.Root); len(issues) > 0 {
		lines := make([]string, 0, len(issues))
		for _, i := range issues {
			lines = append(lines, "- "+i.String())
		}
		note = "The re-bound contract has validation findings — fix docs/stack.md so `csdd plan validate` passes:\n" + strings.Join(lines, "\n")
	}
	if err := ApprovePlan(opts.Root, doc, opts.Hooks.Now()); err != nil {
		return false, "", err
	}
	appendJournal(Dir(opts.Root, opts.Slug), opts.Hooks.Now(), "", "-", "contract re-bound",
		"docs/stack.md Decided rows changed (a recorded decision); the approval hash was updated")
	return true, note, nil
}

// --- run state: the loop's memory across iterations ---------------------------

// runState is what one iteration hands the next. Sessions are fresh processes;
// anything not carried here (or written to disk) is lost between them.
type runState struct {
	hists        map[string]*failureHistory // step key → failed attempts
	handoffs     map[string]string          // step key → progress-verdict handoff
	logs         map[string]string          // step key → workspace-relative failure log
	decisions    []string                   // decisions recorded this run (rolling audit)
	contractNote string                     // validation findings after a re-bind, until fixed
	haltSummary  string
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

// clearStep forgets a step's trail once it advanced: the streak is over, and the
// next step of the same feat starts clean.
func (s *runState) clearStep(key string) {
	delete(s.hists, key)
	delete(s.handoffs, key)
	delete(s.logs, key)
}

// stepKey identifies one step of one feat in the state maps. NUL cannot appear
// in a feat slug or step name, so distinct pairs never collide.
func stepKey(step Step) string { return step.Feat + "\x00" + step.Step }

// maxContextAttempts bounds how many failed attempts the rolling context
// replays. The full history is always in the on-disk failure log; the brief
// carries the recent tail because a session that cannot converge after five
// replayed attempts needs a different angle, not a longer transcript.
const maxContextAttempts = 5

// runContext renders the rolling context one iteration hands the next: contract
// findings, the decisions recorded so far, the predecessor's handoff, and the
// step's failure trail. Appended after the deterministic Brief, so the contract
// part of the prompt stays byte-identical (R7.3) and only the memory varies.
func runContext(st *runState, key string) string {
	hist := st.hists[key]
	histLen := 0
	if hist != nil {
		histLen = hist.len()
	}
	handoff := st.handoffs[key]
	logRel := st.logs[key]
	if st.contractNote == "" && len(st.decisions) == 0 && handoff == "" && histLen == 0 && logRel == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Autonomous run context\n\n")
	b.WriteString("You are one iteration of a self-correcting loop; previous iterations left the\n")
	b.WriteString("state below. Diagnose the ROOT CAUSE before editing — never replay an\n")
	b.WriteString("approach that already failed. If the fix fits this step's contract, make it;\n")
	b.WriteString("if it needs a decision, make and RECORD the decision.\n")
	if st.contractNote != "" {
		b.WriteString("\n### Contract findings to fix first\n\n" + st.contractNote + "\n")
	}
	if len(st.decisions) > 0 {
		b.WriteString("\n### Decisions recorded earlier in this run\n\n")
		for _, d := range st.decisions {
			b.WriteString("- " + d + "\n")
		}
	}
	if handoff != "" {
		b.WriteString("\n### Handoff from the previous session (it reported progress)\n\n" + handoff + "\n")
	}
	if histLen > 0 {
		fmt.Fprintf(&b, "\n### This step already failed %d time(s)\n\n", histLen)
		if logRel != "" {
			fmt.Fprintf(&b, "Full untruncated output: %s\n\n", logRel)
		}
		b.WriteString(hist.renderTail(maxContextAttempts, briefFailureCap))
	} else if logRel != "" {
		fmt.Fprintf(&b, "\n### A previous run left a failure log for this step\n\nRead %s before starting.\n", logRel)
	}
	return b.String()
}

// --- failure history ----------------------------------------------------------

// briefFailureCap bounds how much of one attempt's output the context carries. A
// failing `go test` can print megabytes; the model needs the verdict, not the
// transcript, and the untruncated text is on disk in the failure log regardless.
const briefFailureCap = 4000

// attempt is one failed try at a step: which stage failed, and its full output.
type attempt struct {
	n      int
	stage  string
	output string
}

// failureHistory accumulates every failed attempt at a step across the run. The
// rolling context replays the recent tail — a root cause is usually visible
// across attempts and invisible within one.
type failureHistory struct{ attempts []attempt }

func (h *failureHistory) add(stage, output string) {
	h.attempts = append(h.attempts, attempt{n: len(h.attempts) + 1, stage: stage, output: strings.TrimRight(output, "\n")})
}

func (h *failureHistory) len() int { return len(h.attempts) }

// last renders the most recent failure one-liner-first, for markers and logs.
func (h *failureHistory) last() string {
	if len(h.attempts) == 0 {
		return ""
	}
	a := h.attempts[len(h.attempts)-1]
	return a.stage + ":\n" + a.output
}

// renderTail lays out the last n attempts, each capped at cap bytes; anything
// older stays in the on-disk failure log.
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
// and any post-run marker point at. It is rewritten (not appended) so the file
// always mirrors the history exactly. An I/O failure yields "" — a lost log must
// never take the run down with it.
func writeFailureLog(opts RunOptions, step Step, hist *failureHistory) string {
	rel := filepath.Join(paths.StateDir, "plan", opts.Slug, "failures", step.Feat, stepFileName(step.Step)+".log")
	var b strings.Builder
	fmt.Fprintf(&b, "# plan %s · feat %s · step %s\n", opts.Slug, step.Feat, step.Step)
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

// stepFileName turns a step name into a filesystem-safe basename ("task 1.2" →
// "task-1.2"), so a failure log is addressable from the marker and the journal.
// Separators become dashes and leading dots are trimmed, so no step name — however
// hostile — can resolve to a parent directory.
func stepFileName(step string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '-'
	}, step)
	if safe = strings.Trim(safe, "-."); safe == "" {
		return "step"
	}
	return safe
}

// retractTaskClaim un-checks the tasks.md box for a task step whose gates never
// went green. The box is the session's claim that the task is done; the gates
// refuted it, and a feat's state is derived from that box alone — so leaving it
// checked would let the sequencer skip a task that never passed. Only the
// named task's line is touched, and only when it is currently checked.
func retractTaskClaim(opts RunOptions, step Step) {
	if !strings.HasPrefix(step.Step, StepTaskPrefix) {
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(step.Step, StepTaskPrefix))
	if id == "" {
		return
	}
	path := filepath.Join(paths.Specs(opts.Root), step.Feat, "tasks.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Split the raw bytes (not the normalized copy) so a CRLF file keeps its line
	// endings; mask fences on the normalized copy so a fenced example is never
	// rewritten. Both have the same line count.
	lines := strings.Split(string(raw), "\n")
	masked := strings.Split(validator.MaskCodeFences(textutil.NormalizeNewlines(string(raw))), "\n")
	changed := false
	for i := range lines {
		if i >= len(masked) {
			break
		}
		m := validator.TaskLineRe.FindStringSubmatch(masked[i])
		if m == nil || m[3] != id || !strings.EqualFold(m[2], "x") {
			continue
		}
		open := strings.IndexByte(lines[i], '[')
		close := strings.IndexByte(lines[i], ']')
		if open < 0 || close <= open {
			continue
		}
		lines[i] = lines[i][:open] + "[ ]" + lines[i][close+1:]
		changed = true
	}
	if changed {
		_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
}

// --- journal + block markers ------------------------------------------------

// journal appends one mechanical line to the run journal (log.md), plus a detail
// line per detail (§5.6). CLI-only writer.
func journal(opts RunOptions, step Step, outcome string, details ...string) {
	appendJournal(Dir(opts.Root, opts.Slug), opts.Hooks.Now(), step.Step, step.Feat, outcome, details...)
}

// parkRun leaves the post-run marker that explains why the loop ended without
// completing: the step it was working, how often it failed, and where the full
// output lives. The next `plan run` clears it and retries.
func parkRun(opts RunOptions, step Step, st *runState, reason string) {
	key := stepKey(step)
	attempts := 0
	if hist := st.hists[key]; hist != nil {
		attempts = hist.len()
		if last := firstLine(hist.last()); last != "" {
			reason += "; last failure: " + last
		}
	}
	_ = WriteBlock(opts.Root, opts.Slug, Block{
		Feat: step.Feat, Step: step.Step, Kind: BlockGateFailure,
		Reason: reason, Attempts: attempts, Log: st.logs[key],
		BlockedAt: opts.Hooks.Now().UTC().Format(time.RFC3339),
	})
	journal(opts, step, "blocked ("+BlockGateFailure+")", "reason: "+reason)
}

// clearAllBlocks retires every marker a previous run left, whatever its kind: a
// marker is only ever the record of how a previous run ended, and the autonomous
// loop retries everything. It seeds the failure-log pointers so the first
// session on a previously-failed step starts from its predecessor's evidence
// instead of rediscovering it.
func clearAllBlocks(opts RunOptions, st *runState, logf func(string, ...any)) {
	for _, b := range ListBlocks(opts.Root, opts.Slug) {
		if err := Unblock(opts.Root, opts.Slug, b, "plan run (the autonomous loop retries every feat)", opts.Hooks.Now()); err != nil {
			continue
		}
		if b.Log != "" && b.Step != "" {
			st.logs[stepKey(Step{Feat: b.Feat, Step: b.Step})] = b.Log
		}
		logf("  ↻ retrying %s (was blocked [%s]: %s)", b.Feat, b.Kind, firstLine(b.Reason))
	}
}

// reportBlocks explains why a run has nothing left to do, and how to resume — the
// ways out differ by kind, and none is discoverable from the exit code.
func reportBlocks(opts RunOptions, logf func(string, ...any)) {
	blocks := ListBlocks(opts.Root, opts.Slug)
	if len(blocks) == 0 {
		return
	}
	halts := 0
	for _, b := range blocks {
		logf("  · %s [%s] %s", b.Feat, b.Kind, firstLine(b.Reason))
		if b.Log != "" {
			logf("      log: %s", b.Log)
		}
		if b.Kind == BlockHalt {
			halts++
		}
	}
	if halts > 0 {
		logf("resume: fix the impediment the session reported, then `csdd plan run %s` — it retries every feat", opts.Slug)
	} else {
		logf("resume: `csdd plan run %s` retries every feat; `csdd plan unblock %s --all --force` clears the markers by hand", opts.Slug, opts.Slug)
	}
}

// runLogf is the runner's line-oriented logger onto opts.Out.
func runLogf(opts RunOptions) func(string, ...any) {
	return func(format string, a ...any) { _, _ = fmt.Fprintf(opts.Out, format+"\n", a...) }
}

// --- small helpers ----------------------------------------------------------

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
	installRealHooks(&opts.Hooks)
}

func summarize(out io.Writer, sum RunSummary) RunSummary {
	_, _ = fmt.Fprintf(out, "totals: %d sessions, %d steps advanced, %d decisions recorded, %d failed iterations\n",
		sum.Sessions, sum.Steps, sum.Decisions, sum.Failures)
	return sum
}

func appendFile(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(content)
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
