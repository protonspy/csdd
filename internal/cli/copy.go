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

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/workspace"
)

// copyKind describes one shipped artifact collection `csdd copy` can pull a named
// item from: how to load its embedded source, where it lands on disk, and how to
// treat its entries. The set mirrors the artifact trees `csdd init` scaffolds and
// `init --exclude` can skip, so a workspace can start bare and cherry-pick.
type copyKind struct {
	name   string
	loader func(fs.FS) (map[string]string, error) // rel-path → content, relative to dest
	dest   func(root string) string               // on-disk destination directory
	tree   bool                                   // items are directories (skills); listed by top dir
	exec   bool                                   // chmod 0755 written files (hooks)
}

// copyKinds is the ordered set of copyable artifact collections.
func copyKinds() []copyKind {
	return []copyKind{
		{"rules", templater.RuleFiles, paths.Rules, false, false},
		{"skills", templater.SkillFiles, paths.Skills, true, false},
		{"agents", templater.AgentFiles, paths.Agents, false, false},
		{"commands", templater.CommandFiles, paths.Commands, false, false},
		{"hooks", templater.HookFiles, paths.Hooks, false, true},
		{"templates", templater.WorkflowTemplateFiles, paths.Templates, false, false},
		{"steering", steeringBaselineFiles, paths.Steering, false, false},
	}
}

// runCopy implements `csdd copy <kind>/<name>`, which writes one shipped artifact
// (or a whole sub-group) into the workspace. With no argument it lists everything
// copyable; with a bare kind it lists that kind. Refuses to clobber without --force.
func runCopy(args []string, templates embed.FS) int {
	fset := flag.NewFlagSet("copy", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fset, &root)
	addForce(fset, &force)
	if err := fset.Parse(args); err != nil {
		return failOnFlagParse(err)
	}

	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if info, statErr := os.Stat(paths.Claude(r)); statErr != nil || !info.IsDir() {
		render.Err("not a csdd workspace (no .claude/ at " + r + "). Run `" + prog() + " init` first.")
		return 1
	}

	kinds := copyKinds()
	rest := fset.Args()
	if len(rest) == 0 {
		return listCopyable(templates, kinds, "")
	}

	kindName, name, hasSlash := strings.Cut(rest[0], "/")
	kind, ok := findCopyKind(kinds, kindName)
	if !ok {
		render.Err(fmt.Sprintf("unknown artifact kind %q; valid: %s", kindName, strings.Join(copyKindNames(kinds), ", ")))
		return 1
	}
	if !hasSlash || name == "" {
		// A bare kind (e.g. `csdd copy skills`) lists that kind's items.
		return listCopyable(templates, kinds, kindName)
	}

	entries, err := kind.loader(templates)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	selected := selectCopyEntries(entries, name)
	if len(selected) == 0 {
		render.Err(fmt.Sprintf("no %s named %q. Available: %s", kindName, name, strings.Join(copyItemNames(kind, entries), ", ")))
		return 1
	}

	destDir := kind.dest(r)
	// Refuse to clobber any existing file unless --force, before writing anything.
	if !force {
		for rel := range selected {
			target := filepath.Join(destDir, filepath.FromSlash(rel))
			if pathExists(target) {
				render.Err(workspace.Relative(r, target) + " already exists. Pass --force to overwrite.")
				return 1
			}
		}
	}

	var written []string
	for rel, content := range selected {
		target := filepath.Join(destDir, filepath.FromSlash(rel))
		if err := workspace.WriteFile(target, content, force); err != nil {
			render.Err(err.Error())
			return 1
		}
		if kind.exec {
			_ = os.Chmod(target, 0o755)
		}
		written = append(written, workspace.Relative(r, target))
	}
	sort.Strings(written)
	render.OK(fmt.Sprintf("copied %s/%s (%d file(s))", kindName, name, len(written)))
	for _, w := range written {
		render.Info("  " + w)
	}
	return 0
}

// listCopyable prints every copyable item, or just those of `only` when set.
func listCopyable(templates embed.FS, kinds []copyKind, only string) int {
	render.Info("Copy a shipped artifact into this workspace with `" + prog() + " copy <kind>/<name>`:")
	for _, k := range kinds {
		if only != "" && k.name != only {
			continue
		}
		entries, err := k.loader(templates)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
		render.Info(k.name + ":")
		for _, n := range copyItemNames(k, entries) {
			render.Info("  " + k.name + "/" + n)
		}
	}
	return 0
}

// selectCopyEntries returns the embedded entries that belong to item `name`: an
// exact file (agents/rules/hooks), the file with a `.md` suffix, or every entry
// under `name/` (a skill tree, or a template sub-group like `specs`).
func selectCopyEntries(entries map[string]string, name string) map[string]string {
	sel := map[string]string{}
	for rel, content := range entries {
		if rel == name || rel == name+".md" || strings.HasPrefix(rel, name+"/") {
			sel[rel] = content
		}
	}
	return sel
}

// copyItemNames lists the distinct item names within a kind, in sorted order.
func copyItemNames(k copyKind, entries map[string]string) []string {
	set := map[string]bool{}
	for rel := range entries {
		if k.tree {
			set[strings.SplitN(rel, "/", 2)[0]] = true
		} else {
			set[strings.TrimSuffix(rel, ".md")] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func findCopyKind(kinds []copyKind, name string) (copyKind, bool) {
	for _, k := range kinds {
		if k.name == name {
			return k, true
		}
	}
	return copyKind{}, false
}

func copyKindNames(kinds []copyKind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = k.name
	}
	return out
}

// steeringBaselineFiles loads the shipped baseline steering documents (product,
// tech, structure, …) as filename → content, so `csdd copy steering/<name>` drops
// a starter steering file into .claude/steering/. It excludes custom.md, which is
// a render template (carries {{...}} directives), not a clean baseline.
func steeringBaselineFiles(efs fs.FS) (map[string]string, error) {
	const dir = "templates/steering"
	entries, err := fs.ReadDir(efs, dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		if name == "custom.md" {
			continue
		}
		raw, err := fs.ReadFile(efs, dir+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		out[name] = string(raw)
	}
	return out, nil
}
