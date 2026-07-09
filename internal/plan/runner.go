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
// parsing, gate pipeline, retries, block markers, journal — runs with no
// `claude`, `csdd`, or subprocess at all. The runner never touches git: branch,
// commit, and the PR are the csdd dev-cycle's job, driven inside the session.
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

// RunOptions configures a `csdd plan run` invocation.
type RunOptions struct {
	Root          string
	Slug          string
	AssumeYes     bool    // accept the unverified-sandbox alert without prompting (--yes)
	SessionBudget float64 // per-session --max-budget-usd; 0 = no cap (Claude account limits)
	MaxIterations int     // default 25
	MaxRetries    int     // default 2
	// MaxRepairs is how many repair sessions a single feat may consume before its
	// mechanical block turns sticky and a human has to look. The count accumulates
	// in the marker across runs and resets whenever the feat advances. Zero — the
	// zero value, so a library caller opts in — disables repair entirely: a
	// mechanical block then stays put until `csdd plan unblock` clears it.
	MaxRepairs int
	Out        io.Writer
	Hooks      Hooks
}

// Run outcomes, chosen so the CLI can surface them as distinct exit codes.
const (
	OutcomeComplete = 0 // every feat is done
	OutcomeNothing  = 4 // nothing unblocked (mirrors the sequencer)
	OutcomeDrift    = 5 // plan drifted mid-run
	OutcomeCapped   = 6 // hit the iteration cap without finishing
)

// RunSummary is the totals reported on exit (R9.7).
type RunSummary struct {
	Steps     int
	Retries   int
	Repairs   int // repair sessions spent
	Repaired  int // steps a repair session rescued
	Blocks    int
	Completed bool
	Reason    string
	Outcome   int
}

// Run drives the plan loop (R9): preflight, then repeatedly drift-check → next →
// brief → session → gates → advance/retry/block → journal, until the plan
// completes, nothing is unblocked, drift is detected, or the iteration cap is hit.
func Run(opts RunOptions) (RunSummary, error) {
	fillRunDefaults(&opts)
	h := opts.Hooks
	out := opts.Out
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }

	doc, err := Load(opts.Root, opts.Slug)
	if err != nil {
		return RunSummary{}, err
	}

	// Preflight (R9.1): approval + drift, claude present, mode selection.
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
	logf("plan run %s (bypass mode, %s, max %d iterations)", opts.Slug, budgetLabel(opts.SessionBudget), opts.MaxIterations)

	// A mechanical block is a fact about one attempt, not about the plan, so it is
	// run-scoped: the next run retries the feat instead of skipping it forever.
	// Markers that already spent their repair budget stay put, and deviations always
	// do — only revising the plan retires those.
	carried := clearRepairableBlocks(opts, logf)

	var sum RunSummary
	for iter := 0; iter < opts.MaxIterations; iter++ {
		// R9.5: re-check drift every iteration; stop autonomy immediately on mismatch.
		if _, drift, err := IsApproved(opts.Root, opts.Slug); err != nil {
			return sum, err
		} else if drift {
			sum.Reason = "stopped: plan drifted mid-run (re-approve to continue)"
			sum.Outcome = OutcomeDrift
			logf(sum.Reason)
			return summarize(out, sum), nil
		}

		step, outcome, err := Next(opts.Root, doc, true)
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
			sum.Outcome = OutcomeNothing
			sum.Reason = "nothing unblocked: remaining feats are blocked or waiting on blocked deps"
			logf(sum.Reason)
			reportBlocks(opts, logf)
			return summarize(out, sum), nil
		case SeqNotReady:
			sum.Outcome = OutcomeDrift
			sum.Reason = "plan not approved or drifted"
			logf(sum.Reason)
			return summarize(out, sum), nil
		}

		logf("→ %s / %s", step.Feat, step.Step)
		advanced := executeStep(opts, doc, step, &sum, carried)
		if !advanced {
			sum.Blocks++
		}
	}
	sum.Outcome = OutcomeCapped
	sum.Reason = fmt.Sprintf("reached the iteration cap (%d)", opts.MaxIterations)
	logf(sum.Reason)
	return summarize(out, sum), nil
}

// stepResult is what one session-plus-gates attempt produced.
type stepResult int

const (
	stepFailed    stepResult = iota // mechanical: recorded on the history, retryable
	stepDone                        // green: gates passed and the phase advanced
	stepDeviation                   // the session blocked on an open decision
)

// executeStep runs one step with retries (R9.2/R9.3), then — if the failure was
// mechanical and the feat has repair budget left — one repair session that sees
// every failed attempt rather than just the last. It returns whether the step
// advanced. A feat that still fails gets a typed marker so the sequencer skips it
// and the loop continues with other unblocked feats (R9.4); carried tracks the
// repair sessions already spent on each feat, so the budget survives across runs.
func executeStep(opts RunOptions, doc *PlanDoc, step Step, sum *RunSummary, carried map[string]Block) bool {
	h := opts.Hooks
	logf := runLogf(opts)

	baseBrief, err := Brief(opts.Root, doc, step)
	if err != nil {
		logf("  brief error: %v", err)
		blockStep(opts, step, BlockBrief, "brief assembly failed: "+err.Error(), 0, spentRepairs(carried, step.Feat), "")
		return false
	}

	// Scaffold a spec before its requirements step (R8; runner-owned).
	if step.Step == StepSpecRequirements {
		specDir := filepath.Join(opts.Root, "specs", step.Feat)
		if _, statErr := os.Stat(specDir); statErr != nil {
			if ok, o := h.CSDD(opts.Root, "plan", "generate", opts.Slug, step.Feat, "--require-approved"); !ok {
				blockStep(opts, step, BlockScaffold, "scaffold (plan generate) failed: "+firstLine(o), 0, spentRepairs(carried, step.Feat), "")
				return false
			}
		}
	}

	hist := &failureHistory{}
	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		if attempt > 0 {
			sum.Retries++
			logf("  retry %d/%d", attempt, opts.MaxRetries)
		}
		brief := baseBrief
		if last := hist.last(); last != "" {
			brief += "\n\n## Previous attempt failed — fix this\n\n" + tailCap(last, briefFailureCap) + "\n"
		}
		switch tryStep(opts, step, brief, hist, logf) {
		case stepDone:
			journal(opts, step, "done")
			sum.Steps++
			delete(carried, step.Feat) // the feat moved: its repair streak resets
			logf("  ✓ %s", step.Step)
			return true
		case stepDeviation:
			return false // tryStep already journaled and marked it
		}
	}

	// Every retry is gone and the failure is mechanical. Persist the whole history
	// first — the retry briefs only ever carried a truncated tail, and without this
	// file the actual compiler or test output dies with the process.
	logRel := writeFailureLog(opts, step, hist)
	spent := spentRepairs(carried, step.Feat)
	if spent < opts.MaxRepairs {
		spent++
		sum.Repairs++
		logf("  ⚒ repair session %d/%d — replaying %d failed attempt(s)", spent, opts.MaxRepairs, hist.len())
		switch tryStep(opts, step, repairBrief(baseBrief, hist, logRel), hist, logf) {
		case stepDone:
			journal(opts, step, "done (repaired)")
			sum.Steps++
			sum.Repaired++
			delete(carried, step.Feat)
			logf("  ✓ %s (repaired)", step.Step)
			return true
		case stepDeviation:
			return false
		}
		logRel = writeFailureLog(opts, step, hist) // fold the repair attempt in
	}

	// The session checked the task box on an attempt the gates then refuted. Retract
	// the claim before parking the feat: a later unblock derives the feat's state
	// from that box, and a checked one would walk straight past a task that never
	// went green.
	retractTaskClaim(opts, step)
	reason := "gates failed after " + itoa(hist.len()) + " attempts: " + firstLine(hist.last())
	journal(opts, step, "blocked ("+BlockGateFailure+")", "attempts: "+itoa(hist.len()), "log: "+logRel)
	blockStep(opts, step, BlockGateFailure, reason, hist.len(), spent, logRel)
	logf("  ✗ blocked after %d attempt(s) — full output in %s", hist.len(), logRel)
	if spent >= opts.MaxRepairs {
		logf("    → repair budget spent; `csdd plan unblock %s %s` to retry", opts.Slug, step.Feat)
	}
	return false
}

// tryStep runs one session for the step and, when it claims done, the gates and the
// runner-owned approval. Mechanical failures land on the history so the next attempt
// — and the repair session after it — can read them; a `blocked` verdict is a
// deviation and ends the feat here, because only a revised plan can answer it.
func tryStep(opts RunOptions, step Step, brief string, hist *failureHistory, logf func(string, ...any)) stepResult {
	verdict, err := opts.Hooks.Session(step, brief, opts.SessionBudget)
	if err != nil {
		hist.add("session error", err.Error())
		return stepFailed
	}
	if verdict.Status == VerdictBlocked {
		journalDeviation(opts, step, verdict)
		blockDeviation(opts, step, verdict)
		logf("  blocked by session: %s", firstLine(verdict.Summary))
		logf("    → fold the revision into docs/plans/%s/plan.md, then `csdd plan approve %s`", opts.Slug, opts.Slug)
		return stepDeviation
	}
	if ok, output := runGates(opts, step); !ok {
		hist.add("gate failed", output)
		logf("  gate failed")
		return stepFailed
	}
	// Green: the runner performs the approval a session must never do — it stands in
	// for the human at the phase gate. Persisting the step (branch, commit, PR) is
	// the session's csdd dev-cycle job (/csdd-commit, pre-push), never the runner's;
	// the runner leaves every write in the working tree.
	if err := advance(opts, step); err != nil {
		hist.add("advance failed", err.Error())
		logf("  advance failed: %v", err)
		return stepFailed
	}
	return stepDone
}

// spentRepairs is how many repair sessions this feat has already consumed, carried
// over from the marker the run cleared at startup.
func spentRepairs(carried map[string]Block, feat string) int { return carried[feat].Repairs }

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

// --- failure history ---------------------------------------------------------

// briefFailureCap bounds how much of one attempt's output a brief carries. A
// failing `go test` can print megabytes; the model needs the verdict, not the
// transcript, and the untruncated text is on disk in the failure log regardless.
const briefFailureCap = 4000

// attempt is one failed try at a step: which stage failed, and its full output.
type attempt struct {
	n      int
	stage  string
	output string
}

// failureHistory accumulates every failed attempt at a step. The retry brief reads
// only the last one; the repair session reads them all, which is the whole point —
// a root cause is usually visible across attempts and invisible within one.
type failureHistory struct{ attempts []attempt }

func (h *failureHistory) add(stage, output string) {
	h.attempts = append(h.attempts, attempt{n: len(h.attempts) + 1, stage: stage, output: strings.TrimRight(output, "\n")})
}

func (h *failureHistory) len() int { return len(h.attempts) }

// last renders the most recent failure for the retry brief, matching the shape the
// session has always seen: `<stage>:\n<output>`.
func (h *failureHistory) last() string {
	if len(h.attempts) == 0 {
		return ""
	}
	a := h.attempts[len(h.attempts)-1]
	return a.stage + ":\n" + a.output
}

// render lays every attempt out for the repair brief, each capped at cap bytes.
func (h *failureHistory) render(cap int) string {
	var b strings.Builder
	for _, a := range h.attempts {
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

// repairBrief is the second-chance brief: the step's original contract, plus every
// failed attempt in full, plus an instruction to diagnose before editing. The
// forbidden actions still hold — a fix that needs a decision the plan does not
// cover is still a deviation, not an improvisation.
func repairBrief(base string, hist *failureHistory, logPath string) string {
	var b strings.Builder
	b.WriteString(base)
	fmt.Fprintf(&b, "\n\n## Repair session — this step already failed %d time(s)\n\n", hist.len())
	b.WriteString("You are a fresh session re-running the SAME step. Every previous attempt is\n")
	b.WriteString("below, in order, with its output. Read them, find the ROOT CAUSE, and fix\n")
	b.WriteString("that — do not replay an edit that already failed. The step's contract and\n")
	b.WriteString("its forbidden actions above are unchanged: make the gates pass. If the real\n")
	b.WriteString("fix needs a decision this plan does not cover, return a `blocked` verdict\n")
	b.WriteString("with a revision proposal instead of improvising.\n\n")
	if logPath != "" {
		fmt.Fprintf(&b, "Full untruncated output: %s\n\n", logPath)
	}
	b.WriteString(hist.render(briefFailureCap))
	return b.String()
}

// writeFailureLog persists every attempt's untruncated output under the runner's
// state dir and returns its workspace-relative path, which both the block marker
// and the run journal point at. It is rewritten (not appended) so the file always
// mirrors the history exactly. An I/O failure yields "" — a lost log must never
// take the run down with it.
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
// checked would let the next `plan run` skip a task that never passed. Only the
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

// journalDeviation appends the blocked entry plus the session's revision proposal,
// so the human can fold it back in via the Revise workflow (principle 5).
func journalDeviation(opts RunOptions, step Step, v Verdict) {
	var details []string
	if v.Summary != "" {
		details = append(details, "reason: "+oneLine(v.Summary))
	}
	if v.Revision != "" {
		details = append(details, "revision: "+oneLine(v.Revision))
	}
	journal(opts, step, VerdictBlocked, details...)
}

// blockStep writes a mechanical block marker so the sequencer skips the feat and
// the loop continues with others (R9.4). Markers live under the transient state dir.
func blockStep(opts RunOptions, step Step, kind, reason string, attempts, repairs int, logPath string) {
	_ = WriteBlock(opts.Root, opts.Slug, Block{
		Feat: step.Feat, Step: step.Step, Kind: kind, Reason: reason,
		Attempts: attempts, Repairs: repairs, Log: logPath,
		BlockedAt: opts.Hooks.Now().UTC().Format(time.RFC3339),
	})
}

// blockDeviation records an open decision against the exact plan the session
// objected to. Binding the marker to that hash is what lets a later `plan approve`
// tell "the human revised the plan" from "the human re-approved the same one".
func blockDeviation(opts RunOptions, step Step, v Verdict) {
	hash, _ := HashPlan(Dir(opts.Root, opts.Slug))
	_ = WriteBlock(opts.Root, opts.Slug, Block{
		Feat: step.Feat, Step: step.Step, Kind: BlockDeviation,
		Reason: deviationReason(v), Revision: oneLine(v.Revision), PlanHash: hash,
		BlockedAt: opts.Hooks.Now().UTC().Format(time.RFC3339),
	})
}

// clearRepairableBlocks retires the mechanical markers a previous run left behind,
// returning them by feat so the repair budget they accumulated carries into this
// run. Deviations and markers that exhausted their budget survive, and say so.
func clearRepairableBlocks(opts RunOptions, logf func(string, ...any)) map[string]Block {
	carried := map[string]Block{}
	for _, b := range ListBlocks(opts.Root, opts.Slug) {
		switch {
		case !b.Mechanical():
			logf("  ⏸ %s stays blocked [%s]: %s", b.Feat, b.Kind, firstLine(b.Reason))
		case b.Repairs >= opts.MaxRepairs:
			logf("  ⏸ %s stays blocked: %d/%d repair sessions spent — `csdd plan unblock %s %s` to retry",
				b.Feat, b.Repairs, opts.MaxRepairs, opts.Slug, b.Feat)
		default:
			if err := ClearBlock(opts.Root, opts.Slug, b.Feat); err == nil {
				carried[b.Feat] = b
				logf("  ↻ retrying %s (%d/%d repair sessions spent)", b.Feat, b.Repairs, opts.MaxRepairs)
			}
		}
	}
	return carried
}

// reportBlocks explains why a run has nothing left to do, and how to resume — the
// two ways out differ by kind, and neither is discoverable from the exit code.
func reportBlocks(opts RunOptions, logf func(string, ...any)) {
	blocks := ListBlocks(opts.Root, opts.Slug)
	if len(blocks) == 0 {
		return
	}
	deviations, mechanical, untyped := 0, 0, 0
	for _, b := range blocks {
		logf("  · %s [%s] %s", b.Feat, b.Kind, firstLine(b.Reason))
		if b.Log != "" {
			logf("      log: %s", b.Log)
		}
		switch {
		case b.Kind == BlockDeviation:
			deviations++
			if b.Revision != "" {
				logf("      proposed revision: %s", firstLine(b.Revision))
			}
		case b.Mechanical():
			mechanical++
		default:
			untyped++
		}
	}
	if deviations > 0 {
		logf("resume: fold the revisions into docs/plans/%s/plan.md, then `csdd plan approve %s`", opts.Slug, opts.Slug)
	}
	if mechanical > 0 {
		logf("resume: `csdd plan unblock %s --all` clears the mechanical blocks and retries them", opts.Slug)
	}
	if untyped > 0 {
		logf("resume: `csdd plan unblock %s <feat> --force` clears the untyped markers left by an older csdd", opts.Slug)
	}
}

func deviationReason(v Verdict) string {
	if v.Summary != "" {
		return "session blocked: " + oneLine(v.Summary)
	}
	return "session returned a blocked verdict"
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
		opts.MaxIterations = 25
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 2
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 2
	}
	// MaxRepairs 0 means "no repair sessions": the zero value keeps a library caller
	// on the pre-repair behavior, and `--max-repairs 0` is how a human turns the
	// autonomy off. Only a negative value is a mistake worth normalizing.
	if opts.MaxRepairs < 0 {
		opts.MaxRepairs = 0
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	installRealHooks(&opts.Hooks)
}

func summarize(out io.Writer, sum RunSummary) RunSummary {
	_, _ = fmt.Fprintf(out, "totals: %d steps, %d retries, %d repairs, %d blocks\n",
		sum.Steps, sum.Retries, sum.Repairs, sum.Blocks)
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
