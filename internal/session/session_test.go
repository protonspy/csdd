package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specMirrorFixture exercises the session specJSON mirror's development_flow
// field. The cli package asserts the same field independently (spec_flow_test.go);
// the two mirror structs are kept tag-for-tag in sync by review discipline.
const specMirrorFixture = `{"feature_name":"x","language":"en","phase":"initialized","development_flow":"tdd-e2e","approvals":{},"ready_for_implementation":false,"created_at":"2026-01-01T00:00:00Z"}`

// TestSpecJSONMirrorParsesFlow asserts the read-only mirror parses the
// development_flow field (kept in sync with cli.SpecJSON).
func TestSpecJSONMirrorParsesFlow(t *testing.T) {
	var s specJSON
	if err := json.Unmarshal([]byte(specMirrorFixture), &s); err != nil {
		t.Fatal(err)
	}
	if s.DevelopmentFlow != "tdd-e2e" {
		t.Errorf("mirror DevelopmentFlow = %q, want tdd-e2e", s.DevelopmentFlow)
	}
}

// writeWorkspace materializes a temp workspace from a path→content map and
// returns the root. Paths are slash-separated and relative to the root.
func writeWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const specJSONTasksGenerated = `{
  "feature_name": "photo-albums",
  "language": "en",
  "phase": "tasks-generated",
  "approvals": {
    "requirements": {"generated": true, "approved": true},
    "design": {"generated": true, "approved": true},
    "tasks": {"generated": true, "approved": false}
  },
  "ready_for_implementation": false,
  "created_at": "2025-01-01T00:00:00Z"
}`

func TestOverview(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"specs/photo-albums/spec.json":       specJSONTasksGenerated,
		"specs/photo-albums/requirements.md": "# Requirements\n",
		"specs/photo-albums/tasks.md":        sampleTasks,
		"specs/zebra/spec.json":              `{"feature_name":"zebra","phase":"requirements-generated","approvals":{}}`,
		".claude/steering/product.md":        "# Product\n",
		".claude/agents/code-reviewer.md":    "# Agent\n",
		".claude/skills/tdd/SKILL.md":        "# Skill\n",
		".claude/commands/csdd-commit.md":    "# Cmd\n",
		".claude/hooks/format.sh":            "#!/bin/sh\n",
		".mcp.json":                          `{"mcpServers":{"linear":{},"filesystem":{}}}`,
	})

	ov := LoadOverview(root)
	if len(ov.Specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(ov.Specs))
	}
	// Ordered newest-created first: photo-albums (dated) before zebra (no created_at).
	pa := ov.Specs[0]
	if pa.Feature != "photo-albums" {
		t.Fatalf("first spec = %q, want photo-albums", pa.Feature)
	}
	if !pa.Readable || pa.Phase != "tasks-generated" {
		t.Errorf("photo-albums readable/phase = %v/%q", pa.Readable, pa.Phase)
	}
	if !pa.Approvals["requirements"].Approved || pa.Approvals["tasks"].Approved {
		t.Errorf("photo-albums approvals = %+v", pa.Approvals)
	}
	if pa.Tasks.Total != 5 || pa.Tasks.Done != 3 || pa.Tasks.Pct != 60 {
		t.Errorf("photo-albums task stats = %+v, want total 5 done 3 pct 60", pa.Tasks)
	}

	if len(ov.Steering) != 1 || ov.Steering[0].Name != "product.md" {
		t.Errorf("steering = %+v", ov.Steering)
	}
	if len(ov.Skills) != 1 || ov.Skills[0].Name != "tdd" || ov.Skills[0].Path != ".claude/skills/tdd/SKILL.md" {
		t.Errorf("skills = %+v", ov.Skills)
	}
	if len(ov.Agents) != 1 || len(ov.Commands) != 1 || len(ov.Hooks) != 1 {
		t.Errorf("agents/commands/hooks = %d/%d/%d, want 1/1/1", len(ov.Agents), len(ov.Commands), len(ov.Hooks))
	}
	if len(ov.MCP) != 2 {
		t.Errorf("mcp servers = %d, want 2", len(ov.MCP))
	}
}

func TestOverviewArtifactMetadata(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		".claude/agents/code-reviewer.md": "---\nname: code-reviewer\ndescription: Reviews diffs for bugs.\ntools: Read, Grep\n---\n# Agent\n",
		".claude/skills/tdd/SKILL.md":     "---\nname: tdd\ndescription: Red-green-refactor one task.\n---\n# Skill\n",
		".claude/steering/api.md":         "---\ninclusion: fileMatch\nfileMatchPattern: [\"**/*.go\"]\n---\n# API\n",
		".claude/steering/obs.md":         "---\ninclusion: auto\nname: obs\ndescription: Use when adding instrumentation.\n---\n# Obs\n",
	})
	ov := LoadOverview(root)

	if len(ov.Agents) != 1 || ov.Agents[0].Description != "Reviews diffs for bugs." || ov.Agents[0].Tools != "Read, Grep" {
		t.Errorf("agent meta = %+v", ov.Agents)
	}
	if len(ov.Skills) != 1 || ov.Skills[0].Description != "Red-green-refactor one task." {
		t.Errorf("skill meta = %+v", ov.Skills)
	}
	// Steering sorted: api.md then obs.md.
	if len(ov.Steering) != 2 {
		t.Fatalf("steering = %+v", ov.Steering)
	}
	if ov.Steering[0].Inclusion != "fileMatch" {
		t.Errorf("api steering inclusion = %q, want fileMatch", ov.Steering[0].Inclusion)
	}
	if ov.Steering[1].Inclusion != "auto" || ov.Steering[1].Description != "Use when adding instrumentation." {
		t.Errorf("obs steering = %+v", ov.Steering[1])
	}
}

func TestOverviewArtifactModelEffort(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		".claude/agents/heavy.md":      "---\nname: heavy\ndescription: d\ntools: Read\nmodel: opus\neffort: high\n---\n# Agent\n",
		".claude/agents/plain.md":      "---\nname: plain\ndescription: d\ntools: Read\n---\n# Agent\n",
		".claude/skills/deep/SKILL.md": "---\nname: deep\ndescription: d\nmodel: sonnet\neffort: max\n---\n# Skill\n",
		".claude/skills/lite/SKILL.md": "---\nname: lite\ndescription: d\n---\n# Skill\n",
	})
	ov := LoadOverview(root)

	// Agents sorted: heavy then plain.
	if len(ov.Agents) != 2 {
		t.Fatalf("agents = %+v", ov.Agents)
	}
	if ov.Agents[0].Model != "opus" || ov.Agents[0].Effort != "high" {
		t.Errorf("heavy agent model/effort = %q/%q, want opus/high", ov.Agents[0].Model, ov.Agents[0].Effort)
	}
	if ov.Agents[1].Model != "" || ov.Agents[1].Effort != "" {
		t.Errorf("plain agent should omit model/effort, got %q/%q", ov.Agents[1].Model, ov.Agents[1].Effort)
	}
	// Skills sorted: deep then lite.
	if len(ov.Skills) != 2 {
		t.Fatalf("skills = %+v", ov.Skills)
	}
	if ov.Skills[0].Model != "sonnet" || ov.Skills[0].Effort != "max" {
		t.Errorf("deep skill model/effort = %q/%q, want sonnet/max", ov.Skills[0].Model, ov.Skills[0].Effort)
	}
	if ov.Skills[1].Model != "" || ov.Skills[1].Effort != "" {
		t.Errorf("lite skill should omit model/effort, got %q/%q", ov.Skills[1].Model, ov.Skills[1].Effort)
	}
}

func TestOverviewEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	ov := LoadOverview(root) // no specs/, no .claude/
	if ov.Root != root {
		t.Errorf("root = %q, want %q", ov.Root, root)
	}
	if len(ov.Specs) != 0 || len(ov.Steering) != 0 {
		t.Errorf("expected empty sections, got specs=%d steering=%d", len(ov.Specs), len(ov.Steering))
	}
}

func TestOverviewEmptyMarshalsArrays(t *testing.T) {
	// Empty sections must serialize as [] not null — the dashboard reads these
	// as arrays (.length/.map) and crashes on null. Regression for that bug.
	ov := LoadOverview(t.TempDir())
	b, err := json.Marshal(ov)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{"specs", "steering", "skills", "agents", "mcp", "hooks", "commands"} {
		if strings.Contains(s, `"`+key+`":null`) {
			t.Errorf("overview JSON encodes %q as null (want []): %s", key, s)
		}
	}
}

func TestTreeMarshalsArrays(t *testing.T) {
	b, _ := json.Marshal(Tree(t.TempDir()))
	s := string(b)
	if strings.Contains(s, "null") {
		t.Errorf("empty Tree should encode [] not null: %s", s)
	}
}

func TestOverviewMalformedSpecJSON(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"specs/broken/spec.json": "{ this is not json",
	})
	ov := LoadOverview(root)
	if len(ov.Specs) != 1 {
		t.Fatalf("specs = %d, want 1", len(ov.Specs))
	}
	if ov.Specs[0].Readable {
		t.Errorf("broken spec should be marked unreadable")
	}
	if ov.Specs[0].Phase != "(unreadable)" {
		t.Errorf("phase = %q, want (unreadable)", ov.Specs[0].Phase)
	}
}

func TestOverviewSpecsSortedByCreatedAtDesc(t *testing.T) {
	// Three readable specs whose creation dates differ from alphabetical order,
	// so the assertion distinguishes created-at ordering from name ordering.
	spec := func(name, createdAt string) string {
		return `{"feature_name":"` + name + `","phase":"requirements-generated","approvals":{},"created_at":"` + createdAt + `"}`
	}
	root := writeWorkspace(t, map[string]string{
		"specs/alpha/spec.json": spec("alpha", "2025-01-01T00:00:00Z"),
		"specs/beta/spec.json":  spec("beta", "2025-06-01T00:00:00Z"),
		"specs/gamma/spec.json": spec("gamma", "2025-03-01T00:00:00Z"),
	})

	ov := LoadOverview(root)

	var got []string
	for _, s := range ov.Specs {
		got = append(got, s.Feature)
	}
	// Newest created_at first: beta (Jun) → gamma (Mar) → alpha (Jan).
	want := []string{"beta", "gamma", "alpha"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("spec order = %v, want %v", got, want)
	}
}

func TestOverviewSpecsCreatedAtTieBreaksByName(t *testing.T) {
	// Equal created_at (and the empty-created_at unreadable case) must fall back
	// to a deterministic name-ascending order.
	root := writeWorkspace(t, map[string]string{
		"specs/bravo/spec.json": `{"feature_name":"bravo","approvals":{},"created_at":"2025-01-01T00:00:00Z"}`,
		"specs/alpha/spec.json": `{"feature_name":"alpha","approvals":{},"created_at":"2025-01-01T00:00:00Z"}`,
		"specs/zulu/spec.json":  `{ broken`,
		"specs/yank/spec.json":  `{ broken`,
	})

	ov := LoadOverview(root)

	var got []string
	for _, s := range ov.Specs {
		got = append(got, s.Feature)
	}
	// Dated pair (tie) name-ascending first, then empty-created_at pair name-ascending.
	want := []string{"alpha", "bravo", "yank", "zulu"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("spec order = %v, want %v", got, want)
	}
}

func TestSpecDetailValidation(t *testing.T) {
	// A leaf task missing _Requirements:_ must surface as a validator issue.
	root := writeWorkspace(t, map[string]string{
		"specs/f/spec.json":       `{"phase":"tasks-generated","approvals":{"requirements":{"generated":true,"approved":true},"design":{"generated":true,"approved":true},"tasks":{"generated":true}}}`,
		"specs/f/requirements.md": "# Requirements\n",
		"specs/f/design.md":       "## Architecture Pattern & Boundary Map\n## File Structure Plan\n",
		"specs/f/tasks.md":        "## Phase 1: Foundation\n\n- [ ] 1. do it\n",
	})
	d, err := LoadSpecDetail(root, "f")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.IssueList) == 0 {
		t.Errorf("expected at least one validation issue for the leaf task missing _Requirements:_")
	}
	if d.Feature != "f" {
		t.Errorf("feature = %q, want f", d.Feature)
	}
	if len(d.Phases) != 1 {
		t.Errorf("phases = %d, want 1", len(d.Phases))
	}
}

func TestSpecDetailRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"..", "../etc", "a/b", `a\b`, ""} {
		if _, err := LoadSpecDetail(root, bad); err == nil {
			t.Errorf("SpecDetail(%q) should have errored", bad)
		}
	}
}

func TestSpecDetailNotFound(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadSpecDetail(root, "ghost"); err == nil {
		t.Errorf("SpecDetail for missing spec should error")
	}
}
