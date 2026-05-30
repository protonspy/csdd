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
	"github.com/livelo/csdd/internal/workspace"
)

func runAgent(args []string, templates embed.FS) int {
	action, rest, err := parseAction("agent", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	switch action {
	case "create":
		return agentCreate(rest, templates)
	case "list":
		return agentList(rest)
	case "show":
		return agentShow(rest)
	case "delete":
		return agentDelete(rest)
	default:
		render.Err("unknown agent action: " + action)
		return 1
	}
}

// AgentCreateOptions captures every input the agent-create flow uses.
// Claude Code places custom sub-agents exclusively under .claude/agents/,
// mirroring the .claude/skills/ convention — no platform field by design.
type AgentCreateOptions struct {
	Root        string
	Name        string
	Description string
	Tools       []string
	Model       string
	Title       string
	Force       bool
}

func agentCreate(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("agent create", flag.ContinueOnError)
	var opts AgentCreateOptions
	var tools stringSliceFlag
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Description, "description", "", "When the orchestrator should pick this agent.")
	fs.Var(&tools, "tools", "Tool name (repeatable). Default: Read, Grep.")
	fs.StringVar(&opts.Model, "model", "", "Optional model override (e.g., sonnet, opus, haiku).")
	fs.StringVar(&opts.Title, "title", "", "Document title (default: derived from name).")
	addForce(fs, &opts.Force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd agent create NAME --description '...'")
		return 1
	}
	opts.Name = positionals[0]
	opts.Tools = tools.values
	if err := AgentCreate(templates, opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// AgentCreate creates a custom sub-agent definition with least-privilege defaults.
func AgentCreate(templates embed.FS, opts AgentCreateOptions) error {
	if opts.Description == "" {
		return fmt.Errorf("--description is required")
	}
	if err := workspace.KebabCheck(opts.Name, "agent"); err != nil {
		return err
	}
	r, err := workspace.Resolve(opts.Root)
	if err != nil {
		return err
	}
	base := workspace.AgentRoot(r)
	target := filepath.Join(base, opts.Name+".md")
	if pathExists(target) && !opts.Force {
		return fmt.Errorf("agent already exists: %s", workspace.Relative(r, target))
	}
	tools := opts.Tools
	if len(tools) == 0 {
		tools = []string{"Read", "Grep"}
	}
	toolsStr := strings.Join(tools, ", ")
	title := opts.Title
	if title == "" {
		title = titleCase(opts.Name)
	}
	content, err := templater.Render(templates, "templates/agent/agent.md.tmpl", map[string]string{
		"Name":        opts.Name,
		"Description": opts.Description,
		"Tools":       toolsStr,
		"Model":       opts.Model,
		"Title":       title,
	})
	if err != nil {
		return err
	}
	if err := workspace.WriteFile(target, content, opts.Force); err != nil {
		return err
	}
	render.OK("created " + workspace.Relative(r, target))
	render.Info("tools (least privilege): " + toolsStr)
	return nil
}

func agentList(args []string) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
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
	base := workspace.AgentRoot(r)
	entries, _ := os.ReadDir(base)
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		render.Info("no agents found")
		return 0
	}
	sort.Strings(files)
	fmt.Println(render.Bold(workspace.Relative(r, base)))
	for _, f := range files {
		data, _ := os.ReadFile(filepath.Join(base, f))
		fm := frontmatter.Parse(string(data))
		desc := truncateStr(fm.AsString("description", ""), 80)
		tools := fm.AsString("tools", "—")
		stem := strings.TrimSuffix(f, ".md")
		fmt.Printf("  - %-22s tools=[%s] %s\n", stem, tools, desc)
	}
	return 0
}

func findAgent(root, name string) (string, bool) {
	base := workspace.AgentRoot(root)
	candidate := filepath.Join(base, name+".md")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, true
	}
	return "", false
}

func agentShow(args []string) int {
	fs := flag.NewFlagSet("agent show", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd agent show NAME")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	target, ok := findAgent(r, positionals[0])
	if !ok {
		render.Err("agent not found: " + positionals[0])
		return 1
	}
	fmt.Println(render.Bold(workspace.Relative(r, target)))
	data, _ := os.ReadFile(target)
	fmt.Print(string(data))
	return 0
}

func agentDelete(args []string) int {
	fs := flag.NewFlagSet("agent delete", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd agent delete NAME --force")
		return 1
	}
	if !force {
		render.Err("refusing to delete agent '" + positionals[0] + "' without --force")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	target, ok := findAgent(r, positionals[0])
	if !ok {
		render.Err("agent not found: " + positionals[0])
		return 1
	}
	if err := os.Remove(target); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("deleted " + workspace.Relative(r, target))
	return 0
}
