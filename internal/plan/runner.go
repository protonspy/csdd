package plan

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionMode is the permission posture a runner session runs under. It is part
// of the exported Hooks contract so external drivers (and tests) can construct a
// Session stub.
type SessionMode int

const (
	// ModeSupervised passes --permission-mode acceptEdits: edits apply but the
	// human is in the loop for anything riskier.
	ModeSupervised SessionMode = iota
	// ModeAutonomous passes --dangerously-skip-permissions; only ever selected
	// after `sandbox doctor` proves isolation (principle 7).
	ModeAutonomous
)

func (m SessionMode) String() string {
	if m == ModeAutonomous {
		return "autonomous"
	}
	return "supervised"
}

// forbiddenChangeRoots are the workspace subtrees a session must never modify
// (R9.6). The runner owns the plan tree, the graph, and the state dir; a session
// touching any of them is a hard step failure, verified from the git working tree
// AFTER the session and BEFORE the runner writes its own journal/markers.
var forbiddenChangeRoots = []string{".csdd/", "docs/graph/", "docs/plans/"}

// Hooks are the runner's seams onto the outside world. Every field has a real
// default (installed by Run); tests inject stubs so the whole loop — verdict
// parsing, gate pipeline, retries, block markers, path audit, journal — runs with
// no `claude`, `git`, or subprocess at all.
type Hooks struct {
	// Session runs one claude session for a step and returns its parsed verdict.
	Session func(step Step, brief string, mode SessionMode, budgetUSD float64) (Verdict, error)
	// CSDD runs `csdd <args...>` in root, returning success and combined output.
	CSDD func(root string, args ...string) (bool, string)
	// Gate runs a shell gate command in root, returning success and output.
	Gate func(root, command string) (bool, string)
	// ChangedPaths returns the workspace-relative paths dirty in the working tree.
	ChangedPaths func(root string) ([]string, error)
	// Commit stages paths and commits them with msg.
	Commit func(root, msg string, paths []string) error
	// Doctor proves sandbox isolation (autonomous mode only).
	Doctor func() SandboxReport
	// ClaudeAvailable reports whether the `claude` CLI is on PATH.
	ClaudeAvailable func() bool
	// Now returns the current time (journal timestamps; injected for determinism).
	Now func() time.Time
}

// RunOptions configures a `csdd plan run` invocation.
type RunOptions struct {
	Root          string
	Slug          string
	Autonomous    bool
	SessionBudget float64 // per-session --max-budget-usd; 0 = no cap (Claude account limits)
	MaxIterations int     // default 25
	MaxRetries    int     // default 2
	Out           io.Writer
	Hooks         Hooks
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
	Blocks    int
	Completed bool
	Reason    string
	Outcome   int
}

// Run drives the plan loop (R9): preflight, then repeatedly drift-check → next →
// brief → session → audit → gates → advance/retry/block → journal, until the plan
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
	mode := ModeSupervised
	if opts.Autonomous {
		rep := h.Doctor()
		if !rep.OK {
			return RunSummary{}, fmt.Errorf("--autonomous requires a verified sandbox; `sandbox doctor` failed — refusing to pass --dangerously-skip-permissions")
		}
		mode = ModeAutonomous
	}
	logf("plan run %s (%s mode, %s, max %d iterations)", opts.Slug, mode, budgetLabel(opts.SessionBudget), opts.MaxIterations)

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
			return summarize(out, sum), nil
		case SeqNotReady:
			sum.Outcome = OutcomeDrift
			sum.Reason = "plan not approved or drifted"
			logf(sum.Reason)
			return summarize(out, sum), nil
		}

		logf("→ %s / %s", step.Feat, step.Step)
		advanced := executeStep(opts, doc, step, mode, &sum)
		if !advanced {
			sum.Blocks++
		}
	}
	sum.Outcome = OutcomeCapped
	sum.Reason = fmt.Sprintf("reached the iteration cap (%d)", opts.MaxIterations)
	logf(sum.Reason)
	return summarize(out, sum), nil
}

// executeStep runs one step with retries (R9.2/R9.3). It returns whether the step
// advanced (green) or was blocked (retries exhausted, forbidden change, or a
// blocked verdict). A blocked feat gets a marker so the sequencer skips it and
// the loop continues with other unblocked feats (R9.4).
func executeStep(opts RunOptions, doc *PlanDoc, step Step, mode SessionMode, sum *RunSummary) bool {
	h := opts.Hooks
	out := opts.Out
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }

	baseBrief, err := Brief(opts.Root, doc, step)
	if err != nil {
		logf("  brief error: %v", err)
		block(opts, step, "brief assembly failed: "+err.Error())
		return false
	}

	// Scaffold a spec before its requirements step (R8; runner-owned).
	if step.Step == StepSpecRequirements {
		specDir := filepath.Join(opts.Root, "specs", step.Feat)
		if _, statErr := os.Stat(specDir); statErr != nil {
			if ok, o := h.CSDD(opts.Root, "plan", "generate", opts.Slug, step.Feat, "--require-approved"); !ok {
				block(opts, step, "scaffold (plan generate) failed: "+firstLine(o))
				return false
			}
		}
	}

	var lastFailure string
	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		if attempt > 0 {
			sum.Retries++
			logf("  retry %d/%d", attempt, opts.MaxRetries)
		}
		brief := baseBrief
		if lastFailure != "" {
			brief += "\n\n## Previous attempt failed — fix this\n\n" + lastFailure + "\n"
		}

		verdict, err := h.Session(step, brief, mode, opts.SessionBudget)
		if err != nil {
			lastFailure = "session error: " + err.Error()
			continue
		}
		if verdict.Status == VerdictBlocked {
			// A structured deviation: journal it and stop this feat (R9.4).
			journalDeviation(opts, step, verdict)
			block(opts, step, deviationReason(verdict))
			logf("  blocked by session: %s", firstLine(verdict.Summary))
			return false
		}

		// R9.6 path audit: the session must not have touched the plan/graph/state.
		if bad := auditForbidden(opts); bad != "" {
			lastFailure = "modified a forbidden path (" + bad + "); the runner owns the plan, graph, and state — revert it"
			logf("  hard failure: %s", lastFailure)
			continue
		}

		// Gates.
		if ok, output := runGates(opts, step); !ok {
			lastFailure = "gate failed:\n" + output
			logf("  gate failed")
			continue
		}

		// Green: the runner performs the advance (approval or commit), never the session.
		if err := advance(opts, step); err != nil {
			lastFailure = "advance failed: " + err.Error()
			logf("  advance failed: %v", err)
			continue
		}
		sum.Steps++
		journal(opts, step, "done")
		logf("  ✓ %s", step.Step)
		return true
	}

	// Retries exhausted.
	journal(opts, step, "blocked")
	block(opts, step, "gates failed after "+itoa(opts.MaxRetries)+" retries: "+firstLine(lastFailure))
	logf("  ✗ blocked after retries")
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

// advance performs the green-path mutation the runner (never the session) owns:
// approve the spec phase, or commit the implementation scoped to the session's
// changed paths (R9.2).
func advance(opts RunOptions, step Step) error {
	h := opts.Hooks
	if phase, ok := specPhaseOf(step.Step); ok {
		if approved, o := h.CSDD(opts.Root, "spec", "approve", step.Feat, "--phase", phase); !approved {
			return fmt.Errorf("spec approve %s --phase %s: %s", step.Feat, phase, firstLine(o))
		}
		return nil
	}
	changed, err := h.ChangedPaths(opts.Root)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return fmt.Errorf("no changes to commit for %s", step.Step)
	}
	return h.Commit(opts.Root, fmt.Sprintf("feat(%s): %s", step.Feat, step.Step), changed)
}

// auditForbidden returns the first changed path under a forbidden root, or "".
func auditForbidden(opts RunOptions) string {
	changed, err := opts.Hooks.ChangedPaths(opts.Root)
	if err != nil {
		return "" // a git error is not a forbidden-change signal
	}
	for _, p := range changed {
		p = filepath.ToSlash(p)
		for _, root := range forbiddenChangeRoots {
			if strings.HasPrefix(p, root) {
				return p
			}
		}
	}
	return ""
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

// --- journal + block markers ------------------------------------------------

// journal appends one mechanical line to the run journal (log.md): the
// `## [YYYY-MM-DD] <step> | <feat> | <outcome>` format (§5.6). CLI-only writer.
func journal(opts RunOptions, step Step, outcome string) {
	date := opts.Hooks.Now().UTC().Format("2006-01-02")
	line := fmt.Sprintf("## [%s] %s | %s | %s\n", date, step.Step, step.Feat, outcome)
	appendFile(filepath.Join(Dir(opts.Root, opts.Slug), "log.md"), line)
}

// journalDeviation appends the blocked entry plus the session's revision proposal,
// so the human can fold it back in via the Revise workflow (principle 5).
func journalDeviation(opts RunOptions, step Step, v Verdict) {
	date := opts.Hooks.Now().UTC().Format("2006-01-02")
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] %s | %s | %s\n", date, step.Step, step.Feat, VerdictBlocked)
	if v.Summary != "" {
		fmt.Fprintf(&b, "- reason: %s\n", oneLine(v.Summary))
	}
	if v.Revision != "" {
		fmt.Fprintf(&b, "- revision: %s\n", oneLine(v.Revision))
	}
	appendFile(filepath.Join(Dir(opts.Root, opts.Slug), "log.md"), b.String())
}

// block writes a block marker so the sequencer skips the feat and the loop
// continues with others (R9.4). Markers live under the transient state dir.
func block(opts RunOptions, step Step, reason string) {
	path := filepath.Join(stateDir(opts.Root, opts.Slug), "blocked", step.Feat)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		_ = os.WriteFile(path, []byte(reason+"\n"), 0o644)
	}
}

func deviationReason(v Verdict) string {
	if v.Summary != "" {
		return "session blocked: " + oneLine(v.Summary)
	}
	return "session returned a blocked verdict"
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
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	installRealHooks(&opts.Hooks)
}

func summarize(out io.Writer, sum RunSummary) RunSummary {
	_, _ = fmt.Fprintf(out, "totals: %d steps, %d retries, %d blocks\n", sum.Steps, sum.Retries, sum.Blocks)
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
