package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedForConvert builds a workspace with one agent, one skill, two MCP servers
// (one disabled), and the baseline always-on steering files.
func seedForConvert(t *testing.T) string {
	t.Helper()
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "agent", "create", "code-reviewer",
		"--description", "Read-only reviewer", "--tools", "Read", "--tools", "Grep", "--root", dir); code != 0 {
		t.Fatal("agent create failed")
	}
	if code, _, _ := run(t, "skill", "create", "kiro-tasks",
		"--description", "Generate tasks.", "--root", dir); code != 0 {
		t.Fatal("skill create failed")
	}
	if code, _, _ := run(t, "mcp", "add", "filesystem", "--command", "npx", "--arg", "-y", "--root", dir); code != 0 {
		t.Fatal("mcp add failed")
	}
	if code, _, _ := run(t, "mcp", "add", "legacy", "--command", "old", "--disabled", "--root", dir); code != 0 {
		t.Fatal("mcp add (disabled) failed")
	}
	return dir
}

func TestConvertClaude(t *testing.T) {
	dir := seedForConvert(t)
	if code, _, _ := run(t, "convert", "claude", "--root", dir); code != 0 {
		t.Fatal("convert claude failed")
	}

	// Subagent copied verbatim (Kiro frontmatter is already Claude-compatible).
	srcAgent, _ := os.ReadFile(filepath.Join(dir, ".agents/agents/code-reviewer.md"))
	dstAgent, err := os.ReadFile(filepath.Join(dir, ".claude/agents/code-reviewer.md"))
	if err != nil {
		t.Fatalf("converted agent missing: %v", err)
	}
	if string(srcAgent) != string(dstAgent) {
		t.Error("converted agent should be a verbatim copy of the Kiro agent")
	}

	// Skill tree copied.
	if _, err := os.Stat(filepath.Join(dir, ".claude/skills/kiro-tasks/SKILL.md")); err != nil {
		t.Errorf("converted skill missing: %v", err)
	}

	// .mcp.json: active server present, disabled one omitted, Kiro-only keys stripped.
	mcp, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json missing: %v", err)
	}
	text := string(mcp)
	if !strings.Contains(text, "filesystem") {
		t.Error(".mcp.json should contain the active server")
	}
	if strings.Contains(text, "legacy") {
		t.Error(".mcp.json should omit the disabled server")
	}
	if strings.Contains(text, "disabled") || strings.Contains(text, "autoApprove") {
		t.Errorf(".mcp.json should not carry Kiro-only keys:\n%s", text)
	}

	// CLAUDE.md imports always-on steering (product.md is inclusion: always).
	claude, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md missing: %v", err)
	}
	if !strings.Contains(string(claude), "@.kiro/steering/product.md") {
		t.Errorf("CLAUDE.md should import always-on steering:\n%s", claude)
	}

	// .claude/settings.json makes the workspace standards-complete: it enables
	// the active project MCP server (not the disabled one) and maps .kiroignore
	// to Read-deny permissions.
	settings, err := os.ReadFile(filepath.Join(dir, ".claude/settings.json"))
	if err != nil {
		t.Fatalf(".claude/settings.json missing: %v", err)
	}
	st := string(settings)
	if !strings.Contains(st, "enabledMcpjsonServers") || !strings.Contains(st, "filesystem") {
		t.Errorf("settings.json should enable the active MCP server:\n%s", st)
	}
	if strings.Contains(st, "legacy") {
		t.Error("settings.json should not enable the disabled MCP server")
	}
	if !strings.Contains(st, `"Read(.env)"`) {
		t.Errorf("settings.json should map .kiroignore to Read-deny rules:\n%s", st)
	}
}

func TestConvertClaudeForceAndIdempotent(t *testing.T) {
	dir := seedForConvert(t)
	if code, _, _ := run(t, "convert", "claude", "--root", dir); code != 0 {
		t.Fatal("first convert failed")
	}
	// Re-running without --force must not error and must preserve existing files.
	target := filepath.Join(dir, ".claude/agents/code-reviewer.md")
	if err := os.WriteFile(target, []byte("EDITED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "convert", "claude", "--root", dir); code != 0 {
		t.Fatal("re-convert without --force should succeed")
	}
	if data, _ := os.ReadFile(target); string(data) != "EDITED" {
		t.Error("without --force, an existing converted file must be preserved")
	}
	// With --force it should be overwritten back to the real agent definition.
	if code, _, _ := run(t, "convert", "claude", "--force", "--root", dir); code != 0 {
		t.Fatal("re-convert with --force should succeed")
	}
	if data, _ := os.ReadFile(target); string(data) == "EDITED" {
		t.Error("--force should overwrite the converted file")
	}
}

func TestConvertUnknownTarget(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "convert", "cursor", "--root", dir); code == 0 {
		t.Error("unknown convert target should fail")
	}
	if code, _, _ := run(t, "convert"); code == 0 {
		t.Error("convert without a target should fail")
	}
}

// TestConvertIsUndocumented locks in that the command stays silent: it must not
// appear in the help surface even though it dispatches correctly.
func TestConvertIsUndocumented(t *testing.T) {
	if strings.Contains(helpText, "convert") {
		t.Error("convert must remain undocumented — found it in helpText")
	}
}
