package cmd

import (
	"embed"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/livelo/csdd/internal/paths"
	"github.com/livelo/csdd/internal/render"
	"github.com/livelo/csdd/internal/templater"
	"github.com/livelo/csdd/internal/workspace"
)

func runInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	var root string
	var withBaseline bool
	fs.StringVar(&root, "root", "", "Target directory (default: cwd).")
	fs.BoolVar(&withBaseline, "with-baseline", false, "Also scaffold product.md, tech.md, structure.md.")
	if err := fs.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
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

	created, err := initWorkspace(root, withBaseline, templates)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("Initialized Claude Code workspace at " + root)
	render.Info("directories created: " + intStr(created.dirs))
	render.Info("files created: " + intStr(created.files))
	offerGitignore(root)
	if !withBaseline {
		render.Info("Run `csdd steering init` to scaffold standard steering files.")
	}
	return 0
}

// offerGitignore asks whether to add the root-level csdd artifacts (the binary
// and csdd.md) to .gitignore, then appends the ones the user accepts. It is a
// no-op when none of those files exist or when running non-interactively, so it
// never blocks headless/agent-driven `csdd init`.
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

type initCounts struct {
	dirs, files int
}

// initWorkspace is exported semantically so the TUI can call the same logic.
// It creates the standard Claude Code layout idempotently and returns counts.
func initWorkspace(root string, withBaseline bool, templates embed.FS) (initCounts, error) {
	var c initCounts
	templatesDir := paths.Templates(root)
	layout := []string{
		paths.Steering(root),
		paths.Specs(root),
		paths.Rules(root),
		filepath.Join(templatesDir, "specs"),
		filepath.Join(templatesDir, "steering"),
		filepath.Join(templatesDir, "steering-custom"),
		paths.Skills(root),
		paths.Agents(root),
		paths.Hooks(root),
		filepath.Join(root, "docs", "guides"),
	}
	for _, d := range layout {
		if !pathExists(d) {
			if err := mkdirAll(d); err != nil {
				return c, err
			}
			c.dirs++
		}
	}
	// CLAUDE.md is the single repository entry point for agents; it imports the
	// steering files via @-references. docs/guides/claude-code-sdd.md is the
	// self-contained canonical spec — the only guide an agent needs to consult.
	files := map[string]string{
		paths.Entry(root):              "templates/root/CLAUDE.md.tmpl",
		filepath.Join(root, "csdd.md"): "templates/root/csdd.md.tmpl",
		paths.MCP(root):                "templates/root/mcp.json.tmpl",
		paths.Settings(root):           "templates/root/settings.json.tmpl",
		filepath.Join(root, ".github", "pull_request_template.md"):  "templates/root/pull-request.md.tmpl",
		filepath.Join(root, "docs", "guides", "claude-code-sdd.md"): "templates/guides/claude-code-sdd.md.tmpl",
	}
	for path, tplPath := range files {
		content, err := templater.Static(templates, tplPath)
		if err != nil {
			return c, err
		}
		created, err := workspace.SafeWrite(path, content)
		if err != nil {
			return c, err
		}
		if created {
			c.files++
		}
	}
	// Rules
	rules, err := templater.RuleFiles(templates)
	if err != nil {
		return c, err
	}
	for name, content := range rules {
		path := filepath.Join(paths.Rules(root), name)
		created, err := workspace.SafeWrite(path, content)
		if err != nil {
			return c, err
		}
		if created {
			c.files++
		}
	}
	versionedTemplates, err := templater.WorkflowTemplateFiles(templates)
	if err != nil {
		return c, err
	}
	for rel, content := range versionedTemplates {
		path := filepath.Join(paths.Templates(root), filepath.FromSlash(rel))
		created, err := workspace.SafeWrite(path, content)
		if err != nil {
			return c, err
		}
		if created {
			c.files++
		}
	}
	// Shipped sub-agents (least-privilege reviewers), workflow skills, and hooks.
	treeWrites := []struct {
		base  string
		files func(fs.FS) (map[string]string, error)
		exec  bool // chmod 0755 (hook scripts)
	}{
		{paths.Agents(root), templater.AgentFiles, false},
		{paths.Skills(root), templater.SkillFiles, false},
		{paths.Hooks(root), templater.HookFiles, true},
	}
	for _, tw := range treeWrites {
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
	if withBaseline {
		var names []string
		for name, tplPath := range standardSteeringTemplates() {
			content, err := templater.Static(templates, tplPath)
			if err != nil {
				return c, err
			}
			path := filepath.Join(paths.Steering(root), name)
			created, err := workspace.SafeWrite(path, content)
			if err != nil {
				return c, err
			}
			if created {
				c.files++
			}
			names = append(names, name)
		}
		// Import the baseline steering files into CLAUDE.md as always-on memory.
		if _, err := ensureSteeringImports(root, names...); err != nil {
			return c, err
		}
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
