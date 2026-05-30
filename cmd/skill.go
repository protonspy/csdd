package cmd

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/livelo/csdd/internal/frontmatter"
	"github.com/livelo/csdd/internal/render"
	"github.com/livelo/csdd/internal/templater"
	"github.com/livelo/csdd/internal/validator"
	"github.com/livelo/csdd/internal/workspace"
)

func runSkill(args []string, templates embed.FS) int {
	action, rest, err := parseAction("skill", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	switch action {
	case "create":
		return skillCreate(rest, templates)
	case "list":
		return skillList(rest)
	case "show":
		return skillShow(rest)
	case "add-reference":
		return skillAddArtifact(rest, "references")
	case "add-script":
		return skillAddArtifact(rest, "scripts")
	case "add-asset":
		return skillAddArtifact(rest, "assets")
	case "validate":
		return skillValidate(rest)
	case "delete":
		return skillDelete(rest)
	default:
		render.Err("unknown skill action: " + action)
		return 1
	}
}

// SkillCreateOptions is the headless equivalent shared with the TUI.
// Claude Code places skills exclusively in .claude/skills/, so there
// is no platform field — the canonical layout is intentional.
type SkillCreateOptions struct {
	Root        string
	Name        string
	Description string
	Title       string
}

func skillCreate(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("skill create", flag.ContinueOnError)
	var opts SkillCreateOptions
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Description, "description", "", "One-sentence activation trigger.")
	fs.StringVar(&opts.Title, "title", "", "Document title (default: derived from name).")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd skill create NAME --description '...'")
		return 1
	}
	opts.Name = positionals[0]
	if err := SkillCreate(templates, opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// SkillCreate creates the skill directory with SKILL.md plus references/, assets/, scripts/.
func SkillCreate(templates embed.FS, opts SkillCreateOptions) error {
	if opts.Description == "" {
		return fmt.Errorf("--description is required")
	}
	if err := workspace.KebabCheck(opts.Name, "skill"); err != nil {
		return err
	}
	r, err := workspace.Resolve(opts.Root)
	if err != nil {
		return err
	}
	base := workspace.SkillRoot(r)
	target := filepath.Join(base, opts.Name)
	if pathExists(target) {
		return fmt.Errorf("skill already exists: %s", workspace.Relative(r, target))
	}
	for _, sub := range []string{"references", "assets", "scripts"} {
		if err := mkdirAll(filepath.Join(target, sub)); err != nil {
			return err
		}
	}
	title := opts.Title
	if title == "" {
		title = titleCase(opts.Name)
	}
	content, err := templater.Render(templates, "templates/skill/SKILL.md.tmpl", map[string]string{
		"Name":        opts.Name,
		"Description": opts.Description,
		"Title":       title,
	})
	if err != nil {
		return err
	}
	if err := workspace.WriteFile(filepath.Join(target, "SKILL.md"), content, false); err != nil {
		return err
	}
	render.OK("created " + workspace.Relative(r, target) + "/SKILL.md")
	render.Info("structure: SKILL.md, references/, assets/, scripts/")
	return nil
}

func skillList(args []string) int {
	fs := flag.NewFlagSet("skill list", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	_, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	base := workspace.SkillRoot(r)
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) == 0 {
		render.Info("no skills found")
		return 0
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		render.Info("no skills found")
		return 0
	}
	sort.Strings(dirs)
	fmt.Println(render.Bold(workspace.Relative(r, base)))
	for _, name := range dirs {
		sf := filepath.Join(base, name, "SKILL.md")
		desc := ""
		if data, err := os.ReadFile(sf); err == nil {
			desc = truncateStr(frontmatter.Parse(string(data)).AsString("description", ""), 80)
		}
		fmt.Printf("  - %-28s %s\n", name, desc)
	}
	return 0
}

func findSkill(root, name string) (string, bool) {
	base := workspace.SkillRoot(root)
	candidate := filepath.Join(base, name)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate, true
	}
	return "", false
}

func skillShow(args []string) int {
	fs := flag.NewFlagSet("skill show", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd skill show NAME")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir, ok := findSkill(r, positionals[0])
	if !ok {
		render.Err("skill not found: " + positionals[0])
		return 1
	}
	fmt.Println(render.Bold(workspace.Relative(r, sdir)))
	filepath.Walk(sdir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(sdir, path)
		fmt.Println("  - " + rel)
		return nil
	})
	fmt.Println()
	if data, err := os.ReadFile(filepath.Join(sdir, "SKILL.md")); err == nil {
		fmt.Print(string(data))
	}
	return 0
}

func skillAddArtifact(args []string, subdir string) int {
	fs := flag.NewFlagSet("skill add-"+subdir, flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 2 {
		render.Err("usage: csdd skill add-" + subdir + " SKILL FILE")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir, ok := findSkill(r, positionals[0])
	if !ok {
		render.Err("skill not found: " + positionals[0])
		return 1
	}
	dest, err := safeSkillChildPath(filepath.Join(sdir, subdir), positionals[1])
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if pathExists(dest) {
		render.Err("already exists: " + workspace.Relative(r, dest))
		return 1
	}
	if err := mkdirAll(filepath.Dir(dest)); err != nil {
		render.Err(err.Error())
		return 1
	}
	var placeholder string
	if subdir == "scripts" {
		placeholder = "#!/usr/bin/env bash\n# Deterministic, tested logic the agent invokes\n"
	} else {
		single := subdir
		if len(single) > 0 {
			single = single[:len(single)-1]
		}
		placeholder = "# " + positionals[1] + "\n\n[Populate this " + single + " with real expertise — not generic content.]\n"
	}
	if err := os.WriteFile(dest, []byte(placeholder), 0o644); err != nil {
		render.Err(err.Error())
		return 1
	}
	if subdir == "scripts" {
		_ = os.Chmod(dest, 0o755)
	}
	render.OK("created " + workspace.Relative(r, dest))
	return 0
}

func safeSkillChildPath(base, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("file path is required")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("file path must be relative to the skill subdirectory")
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("file path must stay within the skill subdirectory")
	}
	dest := filepath.Join(base, clean)
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	back, err := filepath.Rel(absBase, absDest)
	if err != nil {
		return "", err
	}
	if back == ".." || strings.HasPrefix(back, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("file path must stay within the skill subdirectory")
	}
	return dest, nil
}

func skillValidate(args []string) int {
	fs := flag.NewFlagSet("skill validate", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd skill validate NAME")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	name := positionals[0]
	sdir, ok := findSkill(r, name)
	if !ok {
		render.Err("skill not found: " + name)
		return 1
	}
	issues, lines, tokens := validator.ValidateSkill(sdir, name)
	if len(issues) == 0 {
		render.OK(fmt.Sprintf("%s: skill valid (%d lines, ~%d tokens)", name, lines, tokens))
		return 0
	}
	for _, i := range issues {
		render.Err(i.String())
	}
	return 2
}

func skillDelete(args []string) int {
	fs := flag.NewFlagSet("skill delete", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd skill delete NAME --force")
		return 1
	}
	if !force {
		render.Err("refusing to delete skill '" + positionals[0] + "' without --force")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir, ok := findSkill(r, positionals[0])
	if !ok {
		render.Err("skill not found: " + positionals[0])
		return 1
	}
	if err := os.RemoveAll(sdir); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("deleted " + workspace.Relative(r, sdir))
	return 0
}
