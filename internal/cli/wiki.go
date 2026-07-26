package cli

import (
	"embed"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/graph"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/workspace"
)

// runWiki dispatches `csdd wiki <action>`. The wiki is the LLM-authored knowledge
// base under docs/: the CLI scaffolds (init) and lints (lint) deterministically;
// content is authored by LLM sessions via the installed `wiki` skill.
func runWiki(args []string, templates embed.FS) int {
	action, rest, err := parseAction("wiki", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if isHelpFlag(action) {
		wikiHelp()
		return 0
	}
	switch action {
	case "init":
		return wikiInit(rest, templates)
	case "lint":
		return wikiLint(rest)
	default:
		render.Err("unknown action for `wiki`: " + action)
		wikiHelp()
		return 1
	}
}

func wikiHelp() {
	fmt.Println(`csdd wiki — the LLM-authored knowledge base under docs/.

  init   Scaffold docs/raw/ (dropzone) + docs/wiki/ (index, log, pages/).
  lint   Report broken wikilinks, orphan pages, index/log desync, unprocessed sources.

Flags: --root DIR, --force (init), --json (lint).`)
}

func wikiInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("wiki init", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	if err := fs.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	created, err := scaffoldWiki(r, force, templates)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK(fmt.Sprintf("Wiki scaffolded at %s (%d file(s) created).", workspace.Relative(r, paths.DocsWiki(r)), created))
	render.Info("Drop sources into docs/raw/, then use the `wiki` skill's Ingest workflow.")
	return 0
}

// scaffoldWiki lays down the docs/raw/ + docs/wiki/ structure idempotently. It
// never overwrites an existing file without --force and never touches docs/raw/
// contents (R9.3, R9.4). Returns the number of files created.
func scaffoldWiki(root string, force bool, templates embed.FS) (int, error) {
	// The pages/ content home is a directory the LLM fills.
	if err := mkdirAll(filepath.Join(paths.DocsWiki(root), "pages")); err != nil {
		return 0, err
	}
	if err := mkdirAll(paths.DocsRaw(root)); err != nil {
		return 0, err
	}
	files, err := templater.WikiScaffoldFiles(templates)
	if err != nil {
		return 0, err
	}
	// Install the wiki schema doc — the `wiki` skill plus the knowledge-base
	// steering rule — through the existing skill/rule mechanisms (R9.2).
	if schema, err := wikiSchemaFiles(templates); err != nil {
		return 0, err
	} else {
		for rel, content := range schema {
			files[rel] = content
		}
	}
	created := 0
	for rel, content := range files {
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if force {
			if err := workspace.WriteFile(dst, content, true); err != nil {
				return created, err
			}
			created++
			continue
		}
		wrote, err := workspace.SafeWrite(dst, content)
		if err != nil {
			return created, err
		}
		if wrote {
			created++
		}
	}
	return created, nil
}

// wikiSchemaFiles returns the wiki skills (.claude/skills/{wiki,codewiki}/*) and
// the knowledge-base steering rule (.claude/rules/knowledge-base.md), keyed by
// workspace-relative destination — the "schema doc" R9.2 installs.
//
// codewiki ships beside wiki because it is the dropzone's other half: wiki
// ingests what you drop, codewiki is how a *source checkout* — a whole
// repository, not an article — becomes something droppable in the first place.
// Scaffolding one without the other leaves the docs/raw/ README describing a
// workflow the workspace cannot run.
func wikiSchemaFiles(templates embed.FS) (map[string]string, error) {
	out := map[string]string{}
	skills, err := templater.SkillFiles(templates)
	if err != nil {
		return nil, err
	}
	for rel, content := range skills {
		if strings.HasPrefix(rel, "wiki/") || strings.HasPrefix(rel, "codewiki/") {
			out[".claude/skills/"+rel] = content
		}
	}
	rules, err := templater.RuleFiles(templates)
	if err != nil {
		return nil, err
	}
	if content, ok := rules["knowledge-base.md"]; ok {
		out[".claude/rules/knowledge-base.md"] = content
	}
	return out, nil
}

func wikiLint(args []string) int {
	fs := flag.NewFlagSet("wiki lint", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	if err := fs.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	g, r, code := loadGraph(root)
	if code != 0 {
		return code
	}
	report := graph.Analyze(g, r)
	findings := graph.WikiFindings(report)
	if jsonOut {
		_ = emitJSON(map[string]any{"findings": findings})
		if len(findings) > 0 {
			return 2
		}
		return 0
	}
	if len(findings) == 0 {
		render.OK("Wiki is healthy: no findings.")
		return 0
	}
	renderFindings(findings)
	render.Warn(fmt.Sprintf("%d wiki finding(s).", len(findings)))
	return 2 // R10.6: exit non-zero on findings (CI-gateable)
}
