package cli

import (
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
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/workspace"
)

// runPlan dispatches `csdd plan <action> ...`. `run` is answered by a refusal:
// the autonomous loop it drove is discontinued (see planRun).
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
		return planRun(rest)
	case "cost":
		return planCost(rest)
	case "verify":
		return planVerify(rest)
	default:
		render.Err("unknown plan action: " + action)
		return 1
	}
}

// planRun answers `csdd plan run`, which is discontinued.
//
// The command drove an autonomous loop: one headless `claude -p` session per feat,
// a verdict gate, an attempt budget, a worktree each. Measured against the same
// work done interactively it cost an order of magnitude more and frequently
// delivered nothing — a session was rebuilt from zero per feat AND per attempt, so
// every discovery was re-paid at orchestrator prices and a `done` the gate refused
// threw a whole paid session away. That is the design, not a defect in it.
//
// It is a refusal rather than an "unknown action" because a user who types this has
// a plan in front of them and needs to know where the work moved, not that they
// mistyped. The runner itself (internal/plan) is untouched and still tested: the
// idea is on standby, and reviving it should be a revert rather than a rewrite.
func planRun(args []string) int {
	// The whole message goes to stderr: this command produces no stdout artifact,
	// and a refusal's guidance belongs on the same stream as the refusal.
	render.Err("`" + prog() + " plan run` is discontinued — a plan is delivered in your own session now.\n" +
		"    " + prog() + " plan next <slug>              # which feat is ready\n" +
		"    " + prog() + " plan brief <slug> --feat F    # that feat's mission\n" +
		"  Work it with the `plan-dev` skill, approving each spec phase yourself.")
	return 1
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
			// longer binds the current plan.md, which is what `plan next` reports as drift.
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
		render.Warn("drift: plan.md/seeds changed since approval — re-approve before working from it")
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

// normalizeEnrichModel maps `none` — how a human spells "no context pass" — onto
// the empty model every enrichment caller already reads as disabled. It is shared
// so every enrichment caller spells it the same way: for a
// while only the runner honored it, and `plan brief --enrich-model none` spawned a
// session for a model literally named "none".
func normalizeEnrichModel(model string) string {
	if strings.EqualFold(strings.TrimSpace(model), "none") {
		return ""
	}
	return model
}

func planBrief(args []string) int {
	fs := flag.NewFlagSet("plan brief", flag.ContinueOnError)
	var root, feat, enrichModel string
	var refresh bool
	addRoot(fs, &root)
	fs.StringVar(&feat, "feat", "", "Feat to brief (default: the sequencer's next feat).")
	fs.BoolVar(&refresh, "refresh", false, "Discard this feat's stored context pack and run the pass again, even when the stored one is still current. Without it the pass runs only when there is nothing usable on disk — a stored pack whose plan row has not changed is reused as-is.")
	fs.StringVar(&enrichModel, "enrich-model", "sonnet", "Model the context pass runs on (claude --model). `none` turns the pass off and briefs from the plan alone.")
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
	// The discovered half is not an extra. Without it the brief is the plan restated
	// — the feat row a human already wrote — and the session rediscovers the tree at
	// orchestrator prices, which is the whole cost the pass exists to avoid. So the
	// pass runs BY DEFAULT here. `--enrich-model none` is how a caller that must not
	// spawn a model — CI, a scripted diff — opts out.
	//
	// A pack already on disk is kept, and that is a stronger rule here than in the
	// runner: it is reused even when the feat's row has moved on since it was
	// written. Regenerating costs a model call, briefing is something a human does
	// repeatedly while editing a plan, and re-spending on every one of those edits is
	// not a default anybody would choose. A stale pack costs a line on stderr and
	// `--refresh` is the way to replace it.
	//
	// The pass runs against the workspace itself rather than a worktree: there is no
	// run in flight to have cut one, and the tree a human is looking at is the tree
	// they are asking about. Diagnostics go to stderr so the brief on stdout stays a
	// clean artifact to pipe or diff.
	hook := plan.EnrichHook(normalizeEnrichModel(enrichModel))
	if refresh {
		if hook == nil {
			render.Err("--refresh needs a model: pass --enrich-model (or drop --refresh)")
			return 1
		}
		if err := plan.RemovePack(r, doc.Slug, target.Slug); err != nil {
			render.Err(err.Error())
			return 1
		}
	}
	pack, err := plan.LoadPack(r, doc.Slug, target.Slug)
	if err != nil {
		render.Warn(err.Error())
	}
	switch {
	case pack != nil:
		if pack.Key != plan.PackKey(doc, target) {
			render.Warn("the stored context pack for " + target.Slug + " predates the current plan row; `--refresh` regenerates it")
		}
	case hook != nil:
		if plan.EnsurePack(r, doc, target, r, hook, func(format string, a ...any) {
			render.Warn(strings.TrimSpace(fmt.Sprintf(format, a...)))
		}) == nil {
			render.Warn("the context pass produced nothing for " + target.Slug + "; briefing from the plan alone")
		}
	default:
		// Nothing on disk and the pass switched off: say so, rather than printing a
		// brief that is silently missing half of itself.
		render.Warn("no stored context pack for " + target.Slug + " and the context pass is off (--enrich-model none); briefing from the plan alone")
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
	fs.BoolVar(&requireApproved, "require-approved", false, "Fail unless the plan is approved and drift-free.")
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
