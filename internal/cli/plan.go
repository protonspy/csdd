package cli

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/plan"
	"github.com/protonspy/csdd/internal/render"
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
	default:
		render.Err("unknown plan action: " + action)
		return 1
	}
}

func planRun(args []string) int {
	fs := flag.NewFlagSet("plan run", flag.ContinueOnError)
	var root string
	var autonomous, assumeYes bool
	var sessionBudget float64
	var maxIterations, stall, maxRetries, maxRepairs int
	addRoot(fs, &root)
	fs.BoolVar(&assumeYes, "yes", false, "Skip the unverified-sandbox prompt: accept running --dangerously-skip-permissions even when `sandbox doctor` fails.")
	fs.BoolVar(&autonomous, "autonomous", false, "Deprecated no-op: plan run always runs bypass-mode (--dangerously-skip-permissions).")
	fs.Float64Var(&sessionBudget, "session-budget", 0, "Per-session cap in USD (claude --max-budget-usd). Default 0 = no cap; the session runs under the Claude account's own limits.")
	fs.IntVar(&maxIterations, "max-iterations", 100, "Sessions the run may spend; one iteration is one claude session.")
	fs.IntVar(&stall, "stall", 10, "Stop early after this many consecutive sessions without a step advancing.")
	fs.IntVar(&maxRetries, "max-retries", 0, "Deprecated no-op: each iteration is one session, and the next iteration is the retry.")
	fs.IntVar(&maxRepairs, "max-repairs", 0, "Deprecated no-op: the self-correcting loop replaced repair sessions.")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan run SLUG [--yes] [--session-budget N] [--max-iterations N] [--stall N]")
		return 1
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
	sum, err := plan.Run(plan.RunOptions{
		Root:          r,
		Slug:          slug,
		AssumeYes:     assumeYes,
		SessionBudget: sessionBudget,
		MaxIterations: maxIterations,
		Stall:         stall,
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
	var root, feat string
	addRoot(fs, &root)
	fs.StringVar(&feat, "feat", "", "Feat to brief (default: the sequencer's next feat).")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " plan brief SLUG [--feat F]")
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
