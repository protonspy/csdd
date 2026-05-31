package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/workspace"
)

// Export target layout. These constants intentionally live here rather than in
// internal/paths: that package defines the *Claude Code* workspace layout,
// whereas these are foreign formats produced only by `csdd export`.
const (
	kiroDir     = ".kiro"
	codexDir    = ".codex"
	agentsFile  = "AGENTS.md"
	codexConfig = "config.toml"

	// codexDocMaxBytes mirrors Codex's default project_doc_max_bytes (32 KiB):
	// AGENTS.md is truncated past this when Codex builds project instructions,
	// so we warn rather than silently overflow.
	codexDocMaxBytes = 32 * 1024
)

// kiroSpecFiles are the SDD artifacts copied into a Kiro spec. spec.json is
// deliberately omitted — Kiro tracks phase/approval state inside the IDE.
var kiroSpecFiles = []string{"requirements.md", "design.md", "tasks.md", "research.md", "bugfix.md"}

// ExportOptions is the headless input shared by the CLI and the TUI so both
// surfaces drive the exact same conversion.
type ExportOptions struct {
	Root  string // csdd workspace root (resolved via workspace.Resolve)
	Out   string // output base dir; empty → same as Root
	Force bool   // overwrite existing target files
}

func runExport(args []string) int {
	target, rest, err := parseAction("export", args)
	if err != nil {
		render.Err(err.Error())
		render.Info("usage: " + prog() + " export {kiro|codex} [--out DIR] [--force]")
		return 1
	}
	fs := flag.NewFlagSet("export "+target, flag.ContinueOnError)
	var opts ExportOptions
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Out, "out", "", "Output base directory (default: project root).")
	addForce(fs, &opts.Force)
	if _, err := parseFlags(fs, rest); err != nil {
		return failOnFlagParse(err)
	}
	switch target {
	case "kiro":
		err = ExportKiro(opts)
	case "codex":
		err = ExportCodex(opts)
	default:
		render.Err("unknown export target: " + target + " (want kiro|codex)")
		return 1
	}
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// exportBase resolves the csdd workspace root and the output base directory.
func exportBase(opts ExportOptions) (root, out string, err error) {
	root, err = workspace.Resolve(opts.Root)
	if err != nil {
		return "", "", err
	}
	out = opts.Out
	if out == "" {
		out = root
		return root, out, nil
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return "", "", err
	}
	return root, out, nil
}

// ExportKiro converts the workspace to Kiro's `.kiro/` layout. Steering
// frontmatter (always|fileMatch|manual|auto, fileMatchPattern) is already
// Kiro-compatible, so steering files copy verbatim; specs copy their SDD
// markdown (without spec.json).
func ExportKiro(opts ExportOptions) error {
	root, out, err := exportBase(opts)
	if err != nil {
		return err
	}
	written := 0

	// Steering → .kiro/steering (verbatim — frontmatter is already compatible).
	steerSrc := paths.Steering(root)
	if entries, err := os.ReadDir(steerSrc); err == nil {
		dstDir := filepath.Join(out, kiroDir, paths.SteeringSeg)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(steerSrc, e.Name()))
			if err != nil {
				return err
			}
			dst := filepath.Join(dstDir, e.Name())
			if err := workspace.WriteFile(dst, string(data), opts.Force); err != nil {
				return err
			}
			written++
		}
		if written > 0 {
			render.OK(fmt.Sprintf("steering → %s (%d file(s))", workspace.Relative(out, dstDir), written))
		}
	}

	// Specs → .kiro/specs (SDD markdown only; Kiro tracks state in-IDE).
	specsSrc := paths.Specs(root)
	if features, err := os.ReadDir(specsSrc); err == nil {
		for _, f := range features {
			if !f.IsDir() {
				continue
			}
			srcDir := filepath.Join(specsSrc, f.Name())
			dstDir := filepath.Join(out, kiroDir, paths.SpecsSeg, f.Name())
			copied := 0
			for _, name := range kiroSpecFiles {
				data, err := os.ReadFile(filepath.Join(srcDir, name))
				if err != nil {
					continue // artifact not generated yet
				}
				if err := workspace.WriteFile(filepath.Join(dstDir, name), string(data), opts.Force); err != nil {
					return err
				}
				copied++
			}
			if copied > 0 {
				written += copied
				render.OK(fmt.Sprintf("spec → %s (%d file(s))", workspace.Relative(out, dstDir), copied))
			}
		}
	}

	if written == 0 {
		render.Info("nothing to export (no steering or specs found under " + root + ")")
		return nil
	}
	render.OK(fmt.Sprintf("exported %d file(s) to %s/", written, workspace.Relative(out, filepath.Join(out, kiroDir))))
	return nil
}

// ExportCodex converts the workspace to Codex's conventions: an AGENTS.md built
// from CLAUDE.md with steering inlined (Codex doesn't resolve @-imports), plus
// .codex/config.toml for any MCP servers.
func ExportCodex(opts ExportOptions) error {
	root, out, err := exportBase(opts)
	if err != nil {
		return err
	}

	agents, err := buildAgentsMD(root)
	if err != nil {
		return err
	}
	dst := filepath.Join(out, agentsFile)
	if err := workspace.WriteFile(dst, agents, opts.Force); err != nil {
		return err
	}
	render.OK("wrote " + workspace.Relative(out, dst))
	if len(agents) > codexDocMaxBytes {
		render.Warn(fmt.Sprintf("AGENTS.md is %d bytes; Codex reads only the first %d (project_doc_max_bytes) — trim always-on steering", len(agents), codexDocMaxBytes))
	}

	cfg, err := loadMCP(paths.MCP(root))
	if err != nil {
		return err
	}
	if len(cfg.MCPServers) == 0 {
		render.Info("no MCP servers to convert (skipped .codex/config.toml)")
		return nil
	}
	for _, name := range sortedServerNames(cfg) {
		s := cfg.MCPServers[name]
		if s.URL != "" {
			render.Warn("MCP '" + name + "' is remote (url) — Codex needs experimental_use_rmcp_client=true; emitted with a note")
		}
		if s.Disabled {
			render.Warn("MCP '" + name + "' is disabled — emitted commented-out in config.toml")
		}
	}
	tomlDst := filepath.Join(out, codexDir, codexConfig)
	if err := workspace.WriteFile(tomlDst, mcpToTOML(cfg), opts.Force); err != nil {
		return err
	}
	render.OK(fmt.Sprintf("wrote %s (%d server(s))", workspace.Relative(out, tomlDst), len(cfg.MCPServers)))
	return nil
}

// steeringDoc is one steering file resolved for inlining into AGENTS.md.
type steeringDoc struct {
	name string // file name, e.g. "product.md"
	path string // absolute source path
	body string // markdown body, frontmatter stripped
	hint string // on-demand pointer text (conditional steering only)
}

// collectSteering reads .claude/steering/*.md and splits the always-on files
// (inlined in full) from the conditional ones (listed as on-demand pointers).
// A missing steering dir is not an error — it yields empty slices.
func collectSteering(root string) (always, conditional []steeringDoc, err error) {
	dir := paths.Steering(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		fm := frontmatter.Parse(string(data))
		doc := steeringDoc{name: name, path: p, body: fm.Body}
		switch fm.AsString("inclusion", "always") {
		case "always":
			always = append(always, doc)
		case "fileMatch":
			doc.hint = "fileMatch: " + strings.Join(fm.AsStringSlice("fileMatchPattern"), ", ")
			conditional = append(conditional, doc)
		case "auto":
			doc.hint = "auto: " + fm.AsString("description", "")
			conditional = append(conditional, doc)
		case "manual":
			doc.hint = "manual (load with #" + strings.TrimSuffix(name, ".md") + ")"
			conditional = append(conditional, doc)
		default:
			doc.hint = fm.AsString("inclusion", "")
			conditional = append(conditional, doc)
		}
	}
	return always, conditional, nil
}

// renderSteeringInline produces the markdown that replaces CLAUDE.md's managed
// @-import block: always-on steering embedded in full, conditional steering as
// a bullet list of on-demand pointers.
func renderSteeringInline(root string, always, conditional []steeringDoc) string {
	var b strings.Builder
	for _, s := range always {
		b.WriteString("<!-- steering: " + s.name + " (always) -->\n")
		b.WriteString(strings.TrimRight(s.body, "\n"))
		b.WriteString("\n\n")
	}
	if len(conditional) > 0 {
		b.WriteString("### Conditional steering (open on demand)\n\n")
		b.WriteString("Codex has no conditional loading — open these files when relevant:\n\n")
		for _, s := range conditional {
			b.WriteString("- `" + filepath.ToSlash(workspace.Relative(root, s.path)) + "` — " + s.hint + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// buildAgentsMD renders AGENTS.md for Codex. The CLAUDE.md body is kept as the
// project's operating manual; its csdd-managed @-import block is replaced with
// the steering inlined directly, since Codex does not resolve @-imports.
func buildAgentsMD(root string) (string, error) {
	always, conditional, err := collectSteering(root)
	if err != nil {
		return "", err
	}
	inlined := renderSteeringInline(root, always, conditional)
	if inlined == "" {
		inlined = "_No steering files defined._\n"
	}

	data, err := os.ReadFile(paths.Entry(root))
	if err != nil {
		// No CLAUDE.md — emit a minimal standalone AGENTS.md.
		out := "# Project Entry Point\n\nGenerated by `csdd export codex` from the csdd workspace.\n\n## Project memory (steering)\n\n" + inlined
		return ensureTrailingNewline(out), nil
	}
	text := string(data)
	start := strings.Index(text, steeringMarkerStart)
	end := strings.Index(text, steeringMarkerEnd)
	if start == -1 || end == -1 || end < start {
		// Unmanaged CLAUDE.md — append steering at the end.
		out := strings.TrimRight(text, "\n") + "\n\n## Project memory (steering)\n\n" + inlined
		return ensureTrailingNewline(out), nil
	}
	end += len(steeringMarkerEnd)
	out := text[:start] + inlined + strings.TrimLeft(text[end:], "\n")
	return ensureTrailingNewline(out), nil
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// mcpToTOML renders .mcp.json as Codex's config.toml [mcp_servers.*] tables.
// It is pure (no IO) so it can be unit-tested directly. Disabled servers are
// emitted commented-out to preserve the entry without activating it; remote
// (url) servers carry a note since Codex gates them behind an experimental flag.
func mcpToTOML(cfg MCPConfig) string {
	var b strings.Builder
	b.WriteString("# Generated by `csdd export codex` from .mcp.json.\n")
	b.WriteString("# Codex reads MCP servers from [mcp_servers.<name>] tables.\n")
	for _, name := range sortedServerNames(cfg) {
		s := cfg.MCPServers[name]
		var block strings.Builder
		fmt.Fprintf(&block, "[mcp_servers.%s]\n", tomlKey(name))
		switch {
		case strings.TrimSpace(s.Command) != "":
			fmt.Fprintf(&block, "command = %s\n", tomlString(s.Command))
			if len(s.Args) > 0 {
				fmt.Fprintf(&block, "args = %s\n", tomlStringArray(s.Args))
			}
		case strings.TrimSpace(s.URL) != "":
			block.WriteString("# remote server — set experimental_use_rmcp_client = true in Codex to use url\n")
			fmt.Fprintf(&block, "url = %s\n", tomlString(s.URL))
		}
		if len(s.Env) > 0 {
			fmt.Fprintf(&block, "\n[mcp_servers.%s.env]\n", tomlKey(name))
			for _, k := range sortedKeys(s.Env) {
				fmt.Fprintf(&block, "%s = %s\n", tomlKey(k), tomlString(s.Env[k]))
			}
		}
		text := block.String()
		b.WriteString("\n")
		if s.Disabled {
			b.WriteString("# --- disabled in .mcp.json (kept for reference) ---\n")
			text = commentOut(text)
		}
		b.WriteString(text)
	}
	return b.String()
}

// tomlString quotes and escapes a TOML basic string.
func tomlString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func tomlStringArray(items []string) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = tomlString(it)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlKey emits a bare key when possible, falling back to a quoted key.
func tomlKey(s string) string {
	if s == "" {
		return tomlString(s)
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return tomlString(s)
		}
	}
	return s
}

func commentOut(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = "#"
			continue
		}
		lines[i] = "# " + l
	}
	return strings.Join(lines, "\n") + "\n"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
