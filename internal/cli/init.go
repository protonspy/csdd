package cli

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/workspace"
)

func runInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	var root string
	var withBaseline, noMCP bool
	var exclude stringSliceFlag
	fs.StringVar(&root, "root", "", "Target directory (default: cwd).")
	fs.BoolVar(&withBaseline, "with-baseline", false, "Also scaffold product.md, tech.md, structure.md.")
	fs.Var(&exclude, "exclude", "Scaffold components to skip (comma-separated, repeatable), e.g. --exclude agents,skills. Valid: "+strings.Join(excludableKeys(), ", ")+".")
	fs.BoolVar(&noMCP, "no-mcp", false, "Do not register the csdd MCP server in .mcp.json.")
	if err := fs.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	if err := rejectPositionals("init", fs); err != nil {
		render.Err(err.Error())
		return 1
	}
	exSet, err := parseExclusions(exclude.values)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	opts := initOptions{withBaseline: withBaseline, exclude: exSet}
	if root == "" {
		var err error
		root, err = filepath.Abs(".")
		if err != nil {
			render.Err(err.Error())
			return 1
		}
	} else {
		abs, err := filepath.Abs(root)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
		root = abs
	}

	created, err := initWorkspace(root, opts, templates)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("Initialized Claude Code workspace at " + root)
	render.Info("directories created: " + intStr(created.dirs))
	render.Info("files created: " + intStr(created.files))
	if len(exSet) > 0 {
		render.Info("excluded: " + strings.Join(excludedList(exSet), ", "))
	}
	// Post-scaffold chores only fire for the components that were actually laid
	// down, so `--exclude mcp` / `--exclude pre-push` don't advertise things that
	// aren't there.
	if !noMCP && !opts.skip("mcp") {
		if added, err := ensureCsddMCPServer(root); err != nil {
			render.Warn("could not register csdd MCP server: " + err.Error())
		} else if added {
			render.Info("registered the csdd MCP server (drive the dev flow as tools) in " + workspace.Relative(root, paths.MCP(root)))
		}
	}
	offerGitignore(root)
	if !opts.skip("pre-push") {
		render.Info("Enable the pre-push test gate: `git config core.hooksPath .githooks`")
	}
	if !opts.withBaseline && !opts.skip("steering") {
		render.Info("Run `" + prog() + " steering init` to scaffold standard steering files.")
	}
	return 0
}

// offerGitignore asks whether to add the root-level csdd artifacts (the compiled
// binary and the pinggy token secret) to .gitignore, then appends the ones the
// user accepts. It is a no-op when none of those files exist or when running
// non-interactively, so it never blocks headless/agent-driven `csdd init`.
func offerGitignore(root string) {
	targets := gitignoreTargets(root)
	if len(targets) == 0 {
		return
	}
	if !confirm("Add " + strings.Join(targets, ", ") + " to .gitignore?") {
		return
	}
	added, err := ensureGitignore(root, targets)
	switch {
	case err != nil:
		render.Warn("could not update .gitignore: " + err.Error())
	case len(added) == 0:
		render.Info(".gitignore already covers those entries")
	default:
		render.OK("Added to .gitignore: " + strings.Join(added, ", "))
	}
}

// csddMCPServerName is the key under which `csdd init` registers the csdd MCP
// server in .mcp.json.
const csddMCPServerName = "csdd"

// ensureCsddMCPServer registers the csdd MCP server (a stdio server launched via
// npx) in .mcp.json, unless an entry of that name already exists. The npm
// package resolves the matching prebuilt csdd binary through its
// optionalDependencies, so the entry is portable across machines — no absolute
// paths. Idempotent; returns whether a new entry was written.
func ensureCsddMCPServer(root string) (bool, error) {
	path := paths.MCP(root)
	cfg, err := loadMCP(path)
	if err != nil {
		return false, err
	}
	if _, exists := cfg.MCPServers[csddMCPServerName]; exists {
		return false, nil
	}
	cfg.MCPServers[csddMCPServerName] = MCPServer{
		Command: "npx",
		Args:    []string{"-y", "@protonspy/csdd-mcp"},
	}
	if err := saveMCP(path, cfg); err != nil {
		return false, err
	}
	return true, nil
}

type initCounts struct {
	dirs, files int
}

// excludable is the ordered set of scaffold components `csdd init --exclude` can
// skip. The order is used for deterministic help text and summary output.
var excludable = []struct{ key, desc string }{
	{"claude-md", "CLAUDE.md entry point"},
	{"mcp", ".mcp.json + csdd MCP server registration"},
	{"settings", ".claude/settings.json"},
	{"pr-template", ".github/pull_request_template.md"},
	{"pre-push", ".githooks/pre-push test gate"},
	{"rules", ".claude/rules/"},
	{"templates", ".claude/templates/"},
	{"steering", ".claude/steering/"},
	{"agents", ".claude/agents/"},
	{"skills", ".claude/skills/"},
	{"commands", ".claude/commands/"},
	{"hooks", ".claude/hooks/"},
	{"specs", "specs/"},
	{"knowledge", "docs/ knowledge base (plans/raw/wiki/graph) + docs/stack.md tech contract"},
}

// excludableKeys returns the valid --exclude component keys in canonical order.
func excludableKeys() []string {
	keys := make([]string, len(excludable))
	for i, comp := range excludable {
		keys[i] = comp.key
	}
	return keys
}

// parseExclusions flattens the (repeatable, comma-separated) --exclude values
// into a validated set of component keys. Unknown keys are rejected so a typo
// like "agent" fails loudly instead of silently scaffolding the component.
func parseExclusions(raw []string) (map[string]bool, error) {
	valid := map[string]bool{}
	for _, k := range excludableKeys() {
		valid[k] = true
	}
	set := map[string]bool{}
	for _, chunk := range raw {
		for _, part := range strings.Split(chunk, ",") {
			key := strings.ToLower(strings.TrimSpace(part))
			if key == "" {
				continue
			}
			if !valid[key] {
				return nil, fmt.Errorf("unknown --exclude component %q; valid: %s", key, strings.Join(excludableKeys(), ", "))
			}
			set[key] = true
		}
	}
	return set, nil
}

// excludedList returns the excluded keys present in set, in canonical order.
func excludedList(set map[string]bool) []string {
	var out []string
	for _, k := range excludableKeys() {
		if set[k] {
			out = append(out, k)
		}
	}
	return out
}

// initOptions controls what `csdd init` scaffolds.
type initOptions struct {
	// withBaseline also writes the standard steering files (product/tech/…) and
	// imports them into CLAUDE.md.
	withBaseline bool
	// exclude is the set of scaffold components (see excludable) to skip, so csdd
	// can be dropped into a project that already has parts of a Claude Code setup.
	exclude map[string]bool
}

// skip reports whether the given scaffold component was excluded.
func (o initOptions) skip(component string) bool { return o.exclude[component] }

// initWorkspace is exported semantically so the TUI can call the same logic.
// It creates the standard Claude Code layout idempotently, honoring opts.exclude,
// and returns the counts of directories and files it created.
func initWorkspace(root string, opts initOptions, templates embed.FS) (initCounts, error) {
	var c initCounts
	templatesDir := paths.Templates(root)

	// Directories, each owned by the component that may exclude it. SafeWrite and
	// manifest.Save mkdir parents on demand, so only the genuinely-empty dirs
	// (steering, specs) strictly need this; creating the rest keeps the dir count
	// and on-disk layout stable.
	dirs := []struct{ comp, path string }{
		{"steering", paths.Steering(root)},
		{"rules", paths.Rules(root)},
		{"templates", filepath.Join(templatesDir, "specs")},
		{"templates", filepath.Join(templatesDir, "steering")},
		{"templates", filepath.Join(templatesDir, "steering-custom")},
		{"specs", paths.Specs(root)},
		{"skills", paths.Skills(root)},
		{"agents", paths.Agents(root)},
		{"commands", paths.Commands(root)},
		{"hooks", paths.Hooks(root)},
		{"knowledge", paths.DocsPlans(root)},
		{"knowledge", paths.DocsRaw(root)},
		{"knowledge", filepath.Join(paths.DocsWiki(root), "pages")},
		{"knowledge", paths.DocsGraph(root)},
	}
	for _, d := range dirs {
		if opts.skip(d.comp) || pathExists(d.path) {
			continue
		}
		if err := mkdirAll(d.path); err != nil {
			return c, err
		}
		c.dirs++
	}

	// Shared entry files at the repo root (plus .github/.githooks). CLAUDE.md is
	// the single, self-contained entry point; the rest are the automation surface.
	rootFiles := []struct {
		comp, path, tpl string
		exec            bool
	}{
		{"claude-md", paths.Entry(root), "templates/root/CLAUDE.md.tmpl", false},
		{"mcp", paths.MCP(root), "templates/root/mcp.json.tmpl", false},
		{"settings", paths.Settings(root), "templates/root/settings.json.tmpl", false},
		{"pre-push", filepath.Join(root, ".githooks", "pre-push"), "templates/root/pre-push.tmpl", true},
		{"pr-template", filepath.Join(root, ".github", "pull_request_template.md"), "templates/root/pull-request.md.tmpl", false},
	}
	for _, rf := range rootFiles {
		if opts.skip(rf.comp) {
			continue
		}
		content, err := templater.Static(templates, rf.tpl)
		if err != nil {
			return c, err
		}
		created, err := workspace.SafeWrite(rf.path, content)
		if err != nil {
			return c, err
		}
		if created {
			c.files++
		}
		// The git pre-push hook must be executable to run as a hook.
		if rf.exec {
			_ = os.Chmod(rf.path, 0o755)
		}
	}

	// Generation rules.
	if !opts.skip("rules") {
		rules, err := templater.RuleFiles(templates)
		if err != nil {
			return c, err
		}
		for name, content := range rules {
			created, err := workspace.SafeWrite(filepath.Join(paths.Rules(root), name), content)
			if err != nil {
				return c, err
			}
			if created {
				c.files++
			}
		}
	}

	// Versioned templates (spec + steering + steering-custom references).
	if !opts.skip("templates") {
		versioned, err := templater.WorkflowTemplateFiles(templates)
		if err != nil {
			return c, err
		}
		for rel, content := range versioned {
			created, err := workspace.SafeWrite(filepath.Join(paths.Templates(root), filepath.FromSlash(rel)), content)
			if err != nil {
				return c, err
			}
			if created {
				c.files++
			}
		}
	}

	// Shipped sub-agents, workflow skills, commands, and hooks — each excludable.
	trees := []struct {
		comp, base string
		files      func(fs.FS) (map[string]string, error)
		exec       bool // chmod 0755 (hook scripts)
	}{
		{"agents", paths.Agents(root), templater.AgentFiles, false},
		{"skills", paths.Skills(root), templater.SkillFiles, false},
		{"commands", paths.Commands(root), templater.CommandFiles, false},
		{"hooks", paths.Hooks(root), templater.HookFiles, true},
	}
	for _, tw := range trees {
		if opts.skip(tw.comp) {
			continue
		}
		entries, err := tw.files(templates)
		if err != nil {
			return c, err
		}
		for rel, content := range entries {
			path := filepath.Join(tw.base, filepath.FromSlash(rel))
			created, err := workspace.SafeWrite(path, content)
			if err != nil {
				return c, err
			}
			if created {
				c.files++
				if tw.exec {
					_ = os.Chmod(path, 0o755)
				}
			}
		}
	}

	// Baseline steering files, imported into CLAUDE.md as always-on memory.
	if opts.withBaseline && !opts.skip("steering") {
		tpls := standardSteeringTemplates()
		names := standardSteeringOrder()
		for _, name := range names {
			content, err := templater.Static(templates, tpls[name])
			if err != nil {
				return c, err
			}
			created, err := workspace.SafeWrite(filepath.Join(paths.Steering(root), name), content)
			if err != nil {
				return c, err
			}
			if created {
				c.files++
			}
		}
		// A no-op when CLAUDE.md was excluded (there is nothing to wire into).
		if _, err := ensureSteeringImports(root, names...); err != nil {
			return c, err
		}
	}

	// The .csdd/ operational-state directory is the workspace marker (R15.2), so
	// it is always created — even when the knowledge scaffold is excluded.
	if !pathExists(paths.State(root)) {
		if err := mkdirAll(paths.State(root)); err != nil {
			return c, err
		}
		c.dirs++
	}

	// Knowledge-base scaffold: the docs/ raw+wiki structure, the generated-index
	// and state READMEs, and the (choice-free) tech contract. Idempotent — never
	// overwrites, never touches docs/raw/ contents (R9.3, R9.4, R15.1).
	if !opts.skip("knowledge") {
		scaffolds := []func(fs.FS) (map[string]string, error){
			templater.WikiScaffoldFiles,
			templater.KnowledgeStructureFiles,
		}
		for _, fn := range scaffolds {
			files, err := fn(templates)
			if err != nil {
				return c, err
			}
			for rel, content := range files {
				created, err := workspace.SafeWrite(filepath.Join(root, filepath.FromSlash(rel)), content)
				if err != nil {
					return c, err
				}
				if created {
					c.files++
				}
			}
		}
	}

	// Wire the managed knowledge-base section into CLAUDE.md (R16.3, R17.4, R17.7).
	if !opts.skip("claude-md") {
		if _, err := ensureKnowledgeSection(root, templates); err != nil {
			return c, err
		}
	}

	// Record the manifest of csdd-managed artifacts so a later `csdd update` can
	// tell pristine shipped files from ones the user has since edited.
	if err := recordManifest(root, templates, time.Now(), nil); err != nil {
		return c, err
	}
	return c, nil
}

func standardSteeringTemplates() map[string]string {
	return map[string]string{
		"product.md":         "templates/steering/product.md.tmpl",
		"tech.md":            "templates/steering/tech.md.tmpl",
		"structure.md":       "templates/steering/structure.md.tmpl",
		"security.md":        "templates/steering/security.md.tmpl",
		"testing.md":         "templates/steering/testing.md.tmpl",
		"api-conventions.md": "templates/steering/api-conventions.md.tmpl",
	}
}

// standardSteeringOrder returns the baseline steering file names in a stable
// sorted order. Both scaffolders iterate this instead of the map directly, so
// the files are created — and the CLAUDE.md @-import block is written — in the
// same order on every run and machine (Go map iteration is randomized).
func standardSteeringOrder() []string {
	m := standardSteeringTemplates()
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
