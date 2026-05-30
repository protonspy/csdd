package cmd

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/livelo/csdd/internal/render"
	"github.com/livelo/csdd/internal/templater"
	"github.com/livelo/csdd/internal/validator"
	"github.com/livelo/csdd/internal/workspace"
)

// SpecJSON mirrors the schema produced by the Python reference implementation.
type SpecJSON struct {
	FeatureName            string                  `json:"feature_name"`
	Language               string                  `json:"language"`
	Phase                  string                  `json:"phase"`
	Approvals              map[string]ApprovalFlag `json:"approvals"`
	ReadyForImplementation bool                    `json:"ready_for_implementation"`
	CreatedAt              string                  `json:"created_at"`
}

// ApprovalFlag tracks generation + approval state for a phase.
type ApprovalFlag struct {
	Generated bool `json:"generated"`
	Approved  bool `json:"approved"`
}

func runSpec(args []string, templates embed.FS) int {
	action, rest, err := parseAction("spec", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	switch action {
	case "init":
		return specInit(rest, templates)
	case "list":
		return specList(rest)
	case "show":
		return specShow(rest)
	case "status":
		return specStatus(rest)
	case "generate":
		return specGenerate(rest, templates)
	case "approve":
		return specApprove(rest)
	case "validate":
		return specValidate(rest)
	case "delete":
		return specDelete(rest)
	default:
		render.Err("unknown spec action: " + action)
		return 1
	}
}

func specInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("spec init", flag.ContinueOnError)
	var root, language string
	addRoot(fs, &root)
	fs.StringVar(&language, "language", "en", "Spec language (default: en).")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd spec init FEATURE")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if _, err := workspace.SpecsDir(r); err != nil {
		render.Err(err.Error())
		return 1
	}
	feature := positionals[0]
	if err := workspace.KebabCheck(feature, "feature"); err != nil {
		render.Err(err.Error())
		return 1
	}
	target := filepath.Join(r, ".kiro", "specs", feature)
	if pathExists(target) {
		render.Err("spec already exists: " + workspace.Relative(r, target))
		return 1
	}
	if err := mkdirAll(target); err != nil {
		render.Err(err.Error())
		return 1
	}
	content, err := templater.Render(templates, "templates/spec/spec.json.tmpl", map[string]string{
		"Feature":   feature,
		"Language":  language,
		"CreatedAt": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if err := workspace.WriteFile(filepath.Join(target, "spec.json"), content, false); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("created " + workspace.Relative(r, target) + "/")
	render.Info("next: `csdd spec generate <feature> --artifact requirements`")
	return 0
}

func specList(args []string) int {
	fs := flag.NewFlagSet("spec list", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	_, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	base, err := workspace.SpecsDir(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	type row struct{ feature, phase, approved, ready string }
	var rows []row
	maxName := len("feature")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := loadSpecJSON(filepath.Join(base, e.Name()))
		if err != nil {
			continue
		}
		ready := "no"
		if data.ReadyForImplementation {
			ready = "yes"
		}
		var approved []string
		for k, v := range data.Approvals {
			if v.Approved {
				approved = append(approved, k)
			}
		}
		sort.Strings(approved)
		appStr := "—"
		if len(approved) > 0 {
			appStr = strings.Join(approved, ",")
		}
		rows = append(rows, row{e.Name(), data.Phase, appStr, ready})
		if len(e.Name()) > maxName {
			maxName = len(e.Name())
		}
	}
	if len(rows) == 0 {
		render.Info("no specs found")
		return 0
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].feature < rows[j].feature })
	fmt.Printf("  %-*s  %-22s  %-28s  ready\n", maxName, "feature", "phase", "approved")
	for _, r := range rows {
		fmt.Printf("  %-*s  %-22s  %-28s  %s\n", maxName, r.feature, r.phase, r.approved, r.ready)
	}
	return 0
}

func specShow(args []string) int {
	fs := flag.NewFlagSet("spec show", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd spec show FEATURE")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	feature := positionals[0]
	sdir := filepath.Join(r, ".kiro", "specs", feature)
	if !pathExists(sdir) {
		render.Err("spec not found: " + feature)
		return 1
	}
	data, err := loadSpecJSON(sdir)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	fmt.Println(render.Bold("feature: " + data.FeatureName))
	fmt.Println("phase:    " + data.Phase)
	fmt.Println("language: " + data.Language)
	fmt.Println("created:  " + data.CreatedAt)
	fmt.Printf("ready:    %v\n", data.ReadyForImplementation)
	fmt.Println("approvals:")
	keys := []string{"requirements", "design", "tasks"}
	for _, k := range keys {
		v, ok := data.Approvals[k]
		if !ok {
			fmt.Printf("  %-14s %s\n", k, render.Red("missing"))
			continue
		}
		state := render.Red("missing")
		switch {
		case v.Approved:
			state = render.Green("approved")
		case v.Generated:
			state = render.Yellow("generated")
		}
		fmt.Printf("  %-14s %s\n", k, state)
	}
	fmt.Println("artifacts:")
	entries, _ := os.ReadDir(sdir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Println("  - " + n)
	}
	return 0
}

func specStatus(args []string) int {
	if code := specShow(args); code != 0 {
		return code
	}
	fmt.Println()
	return specValidate(args)
}

// SpecGenerateOptions is the headless equivalent of the CLI flag struct.
// Sharing this struct lets the TUI bypass argument parsing.
type SpecGenerateOptions struct {
	Root     string
	Feature  string
	Artifact string
	Force    bool
}

func specGenerate(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("spec generate", flag.ContinueOnError)
	var opts SpecGenerateOptions
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Artifact, "artifact", "", "requirements|design|tasks|research|bugfix")
	addForce(fs, &opts.Force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 || opts.Artifact == "" {
		render.Err("usage: csdd spec generate FEATURE --artifact {requirements|design|tasks|research|bugfix}")
		return 1
	}
	opts.Feature = positionals[0]
	if err := SpecGenerate(templates, opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// SpecGenerate creates a spec artifact and updates spec.json accordingly.
func SpecGenerate(templates embed.FS, opts SpecGenerateOptions) error {
	if !containsString(workspace.SpecArtifacts, opts.Artifact) {
		return fmt.Errorf("--artifact must be one of %v", workspace.SpecArtifacts)
	}
	r, err := workspace.Resolve(opts.Root)
	if err != nil {
		return err
	}
	sdir := filepath.Join(r, ".kiro", "specs", opts.Feature)
	if !pathExists(sdir) {
		return fmt.Errorf("spec not found: %s. Run `csdd spec init %s` first", opts.Feature, opts.Feature)
	}
	data, err := loadSpecJSON(sdir)
	if err != nil {
		return err
	}
	// Phase gate: design needs requirements approved; tasks needs design approved.
	if opts.Artifact == "design" || opts.Artifact == "tasks" {
		prev := "requirements"
		if opts.Artifact == "tasks" {
			prev = "design"
		}
		if !data.Approvals[prev].Approved && !opts.Force {
			return fmt.Errorf("phase gate: '%s' must be approved before generating '%s'. Use --force only for explicitly fast-track / Quick Plan flows", prev, opts.Artifact)
		}
	}
	templateMap := map[string][2]string{
		"requirements": {"requirements.md", "templates/spec/requirements.md.tmpl"},
		"design":       {"design.md", "templates/spec/design.md.tmpl"},
		"tasks":        {"tasks.md", "templates/spec/tasks.md.tmpl"},
		"research":     {"research.md", "templates/spec/research.md.tmpl"},
		"bugfix":       {"bugfix.md", "templates/spec/bugfix.md.tmpl"},
	}
	pair := templateMap[opts.Artifact]
	target := filepath.Join(sdir, pair[0])
	if pathExists(target) && !opts.Force {
		return fmt.Errorf("%s already exists. Use --force to overwrite", target)
	}
	content, err := templater.Static(templates, pair[1])
	if err != nil {
		return err
	}
	if err := workspace.WriteFile(target, content, true); err != nil {
		return err
	}
	render.OK("created " + workspace.Relative(r, target))
	// Update spec.json. Only the three gated phases (requirements/design/tasks)
	// advance the phase chain. research and bugfix are auxiliary artifacts: they
	// have no approvals entry and must not clobber requirements/design/tasks
	// progress (e.g. generating research mid-requirements). bugfix is still
	// detected during validation via the presence of bugfix.md.
	tracked := containsString(workspace.SpecPhases, opts.Artifact)
	if a, ok := data.Approvals[opts.Artifact]; ok {
		a.Generated = true
		a.Approved = false
		data.Approvals[opts.Artifact] = a
	}
	if tracked {
		data.Phase = phaseFor(opts.Artifact)
		data.ReadyForImplementation = false
	}
	if err := saveSpecJSON(sdir, data); err != nil {
		return err
	}
	if tracked {
		render.Info("phase set to '" + data.Phase + "'. Human review required before approval.")
	}
	return nil
}

func phaseFor(artifact string) string {
	return artifact + "-generated"
}

// SpecApproveOptions is the headless equivalent of the CLI flag struct.
type SpecApproveOptions struct {
	Root    string
	Feature string
	Phase   string
	Force   bool
}

func specApprove(args []string) int {
	fs := flag.NewFlagSet("spec approve", flag.ContinueOnError)
	var opts SpecApproveOptions
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Phase, "phase", "", "requirements|design|tasks")
	addForce(fs, &opts.Force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 || opts.Phase == "" {
		render.Err("usage: csdd spec approve FEATURE --phase {requirements|design|tasks}")
		return 1
	}
	opts.Feature = positionals[0]
	if err := SpecApprove(opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// SpecApprove validates and marks a phase as human-approved in spec.json.
func SpecApprove(opts SpecApproveOptions) error {
	if !containsString(workspace.SpecPhases, opts.Phase) {
		return fmt.Errorf("--phase must be one of %v", workspace.SpecPhases)
	}
	r, err := workspace.Resolve(opts.Root)
	if err != nil {
		return err
	}
	sdir := filepath.Join(r, ".kiro", "specs", opts.Feature)
	if !pathExists(sdir) {
		return fmt.Errorf("spec not found: %s", opts.Feature)
	}
	data, err := loadSpecJSON(sdir)
	if err != nil {
		return err
	}
	state, ok := data.Approvals[opts.Phase]
	if !ok {
		return fmt.Errorf("approvals[%s] not present in spec.json", opts.Phase)
	}
	if !state.Generated {
		return fmt.Errorf("cannot approve '%s': not generated yet", opts.Phase)
	}
	if prev := previousPhase(opts.Phase); prev != "" && !data.Approvals[prev].Approved {
		if !opts.Force {
			return fmt.Errorf("phase gate: '%s' must be approved before approving '%s'", prev, opts.Phase)
		}
		render.Warn("approval forced despite missing prior approval for '" + prev + "'")
	}
	issues := validator.ValidateSpec(sdir, validator.Phase(opts.Phase))
	if len(issues) > 0 {
		for _, i := range issues {
			render.Err(i.String())
		}
		if !opts.Force {
			return fmt.Errorf("approval blocked by validation issues. Fix them or pass --force")
		}
		render.Warn("approval forced despite validation issues")
	}
	state.Approved = true
	data.Approvals[opts.Phase] = state
	data.Phase = opts.Phase + "-approved"
	ready := true
	for _, p := range workspace.SpecPhases {
		if !data.Approvals[p].Approved {
			ready = false
			break
		}
	}
	data.ReadyForImplementation = ready
	if err := saveSpecJSON(sdir, data); err != nil {
		return err
	}
	render.OK(opts.Feature + ": " + opts.Phase + " approved")
	if ready {
		render.OK(opts.Feature + ": ready_for_implementation = true")
	}
	return nil
}

func previousPhase(phase string) string {
	switch phase {
	case "design":
		return "requirements"
	case "tasks":
		return "design"
	default:
		return ""
	}
}

func specValidate(args []string) int {
	fs := flag.NewFlagSet("spec validate", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd spec validate FEATURE")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	feature := positionals[0]
	sdir := filepath.Join(r, ".kiro", "specs", feature)
	if !pathExists(sdir) {
		render.Err("spec not found: " + feature)
		return 1
	}
	data, err := loadSpecJSON(sdir)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	phase, preflight := validationScope(sdir, data)
	issues := append(preflight, validator.ValidateSpec(sdir, phase)...)
	if len(issues) == 0 {
		render.OK(feature + ": validation passed")
		return 0
	}
	for _, i := range issues {
		render.Err(i.String())
	}
	return 2
}

func validationScope(specDir string, data SpecJSON) (validator.Phase, []validator.Issue) {
	if strings.HasPrefix(data.Phase, "bugfix") || pathExists(filepath.Join(specDir, "bugfix.md")) {
		return validator.PhaseAll, nil
	}
	if data.Approvals["tasks"].Generated {
		return validator.PhaseTasks, nil
	}
	if data.Approvals["design"].Generated {
		return validator.PhaseDesign, nil
	}
	if data.Approvals["requirements"].Generated {
		return validator.PhaseRequirements, nil
	}
	return validator.PhaseAll, []validator.Issue{{
		File: "spec.json",
		Msg:  "no generated artifacts to validate; run `csdd spec generate <feature> --artifact requirements` first",
	}}
}

func specDelete(args []string) int {
	fs := flag.NewFlagSet("spec delete", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd spec delete FEATURE --force")
		return 1
	}
	feature := positionals[0]
	if !force {
		render.Err("refusing to delete spec '" + feature + "' without --force")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir := filepath.Join(r, ".kiro", "specs", feature)
	if !pathExists(sdir) {
		render.Err("spec not found: " + feature)
		return 1
	}
	if err := os.RemoveAll(sdir); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("deleted " + workspace.Relative(r, sdir))
	return 0
}

func loadSpecJSON(specDir string) (SpecJSON, error) {
	var s SpecJSON
	data, err := os.ReadFile(filepath.Join(specDir, "spec.json"))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Approvals == nil {
		s.Approvals = map[string]ApprovalFlag{}
	}
	return s, nil
}

func saveSpecJSON(specDir string, s SpecJSON) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(specDir, "spec.json"), b, 0o644)
}
