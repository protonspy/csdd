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
	"github.com/livelo/csdd/internal/paths"
	"github.com/livelo/csdd/internal/render"
	"github.com/livelo/csdd/internal/templater"
	"github.com/livelo/csdd/internal/validator"
	"github.com/livelo/csdd/internal/workspace"
)

func runSteering(args []string, templates embed.FS) int {
	action, rest, err := parseAction("steering", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	switch action {
	case "init":
		return steeringInit(rest, templates)
	case "create":
		return steeringCreate(rest, templates)
	case "list":
		return steeringList(rest)
	case "show":
		return steeringShow(rest)
	case "delete":
		return steeringDelete(rest)
	case "validate":
		return steeringValidate(rest)
	default:
		render.Err("unknown steering action: " + action)
		return 1
	}
}

func steeringInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("steering init", flag.ContinueOnError)
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
	target := paths.Steering(r)
	if err := mkdirAll(target); err != nil {
		render.Err(err.Error())
		return 1
	}
	created := 0
	var names []string
	for name, tpl := range standardSteeringTemplates() {
		content, err := templater.Static(templates, tpl)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
		path := filepath.Join(target, name)
		ok, err := workspace.SafeWrite(path, content)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
		rel := workspace.Relative(r, path)
		if ok {
			render.OK("created " + rel)
			created++
		} else {
			render.Info("skipped " + rel + " (exists)")
		}
		names = append(names, name)
	}
	render.Info("standard steering files created: " + intStr(created))
	if added, err := ensureSteeringImports(r, names...); err != nil {
		render.Warn("could not update CLAUDE.md imports: " + err.Error())
	} else if added > 0 {
		render.OK("imported " + intStr(added) + " steering file(s) into CLAUDE.md")
	}
	return 0
}

// SteeringCreateOptions captures every input the steering create flow needs.
// The TUI fills this struct from wizard answers and calls SteeringCreate so
// both surfaces share identical validation and output.
type SteeringCreateOptions struct {
	Root        string
	Name        string
	Inclusion   string
	Patterns    []string
	Description string
	Title       string
	Force       bool
}

func steeringCreate(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("steering create", flag.ContinueOnError)
	var opts SteeringCreateOptions
	var patterns stringSliceFlag
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Inclusion, "inclusion", "", "Required: always|fileMatch|manual|auto")
	fs.Var(&patterns, "pattern", "Glob pattern (repeatable; required for inclusion=fileMatch).")
	fs.StringVar(&opts.Description, "description", "", "Required for inclusion=auto.")
	fs.StringVar(&opts.Title, "title", "", "Document title (default: derived from name).")
	addForce(fs, &opts.Force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd steering create NAME --inclusion {always|fileMatch|manual|auto} [...]")
		return 1
	}
	opts.Name = positionals[0]
	opts.Patterns = patterns.values
	if err := SteeringCreate(templates, opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// SteeringCreate is the headless entry point shared by the CLI and the TUI.
func SteeringCreate(templates embed.FS, opts SteeringCreateOptions) error {
	r, err := workspace.Resolve(opts.Root)
	if err != nil {
		return err
	}
	if _, err := workspace.SteeringDir(r); err != nil {
		return err
	}
	if err := workspace.KebabCheck(opts.Name, "steering"); err != nil {
		return err
	}
	if !containsString(workspace.InclusionModes, opts.Inclusion) {
		return fmt.Errorf("--inclusion must be one of %v", workspace.InclusionModes)
	}
	switch opts.Inclusion {
	case "fileMatch":
		if len(opts.Patterns) == 0 {
			return fmt.Errorf("inclusion=fileMatch requires at least one --pattern")
		}
	case "auto":
		if opts.Description == "" {
			return fmt.Errorf("inclusion=auto requires --description (description triggers the auto-load)")
		}
	}
	title := opts.Title
	if title == "" {
		title = titleCase(opts.Name)
	}
	data := struct {
		Inclusion   string
		Patterns    []string
		AutoName    string
		Description string
		Title       string
	}{
		Inclusion:   opts.Inclusion,
		Patterns:    opts.Patterns,
		Description: opts.Description,
		Title:       title,
	}
	if opts.Inclusion == "auto" {
		data.AutoName = opts.Name
	}
	content, err := templater.Render(templates, "templates/steering/custom.md.tmpl", data)
	if err != nil {
		return err
	}
	target := filepath.Join(paths.Steering(r), opts.Name+".md")
	if err := workspace.WriteFile(target, content, opts.Force); err != nil {
		return err
	}
	render.OK("created " + workspace.Relative(r, target) + " (inclusion=" + opts.Inclusion + ")")
	if added, err := ensureSteeringImports(r, opts.Name+".md"); err != nil {
		render.Warn("could not update CLAUDE.md imports: " + err.Error())
	} else if added > 0 {
		render.Info("imported " + opts.Name + ".md into CLAUDE.md (always-on)")
	}
	return nil
}

func steeringList(args []string) int {
	fs := flag.NewFlagSet("steering list", flag.ContinueOnError)
	var root, incFilter string
	addRoot(fs, &root)
	fs.StringVar(&incFilter, "inclusion", "", "Filter by inclusion mode.")
	_, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir, err := workspace.SteeringDir(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	entries, err := os.ReadDir(sdir)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	type row struct{ name, inc, extras string }
	var rows []row
	maxName := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sdir, e.Name()))
		if err != nil {
			continue
		}
		fm := frontmatter.Parse(string(data))
		inc := fm.AsString("inclusion", "—")
		if incFilter != "" && inc != incFilter {
			continue
		}
		extras := ""
		switch inc {
		case "fileMatch":
			extras = strings.Join(fm.AsStringSlice("fileMatchPattern"), ", ")
		case "auto":
			extras = truncateStr(fm.AsString("description", ""), 60)
		}
		rows = append(rows, row{e.Name(), inc, extras})
		if len(e.Name()) > maxName {
			maxName = len(e.Name())
		}
	}
	if len(rows) == 0 {
		render.Info("no steering files found")
		return 0
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, r := range rows {
		fmt.Printf("  %-*s  %-12s  %s\n", maxName, r.name, render.Cyan(r.inc), r.extras)
	}
	return 0
}

func steeringShow(args []string) int {
	fs := flag.NewFlagSet("steering show", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd steering show NAME")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir, err := workspace.SteeringDir(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	target := filepath.Join(sdir, positionals[0]+".md")
	data, err := os.ReadFile(target)
	if err != nil {
		render.Err("steering file not found: " + workspace.Relative(r, target))
		return 1
	}
	fmt.Print(string(data))
	return 0
}

func steeringDelete(args []string) int {
	fs := flag.NewFlagSet("steering delete", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd steering delete NAME")
		return 1
	}
	name := positionals[0]
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir, err := workspace.SteeringDir(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	target := filepath.Join(sdir, name+".md")
	if _, err := os.Stat(target); err != nil {
		render.Err("steering file not found: " + workspace.Relative(r, target))
		return 1
	}
	if (name == "product" || name == "tech" || name == "structure") && !force {
		render.Err("refusing to delete foundational steering '" + name + "' without --force")
		return 1
	}
	if err := os.Remove(target); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("deleted " + workspace.Relative(r, target))
	return 0
}

func steeringValidate(args []string) int {
	fs := flag.NewFlagSet("steering validate", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir, err := workspace.SteeringDir(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	name := ""
	if len(positionals) >= 1 {
		name = positionals[0]
	}
	issues, err := validator.ValidateSteering(sdir, name)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if len(issues) == 0 {
		render.OK("all steering files valid")
		return 0
	}
	for _, i := range issues {
		render.Err(i.String())
	}
	return 2
}

// titleCase converts kebab-case to Title Case (used for default titles).
func titleCase(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// truncateStr caps s to n runes (not bytes) so multi-byte UTF-8 characters
// in descriptions are never split mid-codepoint.
func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
