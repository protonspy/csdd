package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	// Sorted alphabetically: photo-albums then zebra.
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
