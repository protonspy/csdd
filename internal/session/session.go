// Package session builds a read-only view of a csdd workspace for the web
// dashboard. It reads specs/ and .claude/ off disk and never mutates anything —
// the CLI remains the only sanctioned author of artifacts. session imports only
// paths, workspace, and validator (never cli), so the dependency graph stays
// acyclic: cli → web → session.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/validator"
)

// specJSON is a read-only mirror of cli.SpecJSON (internal/cli/spec.go). It is
// duplicated here so session never imports the write-path cli package; keep the
// JSON tags in sync with the canonical struct (a fixture test asserts both
// unmarshal identically).
type specJSON struct {
	FeatureName            string                  `json:"feature_name"`
	Language               string                  `json:"language"`
	Phase                  string                  `json:"phase"`
	DevelopmentFlow        string                  `json:"development_flow,omitempty"`
	Approvals              map[string]approvalFlag `json:"approvals"`
	ReadyForImplementation bool                    `json:"ready_for_implementation"`
	CreatedAt              string                  `json:"created_at"`
}

type approvalFlag struct {
	Generated bool `json:"generated"`
	Approved  bool `json:"approved"`
}

// phaseKeys are the three human-approved spec phases, in order.
var phaseKeys = []string{"requirements", "design", "tasks"}

// Overview is the whole-workspace summary the dashboard renders.
type Overview struct {
	Root     string     `json:"root"`
	Specs    []SpecCard `json:"specs"`
	Steering []Artifact `json:"steering"`
	Skills   []Artifact `json:"skills"`
	Agents   []Artifact `json:"agents"`
	MCP      []Artifact `json:"mcp"`
	Hooks    []Artifact `json:"hooks"`
	Commands []Artifact `json:"commands"`
}

// Artifact is a non-spec workspace item (steering file, skill, agent, …). Path
// is workspace-relative (slash-separated) so the file viewer can open it. The
// metadata fields are filled from frontmatter for agents/skills/steering (so the
// dashboard can list them meaningfully) and left empty for other kinds.
type Artifact struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"` // agents, skills, steering(auto)
	Tools       string `json:"tools,omitempty"`       // agents
	Model       string `json:"model,omitempty"`       // agents, skills (optional override)
	Effort      string `json:"effort,omitempty"`      // agents, skills (optional override)
	Inclusion   string `json:"inclusion,omitempty"`   // steering: always|fileMatch|manual|auto
}

// Approval mirrors a single phase's generation/approval state for the UI.
type Approval struct {
	Generated bool `json:"generated"`
	Approved  bool `json:"approved"`
}

// SpecCard is the per-spec summary used in the sidebar and the overview.
type SpecCard struct {
	Feature   string `json:"feature"`
	Phase     string `json:"phase"`
	Language  string `json:"language"`
	CreatedAt string `json:"createdAt"`
	Ready     bool   `json:"ready"`
	Readable  bool   `json:"readable"`
	// DevelopmentFlow mirrors spec.json's development_flow (unit|tdd|tdd-e2e).
	// Empty for legacy specs (treated as tdd by the dashboard). The frontend gates
	// its TDD-only displays on this, so a unit-flow spec does not show inert
	// RED/GREEN counters that never move.
	DevelopmentFlow string              `json:"developmentFlow,omitempty"`
	Approvals       map[string]Approval `json:"approvals"`
	Artifacts       []string            `json:"artifacts"`
	Tasks           TaskStats           `json:"tasks"`
	Issues          int                 `json:"issues"`
}

// SpecDetail is the full per-spec view: the card plus the grouped task tree and
// the validator's issue list.
type SpecDetail struct {
	SpecCard
	Phases    []TaskPhase       `json:"phases"`
	IssueList []ValidationIssue `json:"issueList"`
	Report    *SpecReport       `json:"report"` // nil when no test-report.json exists
}

// ValidationIssue is a JSON-friendly mirror of validator.Issue.
type ValidationIssue struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Msg  string `json:"msg"`
}

// LoadOverview reads the workspace at root and returns its read-only summary.
// It never errors: a missing specs/ or .claude/ simply yields empty sections,
// and a malformed spec.json yields an unreadable card rather than a panic.
func LoadOverview(root string) Overview {
	ov := Overview{Root: root}
	specsDir := paths.Specs(root)
	if entries, err := os.ReadDir(specsDir); err == nil {
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		for _, n := range names {
			ov.Specs = append(ov.Specs, buildCard(specsDir, n))
		}
		// Newest spec first, by spec.json created_at (an RFC3339 UTC string, so a
		// lexical compare is chronological). Ties — and unreadable specs, whose
		// CreatedAt is empty — fall back to feature name ascending for a stable
		// order.
		sort.Slice(ov.Specs, func(i, j int) bool {
			a, b := ov.Specs[i], ov.Specs[j]
			if a.CreatedAt != b.CreatedAt {
				return a.CreatedAt > b.CreatedAt
			}
			return a.Feature < b.Feature
		})
	}
	ov.Steering = listSteering(paths.Steering(root), root)
	ov.Skills = listSkills(paths.Skills(root), root)
	ov.Agents = listAgents(paths.Agents(root), root)
	ov.Hooks = listFiles(paths.Hooks(root), root, "")
	ov.Commands = listFiles(paths.Commands(root), root, ".md")
	ov.MCP = listMCP(root)
	// Always emit arrays (never nil) so the JSON encodes [] not null. The
	// dashboard treats these as always-present arrays and crashes on null.
	ov.Specs = orEmpty(ov.Specs)
	ov.Steering = orEmpty(ov.Steering)
	ov.Skills = orEmpty(ov.Skills)
	ov.Agents = orEmpty(ov.Agents)
	ov.MCP = orEmpty(ov.MCP)
	ov.Hooks = orEmpty(ov.Hooks)
	ov.Commands = orEmpty(ov.Commands)
	return ov
}

// LoadSpecDetail returns the full view of one spec. The feature name is
// validated to prevent path traversal before it is joined onto the specs dir.
func LoadSpecDetail(root, feature string) (SpecDetail, error) {
	if err := safeFeature(feature); err != nil {
		return SpecDetail{}, err
	}
	specsDir := paths.Specs(root)
	specDir := filepath.Join(specsDir, feature)
	if fi, err := os.Stat(specDir); err != nil || !fi.IsDir() {
		return SpecDetail{}, fmt.Errorf("spec not found: %s", feature)
	}
	card, phases, issues, _ := buildCardFull(specsDir, feature)
	d := SpecDetail{SpecCard: card, Phases: phases}
	for _, is := range issues {
		d.IssueList = append(d.IssueList, ValidationIssue{File: is.File, Line: is.Line, Msg: is.Msg})
	}
	d.Phases = orEmpty(d.Phases)
	d.IssueList = orEmpty(d.IssueList)
	d.Report = loadSpecReport(specDir)
	return d, nil
}

func buildCard(specsDir, feature string) SpecCard {
	card, _, _, _ := buildCardFull(specsDir, feature)
	return card
}

// buildCardFull assembles a spec card and also returns the parsed task phases,
// validation issues, and spec.json it computed along the way. LoadSpecDetail
// reuses these instead of re-reading tasks.md, re-loading spec.json, and
// re-running the validator a second time — the dashboard polls this path every
// ~800ms, and on WSL's /mnt/c each avoided file read crosses the 9p boundary.
func buildCardFull(specsDir, feature string) (SpecCard, []TaskPhase, []validator.Issue, specJSON) {
	specDir := filepath.Join(specsDir, feature)
	card := SpecCard{Feature: feature, Approvals: map[string]Approval{}}
	s, ok := loadSpec(specDir)
	card.Readable = ok
	if ok {
		card.Phase = s.Phase
		card.Language = s.Language
		card.DevelopmentFlow = s.DevelopmentFlow
		card.CreatedAt = s.CreatedAt
		card.Ready = s.ReadyForImplementation
		for _, k := range phaseKeys {
			a := s.Approvals[k]
			card.Approvals[k] = Approval{Generated: a.Generated, Approved: a.Approved}
		}
	} else {
		card.Phase = "(unreadable)"
		for _, k := range phaseKeys {
			card.Approvals[k] = Approval{}
		}
	}
	if entries, err := os.ReadDir(specDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				card.Artifacts = append(card.Artifacts, e.Name())
			}
		}
		sort.Strings(card.Artifacts)
	}
	var phases []TaskPhase
	if data, err := os.ReadFile(filepath.Join(specDir, "tasks.md")); err == nil {
		var stats TaskStats
		phases, stats = ParseTasks(string(data))
		card.Tasks = stats
	}
	issues := validationIssues(specDir, s)
	card.Issues = len(issues)
	card.Artifacts = orEmpty(card.Artifacts)
	return card, phases, issues, s
}

// validationIssues runs the mechanical validator scoped to the spec's furthest
// generated phase, mirroring cli.validationScope. Returns nil when nothing has
// been generated yet (nothing to validate).
func validationIssues(specDir string, s specJSON) []validator.Issue {
	bugfix := fileExists(filepath.Join(specDir, "bugfix.md"))
	var phase validator.Phase
	switch {
	case strings.HasPrefix(s.Phase, "bugfix") || bugfix:
		phase = validator.PhaseAll
	case s.Approvals["tasks"].Generated:
		phase = validator.PhaseTasks
	case s.Approvals["design"].Generated:
		phase = validator.PhaseDesign
	case s.Approvals["requirements"].Generated:
		phase = validator.PhaseRequirements
	default:
		return nil
	}
	return validator.ValidateSpec(specDir, phase)
}

func loadSpec(specDir string) (specJSON, bool) {
	data, err := os.ReadFile(filepath.Join(specDir, "spec.json"))
	if err != nil {
		return specJSON{}, false
	}
	var s specJSON
	if err := json.Unmarshal(data, &s); err != nil {
		return specJSON{}, false
	}
	if s.Approvals == nil {
		s.Approvals = map[string]approvalFlag{}
	}
	return s, true
}

func listFiles(dir, root, ext string) []Artifact {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Artifact
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext != "" && !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		abs := filepath.Join(dir, e.Name())
		out = append(out, Artifact{Name: e.Name(), Path: rel(root, abs)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func listSkillDirs(dir, root string) []Artifact {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Artifact
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		abs := filepath.Join(dir, e.Name(), "SKILL.md")
		out = append(out, Artifact{Name: e.Name(), Path: rel(root, abs)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// readFrontmatter parses the YAML frontmatter of a file, tolerating a missing or
// malformed file by returning an empty Frontmatter.
func readFrontmatter(abs string) frontmatter.Frontmatter {
	data, err := os.ReadFile(abs)
	if err != nil {
		return frontmatter.Frontmatter{}
	}
	return frontmatter.Parse(string(data))
}

// listAgents lists .claude/agents/*.md with their description and tools.
func listAgents(dir, root string) []Artifact {
	out := listFiles(dir, root, ".md")
	for i := range out {
		fm := readFrontmatter(filepath.Join(dir, out[i].Name))
		out[i].Description = fm.AsString("description", "")
		out[i].Tools = fm.AsString("tools", "")
		out[i].Model = fm.AsString("model", "")
		out[i].Effort = fm.AsString("effort", "")
	}
	return out
}

// listSteering lists .claude/steering/*.md with their inclusion mode and (for
// auto inclusion) description.
func listSteering(dir, root string) []Artifact {
	out := listFiles(dir, root, ".md")
	for i := range out {
		fm := readFrontmatter(filepath.Join(dir, out[i].Name))
		out[i].Inclusion = fm.AsString("inclusion", "")
		out[i].Description = fm.AsString("description", "")
	}
	return out
}

// listSkills lists .claude/skills/<name>/ with each SKILL.md description.
func listSkills(dir, root string) []Artifact {
	out := listSkillDirs(dir, root)
	for i := range out {
		fm := readFrontmatter(filepath.Join(dir, out[i].Name, "SKILL.md"))
		out[i].Description = fm.AsString("description", "")
		out[i].Model = fm.AsString("model", "")
		out[i].Effort = fm.AsString("effort", "")
	}
	return out
}

func listMCP(root string) []Artifact {
	data, err := os.ReadFile(paths.MCP(root))
	if err != nil {
		return nil
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	relPath := rel(root, paths.MCP(root))
	var out []Artifact
	for name := range cfg.MCPServers {
		out = append(out, Artifact{Name: name, Path: relPath})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// safeFeature rejects feature names that could escape the specs directory.
func safeFeature(feature string) error {
	if feature == "" || strings.Contains(feature, "..") || strings.ContainsAny(feature, `/\`) {
		return fmt.Errorf("invalid feature name: %q", feature)
	}
	return nil
}

// rel returns abs relative to root in slash form, falling back to abs on error.
func rel(root, abs string) string {
	r, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(r)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// orEmpty returns a non-nil slice so JSON encodes [] instead of null. The web
// dashboard consumes these as always-present arrays (.length/.map), so a null
// would crash the client.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
