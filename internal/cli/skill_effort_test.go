package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readSkillMD returns the SKILL.md contents for a created skill.
func readSkillMD(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/skills", name, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	return string(data)
}

func TestSkillCreateModelEffort(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "skill", "create", "with-both",
		"--description", "Demo trigger.",
		"--model", "opus", "--effort", "low", "--root", dir)
	if code != 0 {
		t.Fatalf("skill create with model+effort failed (code=%d): %s", code, errOut)
	}
	body := readSkillMD(t, dir, "with-both")
	for _, want := range []string{"model: opus", "effort: low"} {
		if !strings.Contains(body, want) {
			t.Errorf("SKILL.md missing %q:\n%s", want, body)
		}
	}
}

func TestSkillCreateModelOnly(t *testing.T) {
	dir := freshWorkspace(t)
	// A free-form model string (not a known alias) is accepted — model is not validated.
	code, _, _ := run(t, "skill", "create", "model-only",
		"--description", "x.", "--model", "claude-opus-4-8", "--root", dir)
	if code != 0 {
		t.Fatalf("free-form model should be accepted, got exit %d", code)
	}
	body := readSkillMD(t, dir, "model-only")
	if !strings.Contains(body, "model: claude-opus-4-8") {
		t.Errorf("SKILL.md missing free-form model line:\n%s", body)
	}
	if strings.Contains(body, "effort:") {
		t.Errorf("SKILL.md must not carry effort when --effort absent:\n%s", body)
	}
}

func TestSkillCreateNeitherIsByteIdentical(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "skill", "create", "plain", "--description", "Plain trigger.", "--root", dir)
	// Normalize line endings so the check is checkout-agnostic (templates ship CRLF).
	body := strings.ReplaceAll(readSkillMD(t, dir, "plain"), "\r\n", "\n")
	// With neither flag the frontmatter block (between the first two `---`) is
	// exactly name + description — byte-identical to the pre-feature shape, with
	// no model/effort keys. Compare the whole block, not just a prefix, so any
	// stray key or whitespace change is caught.
	const wantFrontmatter = "name: plain\ndescription: Plain trigger.\n"
	parts := strings.SplitN(body, "---\n", 3)
	if len(parts) < 3 || parts[0] != "" {
		t.Fatalf("SKILL.md does not start with a `---` frontmatter block:\n%q", body)
	}
	if parts[1] != wantFrontmatter {
		t.Errorf("frontmatter not byte-identical to pre-feature shape.\ngot:\n%q\nwant:\n%q", parts[1], wantFrontmatter)
	}
}

func TestSkillCreateInvalidEffortRejected(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "skill", "create", "bad-effort",
		"--description", "x.", "--effort", "highish", "--root", dir)
	if code != 1 {
		t.Errorf("invalid --effort should exit 1, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/skills/bad-effort")); !os.IsNotExist(err) {
		t.Errorf("invalid --effort must not create the skill directory (err=%v)", err)
	}
}
