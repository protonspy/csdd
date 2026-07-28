package cli

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/plan"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/telegram"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/workspace"
)

// runPlan dispatches `csdd plan <action> ...`. M1 ships init/validate/status;
// approve/next/brief/generate/run land in later milestones (§5.8).
func runPlan(args []string, templates embed.FS) int {
	action, rest, err := parseAction("plan", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if isHelpFlag(action) {
		help(os.Stdout)
		return 0
	}
	switch action {
	case "init":
		return planInit(rest, templates)
	case "list":
		return planList(rest)
	case "validate":
		return planValidate(rest)
	case "status":
		return planStatus(rest)
	case "approve":
		return planApprove(rest)
	case "next":
		return planNext(rest)
	case "brief":
		return planBrief(rest)
	case "generate":
		return planGenerate(rest, templates)
	case "run":
		return planRun(rest, templates)
	case "cost":
		return planCost(rest)
	case "verify":
		return planVerify(rest)
	default:
		render.Err("unknown plan action: " + action)
		return 1
	}
}

func planRun(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("plan run", flag.ContinueOnError)
	var root, model, effort, enrichModel string
	var autonomous, assumeYes, noTelegram bool
	var sessionBudget float64
	var maxIterations, stall, featAttempts, maxRetries, maxRepairs, squadLimit int
	var sessionIdle time.Duration
	addRoot(fs, &root)
	fs.BoolVar(&assumeYes, "yes", false, "Skip the unverified-sandbox prompt: accept running --dangerously-skip-permissions even when `sandbox doctor` fails.")
	fs.BoolVar(&noTelegram, "no-telegram", false, "Do not auto-start the Telegram notifier even when a bot is configured (.csdd/bot.json).")
	fs.BoolVar(&autonomous, "autonomous", false, "Deprecated no-op: plan run always runs bypass-mode (--dangerously-skip-permissions).")
	fs.StringVar(&model, "model", "opus", "Model the orchestrating session runs on (claude --model): sonnet|opus|haiku|fable or a full model ID. It reviews and decides the spec; spec authoring is delegated to the `spec-author` sub-agent (sonnet) and task implementation to the `implementer` sub-agent (each on its own, cheaper model). Empty inherits the ambient default.")
	fs.StringVar(&effort, "effort", "medium", "Reasoning effort the orchestrating session runs at (claude --effort): low|medium|high|xhigh|max. Empty inherits the ambient default.")
	fs.StringVar(&enrichModel, "enrich-model", "sonnet", "Model the per-feat context pass runs on (claude --model). Before each feat is dispatched it reads the worktree once and records what the feat touches, what governs it and what is already there, so the orchestrator does not rediscover it every attempt. `none` turns the pass off and briefs from the plan alone.")
	fs.Float64Var(&sessionBudget, "session-budget", 0, "Per-session cap in USD (claude --max-budget-usd). Default 0 = no cap; the session runs under the Claude account's own limits.")
	fs.IntVar(&maxIterations, "max-iterations", 30, "Sessions the run may spend; one iteration is one claude session.")
	fs.IntVar(&stall, "stall", 10, "Stop early after this many consecutive sessions without a step advancing.")
	fs.DurationVar(&sessionIdle, "session-idle", 0, "Kill a session that makes no progress — no event stream output and no CPU — for this long (default 15m). Not a time limit: real work of any duration keeps resetting it.")
	fs.IntVar(&featAttempts, "feat-attempts", 0, "Stop handing out ONE feat after this many sessions and surface it as blocked (default 4). Bounds a feat whose `done` the verdict gate keeps refusing.")
	fs.IntVar(&squadLimit, "squad-limit", 0, "Maximum claude sessions running at once, each on its own feat in its own git worktree (1..6, default 1). Feats run together whenever the plan's Depends graph allows it; each delivered feat is merged into the run's base branch. Requires a clean git repository.")
	fs.IntVar(&maxRetries, "max-retries", 0, "Deprecated no-op: each iteration is one session, and the next iteration is the retry.")
	fs.IntVar(&maxRepairs, "max-repairs", 0, "Deprecated no-op: the self-correcting loop replaced repair sessions.")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan run SLUG [--model M] [--effort E] [--yes] [--session-budget N] [--max-iterations N] [--stall N] [--feat-attempts N]")
		return 1
	}
	if err := validateEffort(effort); err != nil {
		render.Err(err.Error())
		return 1
	}
	// 0 means "unset" so the runner's default applies; anything else must land in
	// 1..6. The ceiling is the widest topological wave a real plan admitted — past
	// it a plan cannot use the concurrency, and the shared Claude account limit is
	// consumed that much faster for nothing.
	if squadLimit < 0 || squadLimit > planSquadLimitMax {
		render.Err(fmt.Sprintf("--squad-limit must be between 1 and %d (got %d)", planSquadLimitMax, squadLimit))
		return 1
	}
	// `none` is how a human spells "no enrichment pass"; the runner reads an empty
	// model as disabled.
	if strings.EqualFold(strings.TrimSpace(enrichModel), "none") {
		enrichModel = ""
	}
	if autonomous {
		render.Warn("--autonomous is deprecated and now a no-op: plan run always runs bypass-mode")
	}
	if maxRetries != 0 || maxRepairs != 0 {
		render.Warn("--max-retries/--max-repairs are deprecated no-ops: every failure feeds the next session, bounded by --max-iterations and --stall")
	}
	slug := positionals[0]
	if err := workspace.SafeName(slug, "plan"); err != nil {
		render.Err(err.Error())
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	// Auto-start the Telegram notifier for the life of the run when a bot is
	// configured, so run progress reaches the chat without a separate
	// `csdd telegram run`. No-op when unconfigured or suppressed with --no-telegram.
	if !noTelegram {
		stopTelegram := startPlanTelegram(r)
		defer stopTelegram()
	}

	sum, err := plan.Run(plan.RunOptions{
		Root:          r,
		Slug:          slug,
		AssumeYes:     assumeYes,
		SessionBudget: sessionBudget,
		Model:         model,
		Effort:        effort,
		MaxIterations: maxIterations,
		Stall:         stall,
		FeatAttempts:  featAttempts,
		SessionIdle:   sessionIdle,
		SquadLimit:    squadLimit,
		EnrichModel:   enrichModel,
		WorktreeEntry: planEntryDoc(templates),
		Out:           os.Stdout,
	})
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	// Surface the run outcome as a distinct exit code (R9.4/R9.7); the summary is
	// already printed by the runner.
	return sum.Outcome
}

// planSquadLimitMax is the hard ceiling on --squad-limit. It is not arbitrary: the
// widest topological wave the evidence plan (`agency-telegram-platform`, 31 feats)
// admits is 6, so beyond it a plan's own Depends graph cannot supply the
// parallelism, and the only effect of a larger number would be to burn the shared
// Claude account limit faster.
const planSquadLimitMax = 6

// startPlanTelegram auto-starts the read-only Telegram notifier for the duration
// of a plan run when a bot is configured (.csdd/bot.json). The notifier polls the
// run journal (docs/plans/<slug>/log.md) the runner appends to, so each feat's
// done/progress/failed line — and every spec approval the sessions make — reaches
// the chat live. It returns a stop function that cancels the notifier and lets it
// flush the run's final journal lines. When no bot is configured (or the config is
// invalid) it is a silent no-op: Telegram stays opt-in via `csdd telegram init`.
func startPlanTelegram(root string) func() {
	cfg, err := telegram.Load(root)
	if err != nil || cfg.Validate() != nil {
		return func() {}
	}
	notifier := telegram.NewNotifier(telegram.Options{
		Root:     root,
		Client:   telegram.NewClient(cfg.Token, cfg.ChatID, apiBase()),
		Interval: time.Duration(cfg.IntervalSeconds) * time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = notifier.Run(ctx)
	}()
	render.Info("telegram bot configured — relaying plan-run status to chat " + cfg.ChatID)
	return func() {
		cancel()
		// Bound the wait so a slow or unreachable Telegram never wedges the CLI
		// after the run itself has finished.
		select {
		case <-done:
		case <-time.After(6 * time.Second):
		}
	}
}

// requireWorkspaceMarker enforces R1.3: every plan command operates inside an
// initialized workspace (.csdd/ present), failing with actionable guidance
// otherwise instead of silently scaffolding into a non-workspace directory.
func requireWorkspaceMarker(root string) error {
	if !pathExists(paths.State(root)) {
		return fmt.Errorf("not a csdd workspace: no %s/ found under %s. Run `%s init` first", paths.StateDir, root, prog())
	}
	return nil
}

func planInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("plan init", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan init SLUG")
		return 1
	}
	slug := positionals[0]
	if err := workspace.KebabCheck(slug, "plan"); err != nil {
		render.Err(err.Error())
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if err := requireWorkspaceMarker(r); err != nil {
		render.Err(err.Error())
		return 1
	}

	dir := plan.Dir(r, slug)
	content, err := templater.Render(templates, "templates/plan/plan.md.tmpl", map[string]string{"Name": slug})
	if err != nil {
		render.Err(err.Error())
		return 1
	}

	// Idempotent scaffold (R1.2): create only what is missing; --force overwrites.
	planMD := filepath.Join(dir, "plan.md")
	switch {
	case pathExists(planMD) && !force:
		render.Info(workspace.Relative(r, planMD) + " exists; left untouched (--force to overwrite)")
	default:
		if err := workspace.WriteFile(planMD, content, force); err != nil {
			render.Err(err.Error())
			return 1
		}
		render.OK("created " + workspace.Relative(r, planMD))
	}
	seeds := filepath.Join(dir, "seeds")
	if !pathExists(seeds) {
		if err := mkdirAll(seeds); err != nil {
			render.Err(err.Error())
			return 1
		}
		render.OK("created " + workspace.Relative(r, seeds) + "/")
	}
	render.Info(fmt.Sprintf("next: author the feats, then `%s plan validate %s`", prog(), slug))
	return 0
}

// planList is the whole-workspace view `plan status` lacks: every plan under
// docs/plans/ with its approval state and delivery progress, so you can see what
// exists before drilling into one.
func planList(args []string) int {
	fs := flag.NewFlagSet("plan list", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sums, err := plan.Summaries(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if jsonOut {
		rows := make([]planSummaryJSON, 0, len(sums))
		for _, s := range sums {
			rows = append(rows, planSummaryJSON{
				Slug: s.Slug, Name: s.Name, Approved: s.Approved, Drift: s.Drift,
				Feats: s.Feats, Done: s.Done, Complete: s.Complete,
			})
		}
		return emitJSON(rows)
	}
	if len(sums) == 0 {
		render.Info("no plans found; create one with `" + prog() + " plan init SLUG`")
		return 0
	}
	printPlanList(sums)
	return 0
}

// printPlanList renders one line per plan. Like printPlanStatus, the columns hold
// plain (un-colored) words so the fixed widths stay aligned regardless of terminal
// color support.
func printPlanList(sums []plan.Summary) {
	maxName := len("plan")
	for _, s := range sums {
		if len(s.Slug) > maxName {
			maxName = len(s.Slug)
		}
	}
	drifted := false
	fmt.Printf("  %-*s  %-8s  %-9s  %s\n", maxName, "plan", "approval", "feats", "name")
	for _, s := range sums {
		approval := "draft"
		switch {
		case s.Drift:
			// Drift outranks approval in the column: the approval exists but no
			// longer binds the current plan.md, which is what `plan run` refuses on.
			approval = "drift"
			drifted = true
		case s.Approved:
			approval = "approved"
		}
		feats := "—"
		if s.Feats > 0 {
			feats = fmt.Sprintf("%d/%d", s.Done, s.Feats)
			if s.Complete {
				feats += " ✓"
			}
		}
		fmt.Printf("  %-*s  %-8s  %-9s  %s\n", maxName, s.Slug, approval, feats, s.Name)
	}
	if drifted {
		render.Warn("drift: plan.md/seeds changed since approval — re-approve before running")
	}
}

// resolvePlan is the shared preamble for validate/status: resolve the root and
// load+parse the plan, mapping the common failures to exit 1.
func resolvePlan(root, slug string) (string, *plan.PlanDoc, int) {
	if err := workspace.SafeName(slug, "plan"); err != nil {
		render.Err(err.Error())
		return "", nil, 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return "", nil, 1
	}
	doc, err := plan.Load(r, slug)
	if err != nil {
		render.Err(err.Error())
		return "", nil, 1
	}
	return r, doc, 0
}

func planValidate(args []string) int {
	fs := flag.NewFlagSet("plan validate", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan validate SLUG [--json]")
		return 1
	}
	slug := positionals[0]
	r, doc, code := resolvePlan(root, slug)
	if code != 0 {
		return code
	}
	issues := plan.ValidatePlan(doc, r)
	if jsonOut {
		emitJSON(validationJSON{Target: slug, OK: len(issues) == 0, Issues: issuesToJSON(issues)})
		if len(issues) > 0 {
			return 2
		}
		return 0
	}
	if len(issues) == 0 {
		render.OK(slug + ": validation passed")
		return 0
	}
	for _, i := range issues {
		render.Err(i.String())
	}
	return 2
}

func planStatus(args []string) int {
	fs := flag.NewFlagSet("plan status", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan status SLUG [--json]")
		return 1
	}
	slug := positionals[0]
	r, doc, code := resolvePlan(root, slug)
	if code != 0 {
		return code
	}
	st, err := plan.DeriveStatus(r, doc)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if jsonOut {
		return emitJSON(st)
	}
	printPlanStatus(st)
	return 0
}

func planApprove(args []string) int {
	fs := flag.NewFlagSet("plan approve", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan approve SLUG")
		return 1
	}
	r, doc, code := resolvePlan(root, positionals[0])
	if code != 0 {
		return code
	}
	issues := plan.ValidatePlan(doc, r)
	if len(issues) > 0 {
		for _, i := range issues {
			render.Err(i.String())
		}
		if !force {
			render.Err("approval blocked by validation findings; fix them or pass --force")
			return 2
		}
		render.Warn("approving despite validation findings (--force)")
	}
	if err := plan.ApprovePlan(r, doc, time.Now()); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK(doc.Slug + ": approved (bound to the current plan.md + seeds hash)")
	return 0
}

func planNext(args []string) int {
	fs := flag.NewFlagSet("plan next", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan next SLUG [--json]")
		return 1
	}
	r, doc, code := resolvePlan(root, positionals[0])
	if code != 0 {
		return code
	}
	// The sequencer gates on approval so `next` reports not-approved as its own exit
	// code (R6.3), matching what `run` sees.
	feat, outcome, err := plan.NextFeat(r, doc, true)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	switch outcome {
	case plan.SeqFeat:
		if jsonOut {
			return emitJSON(map[string]string{"feat": feat.Slug, "objective": feat.Objective, "milestone": feat.Milestone})
		}
		render.OK(feat.Slug)
		if feat.Objective != "" {
			render.Info(feat.Objective)
		}
		return 0
	case plan.SeqComplete:
		if jsonOut {
			emitJSON(map[string]string{"status": "complete"})
		} else {
			render.OK("plan complete: every feat is delivered")
		}
		return 3
	default: // SeqNotReady
		if jsonOut {
			emitJSON(map[string]string{"status": "not_ready"})
		} else {
			render.Err("plan is not approved; run `" + prog() + " plan approve " + doc.Slug + "`")
		}
		return 5
	}
}

func planBrief(args []string) int {
	fs := flag.NewFlagSet("plan brief", flag.ContinueOnError)
	var root, feat, enrichModel string
	var refresh bool
	addRoot(fs, &root)
	fs.StringVar(&feat, "feat", "", "Feat to brief (default: the sequencer's next feat).")
	fs.BoolVar(&refresh, "refresh", false, "Re-run the context pass for this feat before printing, replacing its stored pack. This is the same pass `plan run` makes before dispatching a feat, so it is how you review — and, by editing .csdd/plan/<slug>/briefs/<feat>.json, correct — what a session will be handed.")
	fs.StringVar(&enrichModel, "enrich-model", "sonnet", "Model --refresh runs the context pass on (claude --model).")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan brief SLUG [--feat F] [--refresh]")
		return 1
	}
	r, doc, code := resolvePlan(root, positionals[0])
	if code != 0 {
		return code
	}
	var target plan.Feat
	if feat != "" {
		f, ok := doc.Feat(feat)
		if !ok {
			render.Err("feat '" + feat + "' is not in plan '" + doc.Slug + "'")
			return 1
		}
		target = f
	} else {
		f, outcome, err := plan.NextFeat(r, doc, false)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
		if outcome != plan.SeqFeat {
			render.Err("no next feat to brief (plan complete)")
			return 1
		}
		target = f
	}
	// --refresh runs the pass against the workspace itself rather than a worktree:
	// there is no run in flight to have cut one, and the tree a human is looking at
	// is the tree they are asking about. Diagnostics go to stderr so the brief on
	// stdout stays a clean artifact to pipe or diff.
	if refresh {
		if err := plan.RemovePack(r, doc.Slug, target.Slug); err != nil {
			render.Err(err.Error())
			return 1
		}
		hook := plan.EnrichHook(enrichModel)
		if hook == nil {
			render.Err("--refresh needs a model: pass --enrich-model")
			return 1
		}
		if plan.EnsurePack(r, doc, target, r, hook, func(format string, a ...any) {
			render.Warn(strings.TrimSpace(fmt.Sprintf(format, a...)))
		}) == nil {
			render.Warn("the context pass produced nothing for " + target.Slug + "; briefing from the plan alone")
		}
	}
	out, err := plan.FeatBrief(r, doc, target)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	fmt.Print(out)
	return 0
}

func planGenerate(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("plan generate", flag.ContinueOnError)
	var root string
	var force, requireApproved bool
	addRoot(fs, &root)
	addForce(fs, &force)
	fs.BoolVar(&requireApproved, "require-approved", false, "Fail unless the plan is approved and drift-free (used by `plan run`).")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 2 {
		render.Err("usage: " + prog() + " plan generate SLUG FEAT")
		return 1
	}
	slug, feat := positionals[0], positionals[1]
	r, doc, code := resolvePlan(root, slug)
	if code != 0 {
		return code
	}
	if err := workspace.SafeName(feat, "feat"); err != nil {
		render.Err(err.Error())
		return 1
	}
	if _, ok := doc.Feat(feat); !ok {
		render.Err("feat '" + feat + "' is not in plan '" + slug + "'")
		return 1
	}
	approved, drift, _ := plan.IsApproved(r, slug)
	if !approved || drift {
		if requireApproved {
			render.Err("plan '" + slug + "' is not approved or has drifted; approve before generating")
			return 1
		}
		render.Warn("plan '" + slug + "' is not approved; generating anyway (human use)")
	}

	specDir := filepath.Join(paths.Specs(r), feat)
	if !pathExists(specDir) {
		if err := SpecInit(templates, SpecInitOptions{Root: r, Feature: feat}); err != nil {
			render.Err(err.Error())
			return 1
		}
	} else {
		render.Info("spec '" + feat + "' already exists; seeding missing artifacts and provenance")
	}
	seeded, err := seedSpecFromPlan(r, slug, feat, force)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	for _, s := range seeded {
		render.OK("seeded specs/" + feat + "/" + s + " (review before approving)")
	}
	render.OK("generated spec for feat '" + feat + "' (provenance: plan=" + slug + ")")
	return 0
}

// seedSpecFromPlan records plan provenance on the spec and copies any pre-authored
// seed artifacts (requirements/design/tasks) from docs/plans/<slug>/seeds/<feat>/
// into the spec, marking each seeded phase generated (not approved — a human/
// runner still reviews). It returns the artifact basenames it seeded.
func seedSpecFromPlan(root, slug, feat string, force bool) ([]string, error) {
	specDir := filepath.Join(paths.Specs(root), feat)
	data, err := loadSpecJSON(specDir)
	if err != nil {
		return nil, err
	}
	data.Plan = slug
	seedDir := filepath.Join(plan.Dir(root, slug), "seeds", feat)
	var seeded []string
	for _, art := range workspace.SpecPhases {
		seedFile := filepath.Join(seedDir, art+".md")
		if !pathExists(seedFile) {
			continue
		}
		target := filepath.Join(specDir, art+".md")
		if pathExists(target) && !force {
			continue
		}
		content, rerr := os.ReadFile(seedFile)
		if rerr != nil {
			return nil, rerr
		}
		if err := workspace.WriteFile(target, string(content), true); err != nil {
			return nil, err
		}
		a := data.Approvals[art]
		a.Generated = true
		a.Approved = false
		a.ContentHash = ""
		if data.Approvals == nil {
			data.Approvals = map[string]ApprovalFlag{}
		}
		data.Approvals[art] = a
		data.Phase = phaseFor(art)
		data.ReadyForImplementation = false
		seeded = append(seeded, art+".md")
	}
	if err := saveSpecJSON(specDir, data); err != nil {
		return nil, err
	}
	return seeded, nil
}

// printPlanStatus renders the human table: plan-level flags, then one line per
// feat with its derived state and (where meaningful) task progress.
func printPlanStatus(st plan.PlanStatus) {
	fmt.Println(render.Bold("plan: " + st.Slug))
	approved := render.Yellow("draft")
	if st.Approved {
		approved = render.Green("approved")
	}
	fmt.Println("approval: " + approved)
	if st.Drift {
		fmt.Println("drift:    " + render.Red("plan.md/seeds changed since approval — re-approve before running"))
	}
	if len(st.Feats) == 0 {
		render.Info("no feats defined")
		return
	}
	maxName := len("feat")
	for _, f := range st.Feats {
		if len(f.Slug) > maxName {
			maxName = len(f.Slug)
		}
	}
	// States are printed plain (no ANSI) so the fixed-width columns stay aligned
	// regardless of terminal color support.
	fmt.Printf("  %-3s %-*s  %-13s  %-10s  %s\n", "#", maxName, "feat", "state", "milestone", "progress")
	for _, f := range st.Feats {
		progress := ""
		if f.TasksTotal > 0 {
			progress = fmt.Sprintf("%d/%d tasks", f.TasksChecked, f.TasksTotal)
		}
		fmt.Printf("  %-3s %-*s  %-13s  %-10s  %s\n", f.Num, maxName, f.Slug, f.State, f.Milestone, progress)
	}
}

// planCost reports what a run spent, read back out of sessions.jsonl.
//
// The file has recorded every attempt's cost since R9.2 and nothing read it back,
// so the only way to see a run's spend was to parse JSONL by hand. Exit code stays
// 0 whatever the numbers say — this reports, it does not judge.
func planCost(args []string) int {
	fs := flag.NewFlagSet("plan cost", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan cost SLUG [--json]")
		return 1
	}
	r, doc, code := resolvePlan(root, positionals[0])
	if code != 0 {
		return code
	}
	rep := plan.BuildCostReport(r, doc.Slug)
	if jsonOut {
		return emitJSON(rep)
	}
	printCostReport(rep)
	return 0
}

func printCostReport(rep plan.CostReport) {
	fmt.Printf("plan: %s\n", rep.Plan)
	if rep.Totals.Attempts == 0 {
		render.Info("no sessions recorded yet — this plan has not been run")
		return
	}
	fmt.Printf("  %-28s %8s %8s %6s %10s %12s\n", "feat", "attempts", "settled", "gated", "cost", "tokens")
	for _, f := range rep.Feats {
		fmt.Printf("  %-28s %8d %8d %6d %10s %12s%s\n",
			f.Feat, f.Attempts, f.Settled, f.Gated, money(f.CostUSD), thousands(f.Tokens.Total()), deliveredMark(f.Delivered))
	}
	fmt.Printf("  %-28s %8d %8d %6d %10s %12s\n",
		"TOTAL", rep.Totals.Attempts, rep.Totals.Settled, rep.Totals.Gated,
		money(rep.Totals.CostUSD), thousands(rep.Totals.Tokens.Total()))

	t := rep.Totals.Tokens
	fmt.Printf("\n  tokens: %s fresh input · %s output · %s cache read · %s cache write\n",
		thousands(t.Input), thousands(t.Output), thousands(t.CacheRead), thousands(t.CacheCreation))

	if len(rep.ByModel) > 0 {
		fmt.Printf("\n  by model\n")
		for _, m := range rep.ByModel {
			fmt.Printf("    %-34s %12s %10s\n", m.Model, thousands(m.Tokens.Total()), money(m.CostUSD))
		}
		// The two figures come from different fields of the same event: the
		// envelope's own `usage` block, and the CLI's per-model aggregation. A gap
		// between them is work the top-level figure did not count, which on a plan
		// run means the sub-agents. Reporting both is the honest move — asserting
		// which one is right would be a guess.
		if mt := rep.ModelTotal().Total(); mt != rep.Totals.Tokens.Total() {
			fmt.Printf("    %-34s %12s\n", "(per-model total)", thousands(mt))
			render.Warn(fmt.Sprintf("the per-model breakdown and the session totals disagree by %s tokens; "+
				"the session total counts the orchestrating chain, the breakdown counts every model that billed",
				thousands(abs(mt-rep.Totals.Tokens.Total()))))
		}
	}

	if n := len(rep.Unmeasured); n > 0 {
		render.Warn(fmt.Sprintf("%d attempt(s) were opened and never settled: %s", n, strings.Join(rep.Unmeasured, ", ")))
		fmt.Println("  Those sessions ran and spent money; the run was interrupted before their cost")
		fmt.Println("  could be recorded, so every figure above understates the real total.")
	}
	if rep.Totals.Gated > 0 {
		fmt.Printf("\n  %d of %d settled session(s) were paid for and handed back (a refused `done`).\n",
			rep.Totals.Gated, rep.Totals.Settled)
	}
}

// planVerify proves — or fails to prove — that each feat was delivered, by
// cross-checking the ledger, the on-disk artifacts, git, and the worktrees.
// Findings exit 2, the convention every lint/validate command in this CLI follows.
func planVerify(args []string) int {
	fs := flag.NewFlagSet("plan verify", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan verify SLUG [--json]")
		return 1
	}
	r, doc, code := resolvePlan(root, positionals[0])
	if code != 0 {
		return code
	}
	rep := plan.Verify(r, doc)
	if jsonOut {
		if !rep.OK {
			_ = emitJSON(rep)
			return 2
		}
		return emitJSON(rep)
	}
	printVerifyReport(rep)
	if !rep.OK {
		return 2
	}
	return 0
}

func printVerifyReport(rep plan.VerifyReport) {
	fmt.Printf("plan: %s\n", rep.Plan)
	if rep.Git {
		fmt.Printf("merges checked against: %s\n", rep.Branch)
	} else {
		render.Warn("not a git repository (or detached HEAD) — merge evidence cannot be checked")
	}
	fmt.Printf("  %-28s %-18s %-7s %-10s %-7s %s\n", "feat", "state", "ledger", "artifacts", "merged", "worktree")
	for _, f := range rep.Feats {
		fmt.Printf("  %-28s %-18s %-7s %-10s %-7s %s\n",
			f.Feat, f.State, yesNo(f.LedgerDone), yesNo(f.Artifacts), mergedCell(rep.Git, f.Merged, f.MergeCommit), liveCell(f.LiveWorktree))
	}
	var findings int
	for _, f := range rep.Feats {
		for _, msg := range f.Findings {
			findings++
			render.Err(msg)
		}
	}
	if findings == 0 {
		render.OK("every feat's records agree")
		return
	}
	fmt.Printf("\n%d finding(s): the records disagree about what was delivered.\n", findings)
}

func deliveredMark(done bool) string {
	if done {
		return "  ✓"
	}
	return ""
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func mergedCell(git, merged bool, commit string) string {
	if !git {
		return "—"
	}
	if merged {
		return commit
	}
	return "no"
}

func liveCell(live bool) string {
	if live {
		return "live"
	}
	return "—"
}

func money(usd float64) string { return fmt.Sprintf("$%.2f", usd) }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// thousands groups an integer with thin separators so a seven-digit token count is
// readable at a glance, which is the only reason these numbers are printed at all.
func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// planEntryDoc is the lean CLAUDE.md each feat's worktree gets for the duration of
// the run. A template that will not render is not worth failing a run over: the
// sessions fall back to the repository's own file, which is more expensive to read
// and still correct enough to work from.
func planEntryDoc(templates embed.FS) string {
	doc, err := templater.PlanEntry(templates)
	if err != nil {
		render.Warn("could not load the plan-session CLAUDE.md; sessions will read the repository's own: " + err.Error())
		return ""
	}
	return doc
}
