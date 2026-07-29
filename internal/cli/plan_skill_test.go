package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRDSkillAndCommandsInstalled(t *testing.T) {
	dir := freshWorkspace(t)

	// The PRD skill and the /prd + wrapper commands ship with `csdd init`.
	mustExist := []string{
		".claude/skills/prd/SKILL.md",
		".claude/commands/prd.md",
		".claude/commands/csdd-plan-validate.md",
		".claude/commands/csdd-plan-status.md",
	}
	for _, p := range mustExist {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("init did not install %s: %v", p, err)
		}
	}

	// The installed PRD skill passes the skill validator (required sections, etc.).
	if code, _, errOut := run(t, "skill", "validate", "prd", "--root", dir); code != 0 {
		t.Errorf("prd skill failed validation (code=%d): %s", code, errOut)
	}
}

func TestPlanModeMomentsInClaudeMD(t *testing.T) {
	dir := freshWorkspace(t)
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"Plan mode", "/prd", "Never edit an approved plan", "Revise"} {
		if !strings.Contains(text, want) {
			t.Errorf("CLAUDE.md managed section missing plan-mode moment %q", want)
		}
	}
}
