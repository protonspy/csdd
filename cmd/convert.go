package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/livelo/csdd/internal/frontmatter"
	"github.com/livelo/csdd/internal/render"
	"github.com/livelo/csdd/internal/workspace"
)

// runConvert backs the intentionally undocumented ("silent") `csdd convert`
// command. It exists and is fully supported, but is omitted from `csdd --help`
// and from csdd.md on purpose — it is an escape hatch for porting a Kiro
// workspace onto another agent platform's expected layout.
func runConvert(args []string) int {
	action, rest, err := parseAction("convert", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	switch action {
	case "claude":
		return convertClaude(rest)
	default:
		render.Err("unknown convert target: " + action)
		return 1
	}
}

func convertClaude(args []string) int {
	fs := flag.NewFlagSet("convert claude", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if err := ConvertToClaude(r, force); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// ConvertToClaude mirrors a Kiro workspace into the layout Claude Code expects:
//
//	.agents/agents/*.md       → .claude/agents/*.md       (Claude subagents)
//	.agents/skills/<name>/    → .claude/skills/<name>/     (Claude Agent Skills)
//	.kiro/settings/mcp.json   → .mcp.json                 (active servers only)
//	always-inclusion steering → CLAUDE.md                 (with @imports)
//
// Kiro's agent and skill frontmatter is already Claude-compatible, so those are
// structural copies. Without force, existing targets are preserved (the command
// is safe to re-run); with --force, targets are overwritten.
func ConvertToClaude(root string, force bool) error {
	agents, err := copyMarkdownDir(workspace.AgentRoot(root), filepath.Join(root, ".claude", "agents"), force)
	if err != nil {
		return err
	}
	skills, err := copySkillDirs(workspace.SkillRoot(root), filepath.Join(root, ".claude", "skills"), force)
	if err != nil {
		return err
	}
	var mcpNames []string
	var skipped int
	if path, err := mcpFile(root); err == nil {
		mcpNames, skipped, err = convertMCPToClaude(path, filepath.Join(root, ".mcp.json"), force)
		if err != nil {
			return err
		}
	}
	wroteSettings, err := writeClaudeSettings(root, mcpNames, force)
	if err != nil {
		return err
	}
	if err := writeClaudeMd(root, force); err != nil {
		return err
	}
	render.OK(fmt.Sprintf("converted to Claude Code: %d agent(s) → .claude/agents/, %d skill(s) → .claude/skills/, %d MCP server(s) → .mcp.json",
		agents, skills, len(mcpNames)))
	if skipped > 0 {
		render.Info(fmt.Sprintf("%d disabled MCP server(s) omitted from .mcp.json", skipped))
	}
	if wroteSettings {
		render.Info(".claude/settings.json: project MCP servers enabled + .kiroignore mapped to Read-deny")
	}
	render.Info("entry point: CLAUDE.md")
	return nil
}

// copyMarkdownDir copies every *.md file from src to dst and returns how many
// source files were converted. A missing src is treated as empty.
func copyMarkdownDir(src, dst string, force bool) (int, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var count int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return count, err
		}
		if err := writeConverted(filepath.Join(dst, e.Name()), data, force); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// copySkillDirs copies each skill subdirectory of src (recursively) into dst and
// returns how many skills were converted.
func copySkillDirs(src, dst string, force bool) (int, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var count int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), force); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func copyTree(src, dst string, force bool) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeConverted(target, data, force)
	})
}

// convertMCPToClaude writes a clean .mcp.json: active servers only, with the
// Kiro-only keys (disabled, autoApprove) stripped. Returns the sorted active
// server names and the count of skipped (disabled) servers.
func convertMCPToClaude(srcPath, dstPath string, force bool) ([]string, int, error) {
	cfg, err := loadMCP(srcPath)
	if err != nil {
		return nil, 0, err
	}
	out := MCPConfig{MCPServers: map[string]MCPServer{}}
	var skipped int
	var names []string
	for name, s := range cfg.MCPServers {
		if s.Disabled {
			skipped++
			continue
		}
		s.Disabled = false
		s.AutoApprove = nil
		out.MCPServers[name] = s
		names = append(names, name)
	}
	sort.Strings(names)
	if len(out.MCPServers) == 0 {
		return nil, skipped, nil
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, skipped, err
	}
	b = append(b, '\n')
	if err := writeConverted(dstPath, b, force); err != nil {
		return nil, skipped, err
	}
	return names, skipped, nil
}

// claudeSettings is the minimal subset of .claude/settings.json this command
// produces. Marshaled field order is stable (declaration order).
type claudeSettings struct {
	EnabledMcpjsonServers []string           `json:"enabledMcpjsonServers,omitempty"`
	Permissions           *claudePermissions `json:"permissions,omitempty"`
}

type claudePermissions struct {
	Deny []string `json:"deny,omitempty"`
}

// writeClaudeSettings produces .claude/settings.json so the converted workspace
// is standards-complete: it enables the project MCP servers (no trust prompt)
// and translates .kiroignore into Read-deny permissions. Returns whether a file
// was written (nothing worth writing → skipped).
func writeClaudeSettings(root string, mcpServers []string, force bool) (bool, error) {
	settings := claudeSettings{EnabledMcpjsonServers: mcpServers}
	if deny := kiroignoreDenyRules(root); len(deny) > 0 {
		settings.Permissions = &claudePermissions{Deny: deny}
	}
	if len(settings.EnabledMcpjsonServers) == 0 && settings.Permissions == nil {
		return false, nil
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	b = append(b, '\n')
	if err := writeConverted(filepath.Join(root, ".claude", "settings.json"), b, force); err != nil {
		return false, err
	}
	return true, nil
}

// kiroignoreDenyRules maps each .kiroignore pattern to a Claude Read-deny rule.
// Comments and gitignore negations (`!`) are skipped — Read-deny has no "allow
// back" form.
func kiroignoreDenyRules(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".kiroignore"))
	if err != nil {
		return nil
	}
	var rules []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		rules = append(rules, "Read("+line+")")
	}
	return rules
}

// writeClaudeMd generates the CLAUDE.md entry point, importing always-on
// steering files so Claude Code loads the same project memory Kiro would.
func writeClaudeMd(root string, force bool) error {
	always, conditional := classifySteering(filepath.Join(root, ".kiro", "steering"))
	var b strings.Builder
	b.WriteString("# CLAUDE.md\n\n")
	b.WriteString("> Generated by `csdd convert claude` from the Kiro workspace. Re-run with `--force` after changing Kiro artifacts.\n\n")
	b.WriteString("This project follows the Kiro Spec-Driven Development workflow. The canonical, self-contained guide is `docs/guides/kiro_sdd.md`; the operational CLI guide is `csdd.md`.\n\n")
	b.WriteString("## Project memory (steering)\n\n")
	if len(always) == 0 {
		b.WriteString("_No always-on steering files._\n")
	} else {
		b.WriteString("Always-on project memory:\n\n")
		for _, name := range always {
			b.WriteString("@.kiro/steering/" + name + "\n")
		}
	}
	if len(conditional) > 0 {
		b.WriteString("\nLoad on demand when relevant: " + strings.Join(conditional, ", ") + ".\n")
	}
	b.WriteString("\n## Subagents\n\nCustom subagents are in `.claude/agents/` (converted from `.agents/agents/`).\n")
	b.WriteString("\n## Skills\n\nAgent skills are in `.claude/skills/` (converted from `.agents/skills/`).\n")
	b.WriteString("\n## MCP\n\nWorkspace MCP servers are in `.mcp.json`.\n")
	return writeConverted(filepath.Join(root, "CLAUDE.md"), []byte(b.String()), force)
}

// classifySteering splits steering files into always-on and conditional sets.
func classifySteering(dir string) (always, conditional []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if frontmatter.Parse(string(data)).AsString("inclusion", "") == "always" {
			always = append(always, e.Name())
		} else {
			conditional = append(conditional, e.Name())
		}
	}
	sort.Strings(always)
	sort.Strings(conditional)
	return always, conditional
}

// writeConverted writes data to path. With force it overwrites; otherwise it
// preserves an existing file (so the conversion is safe to re-run).
func writeConverted(path string, data []byte, force bool) error {
	if force {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	}
	_, err := workspace.SafeWrite(path, string(data))
	return err
}
