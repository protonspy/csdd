package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bareWorkspace inits a workspace with every shipped artifact tree excluded, so
// `csdd copy` has empty destinations to copy into.
func bareWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// hooks are opt-in (absent unless --include hooks), so they need no exclude.
	if code, _, errOut := run(t, "init", "--root", dir,
		"--exclude", "skills,agents,rules,commands,templates,steering"); code != 0 {
		t.Fatalf("init --exclude failed (code=%d): %s", code, errOut)
	}
	return dir
}

func TestCopySkillTree(t *testing.T) {
	dir := bareWorkspace(t)
	code, out, errOut := run(t, "copy", "--root", dir, "skills/dev-architecture")
	if code != 0 {
		t.Fatalf("copy skill failed (code=%d): %s", code, errOut)
	}
	if !strings.Contains(out, "copied skills/dev-architecture") {
		t.Errorf("missing success line:\n%s", out)
	}
	// A skill copies its whole tree, not just SKILL.md.
	for _, f := range []string{
		".claude/skills/dev-architecture/SKILL.md",
		".claude/skills/dev-architecture/assets/adr-template.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("skill tree missing %s: %v", f, err)
		}
	}
}

func TestCopyAgentAndRule(t *testing.T) {
	dir := bareWorkspace(t)
	for _, tc := range []struct{ arg, want string }{
		{"agents/code-reviewer", ".claude/agents/code-reviewer.md"},
		{"rules/ears-format", ".claude/rules/ears-format.md"},
	} {
		if code, _, errOut := run(t, "copy", "--root", dir, tc.arg); code != 0 {
			t.Fatalf("copy %s failed: %s", tc.arg, errOut)
		}
		if _, err := os.Stat(filepath.Join(dir, tc.want)); err != nil {
			t.Errorf("copy %s did not produce %s: %v", tc.arg, tc.want, err)
		}
	}
}

// A bare item name that maps to a sub-tree (a template group) copies the group.
func TestCopyTemplateSubGroup(t *testing.T) {
	dir := bareWorkspace(t)
	if code, _, errOut := run(t, "copy", "--root", dir, "templates/specs"); code != 0 {
		t.Fatalf("copy templates/specs failed: %s", errOut)
	}
	for _, f := range []string{"requirements.md", "design.md", "tasks.md"} {
		if _, err := os.Stat(filepath.Join(dir, ".claude/templates/specs", f)); err != nil {
			t.Errorf("sub-group copy missing %s: %v", f, err)
		}
	}
}

func TestCopyUnknownItemAndKind(t *testing.T) {
	dir := bareWorkspace(t)
	if code, _, errOut := run(t, "copy", "--root", dir, "agents/does-not-exist"); code != 1 ||
		!strings.Contains(errOut, "no agents named") {
		t.Errorf("unknown item should exit 1 with a helpful error; code=%d err=%s", code, errOut)
	}
	if code, _, errOut := run(t, "copy", "--root", dir, "widgets/foo"); code != 1 ||
		!strings.Contains(errOut, "unknown artifact kind") {
		t.Errorf("unknown kind should exit 1; code=%d err=%s", code, errOut)
	}
}

func TestCopyRefusesClobberWithoutForce(t *testing.T) {
	dir := bareWorkspace(t)
	if code, _, errOut := run(t, "copy", "--root", dir, "agents/implementer"); code != 0 {
		t.Fatalf("first copy failed: %s", errOut)
	}
	if code, _, errOut := run(t, "copy", "--root", dir, "agents/implementer"); code != 1 ||
		!strings.Contains(errOut, "already exists") {
		t.Errorf("second copy should refuse without --force; code=%d err=%s", code, errOut)
	}
	if code, _, errOut := run(t, "copy", "--root", dir, "--force", "agents/implementer"); code != 0 {
		t.Errorf("copy --force should overwrite; code=%d err=%s", code, errOut)
	}
}

func TestCopyListsAvailable(t *testing.T) {
	dir := bareWorkspace(t)
	// Bare `copy` lists everything copyable.
	if code, out, _ := run(t, "copy", "--root", dir); code != 0 ||
		!strings.Contains(out, "skills/dev-architecture") || !strings.Contains(out, "agents/code-reviewer") {
		t.Errorf("bare copy should list items; code=%d out=%s", code, out)
	}
	// `copy <kind>` lists only that kind.
	code, out, _ := run(t, "copy", "--root", dir, "agents")
	if code != 0 || !strings.Contains(out, "agents/implementer") {
		t.Errorf("copy <kind> should list that kind; code=%d out=%s", code, out)
	}
	if strings.Contains(out, "skills/dev-architecture") {
		t.Errorf("copy agents should not list skills:\n%s", out)
	}
}

func TestCopyRequiresWorkspace(t *testing.T) {
	dir := t.TempDir() // no .claude/
	if code, _, errOut := run(t, "copy", "--root", dir, "skills/tdd-cycle"); code != 1 ||
		!strings.Contains(errOut, "not a csdd workspace") {
		t.Errorf("copy outside a workspace should exit 1; code=%d err=%s", code, errOut)
	}
}
