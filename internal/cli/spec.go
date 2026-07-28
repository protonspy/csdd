package cli

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/manifest"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/session"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/validator"
	"github.com/protonspy/csdd/internal/workspace"
)

// specSchemaVersion is the current spec.json schema. It is written on every save
// so a future csdd can detect and migrate older layouts instead of guessing.
const specSchemaVersion = 1

// SpecJSON mirrors the schema produced by the Python reference implementation.
type SpecJSON struct {
	SchemaVersion          int                     `json:"schema_version,omitempty"`
	FeatureName            string                  `json:"feature_name"`
	Language               string                  `json:"language"`
	Phase                  string                  `json:"phase"`
	DevelopmentFlow        string                  `json:"development_flow,omitempty"`
	Approvals              map[string]ApprovalFlag `json:"approvals"`
	ReadyForImplementation bool                    `json:"ready_for_implementation"`
	CreatedAt              string                  `json:"created_at"`

	// Plan records the plan slug this spec was generated from (`csdd plan
	// generate`), the provenance the graph turns into a `plans` edge. Absent for
	// specs created directly via `csdd spec init`.
	Plan string `json:"plan,omitempty"`

	// extra preserves any keys a newer csdd wrote that this binary does not model,
	// so round-tripping spec.json through an older binary never silently drops
	// them. Unexported ⇒ ignored by encoding/json; merged back in saveSpecJSON.
	extra map[string]json.RawMessage
}

// knownSpecKeys are the JSON object keys SpecJSON models directly; anything else
// on disk is captured into SpecJSON.extra and round-tripped untouched.
var knownSpecKeys = []string{
	"schema_version", "feature_name", "language", "phase", "development_flow",
	"approvals", "ready_for_implementation", "created_at", "plan",
}

// defaultDevelopmentFlow is the flow assumed when none is selected and steering
// declares no default. `unit` is the default: implementation first, with a unit
// test covering the behavior in the same task — the lightest posture that still
// keeps every behavior tested. `tdd` (RED→GREEN) is REQUIRED for money, auth,
// tenancy, and anything irreversible; set it explicitly (steering default or
// --flow) where those guarantees matter.
const defaultDevelopmentFlow = "unit"

// developmentFlows is the closed set of selectable development flows:
//   - unit:    implementation first, then a unit test covering it in the same task (the default)
//   - tdd:     test-first RED→GREEN — required for money/auth/tenancy/irreversible surfaces
//   - tdd-e2e: TDD plus end-to-end coverage of golden and error flows
var developmentFlows = []string{"unit", "tdd", "tdd-e2e"}

// validDevelopmentFlow reports whether f is one of the selectable flows.
func validDevelopmentFlow(f string) bool {
	for _, v := range developmentFlows {
		if v == f {
			return true
		}
	}
	return false
}

// effectiveFlow coerces a stored (possibly empty, e.g. legacy spec.json) flow to
// the value callers should act on. Absent ⇒ defaultDevelopmentFlow.
func effectiveFlow(f string) string {
	if f == "" {
		return defaultDevelopmentFlow
	}
	return f
}

// resolveDefaultFlow returns the workspace default development flow declared in
// steering frontmatter (key default_development_flow). Steering files are scanned
// in name order; the first valid declared value wins. Absent or invalid ⇒
// defaultDevelopmentFlow (an invalid value is separately flagged by the steering
// validator, so init stays robust rather than writing a bad flow).
func resolveDefaultFlow(root string) string {
	dir := paths.Steering(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return defaultDevelopmentFlow
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		fm := frontmatter.Parse(string(data))
		if v := fm.AsString("default_development_flow", ""); validDevelopmentFlow(v) {
			return v
		}
	}
	return defaultDevelopmentFlow
}

// ApprovalFlag tracks generation + approval state for a phase. ContentHash binds
// an approval to the exact artifact content that was approved: if the phase's
// requirements.md / design.md / tasks.md is edited by hand after approval, the
// stored hash no longer matches and the drift is reported — closing the loophole
// where a spec-driven gate could be bypassed by editing the file post-approval.
type ApprovalFlag struct {
	Generated   bool   `json:"generated"`
	Approved    bool   `json:"approved"`
	ContentHash string `json:"content_hash,omitempty"`
}

func runSpec(args []string, templates embed.FS) int {
	action, rest, err := parseAction("spec", args)
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
	case "test-report":
		return specTestReport(rest)
	case "delete":
		return specDelete(rest)
	default:
		render.Err("unknown spec action: " + action)
		return 1
	}
}

// SpecInitOptions is the headless input shared by the CLI action and the TUI
// wizard. Flow "" resolves the steering default, then defaultDevelopmentFlow.
type SpecInitOptions struct {
	Root     string
	Feature  string
	Language string
	Flow     string
}

// SpecInit creates specs/<feature>/spec.json with the resolved development flow.
// It is the single source of truth for spec creation; the CLI action and the TUI
// wizard both fill SpecInitOptions and call it. An explicit invalid Flow is
// rejected before any write (the caller maps the error to exit 1).
func SpecInit(templates embed.FS, opts SpecInitOptions) error {
	if opts.Flow != "" && !validDevelopmentFlow(opts.Flow) {
		return fmt.Errorf("invalid development flow %q: must be one of %s", opts.Flow, strings.Join(developmentFlows, "|"))
	}
	r, err := workspace.Resolve(opts.Root)
	if err != nil {
		return err
	}
	if _, err := workspace.SpecsDir(r); err != nil {
		return err
	}
	if err := workspace.KebabCheck(opts.Feature, "feature"); err != nil {
		return err
	}
	target := filepath.Join(paths.Specs(r), opts.Feature)
	if pathExists(target) {
		return errors.New("spec already exists: " + workspace.Relative(r, target))
	}
	flow := opts.Flow
	if flow == "" {
		flow = resolveDefaultFlow(r)
	}
	language := opts.Language
	if language == "" {
		language = "en"
	}
	if err := mkdirAll(target); err != nil {
		return err
	}
	content, err := templater.Render(templates, "templates/spec/spec.json.tmpl", map[string]string{
		"Feature":         opts.Feature,
		"Language":        language,
		"DevelopmentFlow": flow,
		"CreatedAt":       time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	if err != nil {
		return err
	}
	if err := workspace.WriteFile(filepath.Join(target, "spec.json"), content, false); err != nil {
		return err
	}
	render.OK("created " + workspace.Relative(r, target) + "/ (flow: " + flow + ")")
	render.Info(fmt.Sprintf("next: `%s spec generate <feature> --artifact requirements`", prog()))
	return nil
}

func specInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("spec init", flag.ContinueOnError)
	var opts SpecInitOptions
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Language, "language", "en", "Spec language (default: en).")
	fs.StringVar(&opts.Flow, "flow", "", "Development flow: unit|tdd|tdd-e2e (default: steering default, else unit).")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " spec init FEATURE")
		return 1
	}
	opts.Feature = positionals[0]
	if err := SpecInit(templates, opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

func specList(args []string) int {
	fs := flag.NewFlagSet("spec list", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
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
	var jsonRows []specSummaryJSON
	maxName := len("feature")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := loadSpecJSON(filepath.Join(base, e.Name()))
		if err != nil {
			// A dir with no spec.json isn't a spec — skip it quietly. But a spec.json
			// that fails to parse is corruption the user must see, not hide.
			if !os.IsNotExist(err) {
				render.Warn("skipping spec '" + e.Name() + "': " + err.Error())
			}
			continue
		}
		ready := "no"
		if data.ReadyForImplementation {
			ready = "yes"
		}
		approved := []string{}
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
		jsonRows = append(jsonRows, specSummaryJSON{
			Feature:  e.Name(),
			Phase:    data.Phase,
			Approved: approved,
			Ready:    data.ReadyForImplementation,
		})
		if len(e.Name()) > maxName {
			maxName = len(e.Name())
		}
	}
	if jsonOut {
		sort.Slice(jsonRows, func(i, j int) bool { return jsonRows[i].Feature < jsonRows[j].Feature })
		return emitJSON(jsonRows)
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
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " spec show FEATURE [--json]")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	feature := positionals[0]
	if err := workspace.SafeName(feature, "feature"); err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir := filepath.Join(paths.Specs(r), feature)
	if !pathExists(sdir) {
		render.Err("spec not found: " + feature)
		return 1
	}
	data, err := loadSpecJSON(sdir)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if jsonOut {
		return emitJSON(data)
	}
	fmt.Println(render.Bold("feature: " + data.FeatureName))
	fmt.Println("phase:    " + data.Phase)
	fmt.Println("language: " + data.Language)
	fmt.Println("flow:     " + effectiveFlow(data.DevelopmentFlow))
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
		render.Err("usage: " + prog() + " spec generate FEATURE --artifact {requirements|design|tasks|research|bugfix}")
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
	if err := workspace.SafeName(opts.Feature, "feature"); err != nil {
		return err
	}
	sdir := filepath.Join(paths.Specs(r), opts.Feature)
	if !pathExists(sdir) {
		return fmt.Errorf("spec not found: %s. Run `%s spec init %s` first", opts.Feature, prog(), opts.Feature)
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
		if !phaseApprovedAndCurrent(sdir, data, prev) && !opts.Force {
			return fmt.Errorf("phase gate: '%s' must be approved (and unchanged since) before generating '%s'. Use --force only for explicitly fast-track / Quick Plan flows", prev, opts.Artifact)
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
	// tasks.md is shaped by the spec's development_flow: the RED/GREEN pair under
	// tdd/tdd-e2e, implementation-then-cover under unit. Scaffolding the wrong shape
	// would hand the author a tasks.md the validator immediately rejects, so pick
	// the template the declared flow will accept.
	if opts.Artifact == "tasks" && effectiveFlow(data.DevelopmentFlow) == "unit" {
		pair[1] = "templates/spec/tasks-unit.md.tmpl"
	}
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
		a.ContentHash = "" // regeneration invalidates any prior approval binding
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
		render.Err("usage: " + prog() + " spec approve FEATURE --phase {requirements|design|tasks}")
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
	if err := workspace.SafeName(opts.Feature, "feature"); err != nil {
		return err
	}
	sdir := filepath.Join(paths.Specs(r), opts.Feature)
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
		// The generated flag is bookkeeping from `spec generate`; what an approval
		// certifies is the artifact content. A phase authored directly — by a human
		// or a session delivering a plan feat — is just as approvable, so only a missing
		// artifact blocks here.
		if !phaseAuthored(sdir, data, opts.Phase) {
			return fmt.Errorf("cannot approve '%s': %s not found — generate or author it first", opts.Phase, phaseArtifact(opts.Phase))
		}
		state.Generated = true
	}
	if prev := previousPhase(opts.Phase); prev != "" && !phaseApprovedAndCurrent(sdir, data, prev) {
		if !opts.Force {
			return fmt.Errorf("phase gate: '%s' must be approved (and unchanged since) before approving '%s'", prev, opts.Phase)
		}
		render.Warn("approval forced despite missing/stale prior approval for '" + prev + "'")
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
	if h, err := phaseContentHash(sdir, opts.Phase); err == nil {
		state.ContentHash = h
	}
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

// phaseAuthored reports whether a phase has an artifact to validate or approve:
// either `spec generate` marked it generated, or the artifact file itself exists
// (authored directly by a human or by a session working from a plan). Approval and validation
// certify content, not the route it was produced by.
func phaseAuthored(specDir string, data SpecJSON, phase string) bool {
	if data.Approvals[phase].Generated {
		return true
	}
	name := phaseArtifact(phase)
	return name != "" && pathExists(filepath.Join(specDir, name))
}

// phaseArtifact maps an approvable phase to the artifact whose content its
// approval certifies.
func phaseArtifact(phase string) string {
	switch phase {
	case "requirements":
		return "requirements.md"
	case "design":
		return "design.md"
	case "tasks":
		return "tasks.md"
	}
	return ""
}

// phaseContentHash returns the line-ending-normalized content hash of a phase's
// artifact. Normalization means a CRLF re-checkout never looks like drift.
func phaseContentHash(specDir, phase string) (string, error) {
	name := phaseArtifact(phase)
	if name == "" {
		return "", fmt.Errorf("no artifact for phase %q", phase)
	}
	b, err := os.ReadFile(filepath.Join(specDir, name))
	if err != nil {
		return "", err
	}
	return manifest.Hash(string(b)), nil
}

// phaseApprovedAndCurrent reports whether a phase is approved AND its artifact
// has not been edited since (no drift). A drifted approval must not satisfy a
// phase gate — otherwise a post-approval hand-edit would let the next phase
// proceed on a stale checkpoint. Approvals with no stored hash (written by an
// older csdd) are trusted, since drift can't be detected for them.
func phaseApprovedAndCurrent(specDir string, s SpecJSON, phase string) bool {
	a, ok := s.Approvals[phase]
	if !ok || !a.Approved {
		return false
	}
	if a.ContentHash == "" {
		return true
	}
	h, err := phaseContentHash(specDir, phase)
	return err == nil && h == a.ContentHash
}

// approvalDriftIssues reports any phase whose artifact was edited after approval,
// so a hand-edit can't silently ride on a stale approval. Approvals recorded by
// older csdd versions (no stored hash) are skipped — no false positives.
func approvalDriftIssues(specDir string, s SpecJSON) []validator.Issue {
	var out []validator.Issue
	for _, phase := range workspace.SpecPhases {
		a, ok := s.Approvals[phase]
		if !ok || !a.Approved || a.ContentHash == "" {
			continue
		}
		h, err := phaseContentHash(specDir, phase)
		if err != nil {
			continue
		}
		if h != a.ContentHash {
			out = append(out, validator.Issue{
				File: phaseArtifact(phase),
				Msg: fmt.Sprintf("edited after approval — the '%s' approval no longer certifies this content; re-approve (`%s spec approve %s --phase %s`) or regenerate",
					phase, prog(), s.FeatureName, phase),
			})
		}
	}
	return out
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
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " spec validate FEATURE [--json]")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	feature := positionals[0]
	if err := workspace.SafeName(feature, "feature"); err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir := filepath.Join(paths.Specs(r), feature)
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
	issues = append(issues, approvalDriftIssues(sdir, data)...)
	if jsonOut {
		// Exit code still encodes the result (2 = issues) so an agent can branch on
		// $? without parsing, while stdout carries the details.
		emitJSON(validationJSON{Target: feature, OK: len(issues) == 0, Issues: issuesToJSON(issues)})
		if len(issues) > 0 {
			return 2
		}
		return 0
	}
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
	if phaseAuthored(specDir, data, "tasks") {
		return validator.PhaseTasks, nil
	}
	if phaseAuthored(specDir, data, "design") {
		return validator.PhaseDesign, nil
	}
	if phaseAuthored(specDir, data, "requirements") {
		return validator.PhaseRequirements, nil
	}
	return validator.PhaseAll, []validator.Issue{{
		File: "spec.json",
		Msg:  fmt.Sprintf("no generated artifacts to validate; run `%s spec generate <feature> --artifact requirements` first", prog()),
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
		render.Err("usage: " + prog() + " spec delete FEATURE --force")
		return 1
	}
	feature := positionals[0]
	if err := workspace.SafeName(feature, "feature"); err != nil {
		render.Err(err.Error())
		return 1
	}
	if !force {
		render.Err("refusing to delete spec '" + feature + "' without --force")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir := filepath.Join(paths.Specs(r), feature)
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

// specTestReport records structured TDD test/coverage metrics for a spec into
// specs/<feature>/test-report.json (the convention the dashboard reads). It
// parses a JUnit/lcov/Cobertura report when given, or takes explicit counts.
func specTestReport(args []string) int {
	fs := flag.NewFlagSet("spec test-report", flag.ContinueOnError)
	var root, junit, coverage, command, lang, path, cmd, task string
	var run, fast bool
	var total, passed, failed, skipped, covered, lines int
	var pct float64
	addRoot(fs, &root)
	fs.StringVar(&junit, "junit", "", "JUnit XML report to parse for test counts.")
	fs.StringVar(&coverage, "coverage", "", "Coverage report to parse (lcov/Cobertura/JaCoCo/Go coverprofile).")
	fs.StringVar(&command, "command", "", "Test command, recorded for display.")
	fs.StringVar(&lang, "lang", "", "Language for --run defaults and coverage discovery: python | typescript | java | go | rust.")
	fs.StringVar(&path, "path", "", "Directory to run in / auto-discover JUnit+coverage reports under (e.g. tests/). Defaults to the workspace root.")
	fs.BoolVar(&run, "run", false, "Execute the tests (per-language default, or --cmd) before parsing the reports they produce.")
	fs.StringVar(&cmd, "cmd", "", "Test command to execute with --run (overrides the per-language default).")
	fs.BoolVar(&fast, "fast", false, "With --run and no --cmd, use the language's coverage-free command (the Tier-2 task-exit gate). Coverage belongs to the Tier-3 feat-exit run, so omit --fast there.")
	fs.StringVar(&task, "task", "", "Task ID this run is evidence for. Files the result under that task and preserves every other task's result, so concurrent implementers don't overwrite each other.")
	fs.IntVar(&total, "total", -1, "Explicit test total (when no --junit).")
	fs.IntVar(&passed, "passed", -1, "Explicit tests passed.")
	fs.IntVar(&failed, "failed", -1, "Explicit tests failed.")
	fs.IntVar(&skipped, "skipped", -1, "Explicit tests skipped.")
	fs.Float64Var(&pct, "pct", -1, "Explicit coverage percent (when no --coverage).")
	fs.IntVar(&covered, "covered", -1, "Explicit covered lines.")
	fs.IntVar(&lines, "lines", -1, "Explicit total lines.")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " spec test-report FEATURE [--run [--fast] [--cmd \"...\"]] [--task ID] [--lang LANG] [--path DIR] [--junit FILE] [--coverage FILE]")
		return 1
	}
	feature := positionals[0]
	if err := workspace.SafeName(feature, "feature"); err != nil {
		render.Err(err.Error())
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir := filepath.Join(paths.Specs(r), feature)
	if !pathExists(sdir) {
		render.Err("spec not found: " + feature)
		return 1
	}

	rep := session.SpecReport{
		Feature:   feature,
		UpdatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		Command:   command,
	}

	// Parsed summaries (from explicit reports, a --run execution, or discovery).
	var ts *session.TestSummary
	var cov *session.Coverage

	if junit != "" {
		ts, err = session.ParseJUnit(r, junit)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
	}
	if coverage != "" {
		cov, err = session.ParseCoverageFile(r, coverage)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
	}

	// Run the tests and/or auto-discover the reports they produce, scoped by
	// --path and filtered by --lang.
	ranExit, ran := 0, false
	if run || lang != "" || path != "" {
		if _, ok := session.CoverageFormatsForLang(lang); !ok {
			render.Err("unsupported --lang " + strconv.Quote(lang) + "; supported: " + strings.Join(session.SupportedLangs(), ", "))
			return 1
		}
		dir := r
		if path != "" {
			if filepath.IsAbs(path) {
				dir = path
			} else {
				dir = filepath.Join(r, path)
			}
			if !pathExists(dir) {
				render.Err("--path not found: " + path)
				return 1
			}
		}
		if run {
			toRun := cmd
			if toRun == "" {
				// --fast picks the coverage-free variant (Tier 2). Without it the
				// coverage-bearing command stays the evidence default, so no
				// existing invocation changes behavior (R8.2, R8.3).
				pick := session.DefaultTestCommand
				if fast {
					pick = session.FastTestCommand
				}
				dc, ok := pick(lang)
				if !ok {
					render.Err("with --run, pass --cmd \"...\" or a --lang that has a default (python|typescript|java|go|rust)")
					return 1
				}
				toRun = dc
			} else {
				if ok, markers := session.ValidateTestCommandForLang(lang, cmd); !ok {
					// The override doesn't look like this language's test tooling —
					// flag it (and record it) but don't block a deliberate custom run.
					msg := fmt.Sprintf("--cmd does not look like a %s test command (expected one of: %s)", lang, strings.Join(markers, ", "))
					render.Warn(msg)
					rep.Attentions = append(rep.Attentions, msg)
				}
				// The marker check above proves only that the right TOOL ran, never
				// what it was asked to run. A command that skips or selects tests
				// still produces a report that asserts green — with authority it has
				// not earned — so name the narrowing flags on the artifact itself
				// (R11.1). An attention, not a rejection: legitimate exclusions
				// exist, but they must be visible.
				// Selectors, not just flags: a bare path (`pytest tests/unit`)
				// narrows the run exactly as hard and carries no flag, so it used
				// to write a green whole-suite claim unchallenged (R11.1).
				if sel := session.DetectTestScopeSelectors(lang, cmd); len(sel) > 0 {
					msg := fmt.Sprintf("--cmd narrows the test run (%s): this evidence does not cover the whole suite",
						strings.Join(sel, ", "))
					render.Warn(msg)
					rep.Attentions = append(rep.Attentions, msg)
				}
			}
			rep.Command = toRun
			render.Info("running tests: " + toRun + "  (in " + workspace.Relative(r, dir) + ")")
			ranExit = runTestCommand(dir, toRun)
			ran = true
			if ranExit == 0 {
				render.OK("test command finished (exit 0)")
			} else {
				render.Warn(fmt.Sprintf("test command exited with code %d — recording reports anyway", ranExit))
			}
		}
		dts, dcov := session.DiscoverReport(r, dir, lang)
		if ts == nil {
			ts = dts
		}
		if cov == nil {
			cov = dcov
		}
	}

	// Fold tests: parsed summary first, else explicit counts.
	if ts != nil {
		rep.Tests = &session.SpecTestCounts{Total: ts.Total, Passed: ts.Passed, Failed: ts.Failed, Skipped: ts.Skipped}
		rep.Attentions = append(rep.Attentions, testAttentions(ts)...)
	} else if total >= 0 || passed >= 0 || failed >= 0 || skipped >= 0 {
		rep.Tests = &session.SpecTestCounts{Total: nz(total), Passed: nz(passed), Failed: nz(failed), Skipped: nz(skipped)}
	}

	// Fold coverage: parsed summary first, else explicit counts.
	if cov != nil {
		rep.Coverage = &session.SpecCovSummary{Pct: cov.Pct, Covered: cov.Covered, Lines: cov.Lines}
	} else if pct >= 0 || covered >= 0 || lines >= 0 {
		c := &session.SpecCovSummary{Covered: nz(covered), Lines: nz(lines)}
		switch {
		case pct >= 0:
			c.Pct = pct
		case c.Lines > 0:
			c.Pct = float64(c.Covered) * 100 / float64(c.Lines)
		}
		rep.Coverage = c
	}

	if ran && ranExit != 0 {
		rep.Attentions = append([]string{fmt.Sprintf("test command exited with code %d", ranExit)}, rep.Attentions...)
	}

	if rep.Tests == nil && rep.Coverage == nil {
		render.Err("nothing to record: pass --run, --junit/--coverage, --lang/--path, or explicit --total/--passed/--pct flags")
		return 1
	}

	// testPaths is the spec's own folder — deterministic over (root, feature) and
	// independent of where the test/coverage artifacts were found.
	specRel := filepath.ToSlash(workspace.Relative(r, sdir))
	if !strings.HasSuffix(specRel, "/") {
		specRel += "/"
	}
	rep.TestPaths = []string{specRel}

	// Merge rather than overwrite: several implementers work one spec at once, and
	// a wholesale rebuild means only the last one's evidence survives (R6.1, R6.2).
	if err := session.WriteSpecReport(sdir, task, rep); err != nil {
		render.Err(err.Error())
		return 1
	}
	target := filepath.Join(sdir, session.SpecReportFile)
	render.OK("wrote " + workspace.Relative(r, target))
	if rep.Tests != nil {
		render.Info(fmt.Sprintf("tests: %d passed · %d failed · %d skipped (of %d)", rep.Tests.Passed, rep.Tests.Failed, rep.Tests.Skipped, rep.Tests.Total))
	}
	if rep.Coverage != nil {
		render.Info(fmt.Sprintf("coverage: %.1f%% (%d/%d lines)", rep.Coverage.Pct, rep.Coverage.Covered, rep.Coverage.Lines))
	}
	for _, a := range rep.Attentions {
		render.Warn(a)
	}
	// A failed test run is reflected in the exit code so callers (CI, MCP) can gate.
	if ran && ranExit != 0 {
		return 1
	}
	return 0
}

// runTestCommand executes a test command string in dir via the platform shell,
// streaming its output, and returns its exit code (-1 if it could not start).
func runTestCommand(dir, command string) int {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/c", command)
	} else {
		c = exec.Command("sh", "-c", command)
	}
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		return -1
	}
	return 0
}

// testAttentions derives the key unit-test signals worth flagging: failing tests
// (capped) and a skipped-count note.
func testAttentions(ts *session.TestSummary) []string {
	var out []string
	const cap = 5
	for i, f := range ts.Failures {
		if i >= cap {
			out = append(out, fmt.Sprintf("… +%d more failing test(s)", len(ts.Failures)-cap))
			break
		}
		label := strings.TrimSpace(f.Suite + " / " + f.Name)
		out = append(out, "FAIL "+strings.TrimPrefix(label, "/ "))
	}
	if ts.Skipped > 0 {
		out = append(out, fmt.Sprintf("%d test(s) skipped", ts.Skipped))
	}
	return out
}

// nz clamps an "unset" (-1) flag value to 0.
func nz(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func loadSpecJSON(specDir string) (SpecJSON, error) {
	var s SpecJSON
	path := filepath.Join(specDir, "spec.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parse %s: %w", path, err)
	}
	// Capture keys this binary does not model so save() can round-trip them.
	var all map[string]json.RawMessage
	if json.Unmarshal(data, &all) == nil {
		for _, k := range knownSpecKeys {
			delete(all, k)
		}
		if len(all) > 0 {
			s.extra = all
		}
	}
	if s.Approvals == nil {
		s.Approvals = map[string]ApprovalFlag{}
	}
	return s, nil
}

func saveSpecJSON(specDir string, s SpecJSON) error {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = specSchemaVersion
	}
	var b []byte
	var err error
	if len(s.extra) == 0 {
		// Fast path: keep the clean, field-ordered layout with no diff churn.
		b, err = json.MarshalIndent(s, "", "  ")
	} else {
		// Forward-compat path: merge unknown keys back so an older binary never
		// drops fields a newer csdd wrote.
		known, mErr := json.Marshal(s)
		if mErr != nil {
			return mErr
		}
		merged := map[string]json.RawMessage{}
		if err := json.Unmarshal(known, &merged); err != nil {
			return err
		}
		for k, v := range s.extra {
			if _, ok := merged[k]; !ok {
				merged[k] = v
			}
		}
		b, err = json.MarshalIndent(merged, "", "  ")
	}
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return workspace.AtomicWrite(filepath.Join(specDir, "spec.json"), b, 0o644)
}
