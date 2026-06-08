package cli

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/render"
)

// Preset is a named, pre-filled MCP server configuration that `mcp install`
// expands into an MCPAddOptions and hands to MCPAdd. Adding a preset is a single
// registry entry — there is no separate write path, so duplicate detection,
// transport validation, and the on-disk shape are identical to a manual add.
type Preset struct {
	Name      string   // server name written to .mcp.json (kebab-case)
	Summary   string   // one-line description shown by `mcp presets`
	Transport string   // display label: "http" | "sse" | "stdio"
	Command   string   // stdio only
	Args      []string // stdio only
	URL       string   // remote only
	Type      string   // remote only: "sse" | "http"
	Note      string   // optional hint emitted after install (e.g. auth caveat)
}

// mcpPresetRegistry holds the known servers `mcp install` can register. Keep the
// entries secret-free: a preset stores only non-sensitive connection details.
var mcpPresetRegistry = map[string]Preset{
	"context7": {
		Name:      "context7",
		Summary:   "Up-to-date library/API docs.",
		Transport: "http",
		URL:       "https://mcp.context7.com/mcp",
		Type:      "http",
	},
	"playwright": {
		Name:      "playwright",
		Summary:   "Browser automation for frontend e2e/QA.",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"@playwright/mcp@latest"},
	},
	"github": {
		Name:      "github",
		Summary:   "GitHub repos, PRs, issues, Actions.",
		Transport: "http",
		URL:       "https://api.githubcopilot.com/mcp/",
		Type:      "http",
		Note:      "Requires GitHub auth — authenticate via your client's OAuth on first use (or add a PAT). No token is stored by csdd.",
	},
}

// MCPPresets returns every preset sorted by name. The CLI listing and the TUI
// preset picker both read this, so neither can drift from the registry.
func MCPPresets() []Preset {
	out := make([]Preset, 0, len(mcpPresetRegistry))
	for _, p := range mcpPresetRegistry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// presetNames returns the preset names, sorted — used in error messages. It
// projects from MCPPresets() so there is a single sort path.
func presetNames() []string {
	presets := MCPPresets()
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

// MCPInstallPresetOptions is the headless input shared by the CLI and the TUI.
type MCPInstallPresetOptions struct {
	Root  string
	Names []string
	Force bool
}

// MCPInstallPreset expands each named preset to an MCPAddOptions and installs it
// via MCPAdd. Every name is validated before any write, so an unknown name in a
// multi-install leaves .mcp.json untouched. Each successful add prints its own
// render.OK; a preset Note is surfaced afterwards.
func MCPInstallPreset(opts MCPInstallPresetOptions) error {
	if len(opts.Names) == 0 {
		return fmt.Errorf("no preset name given")
	}
	for _, name := range opts.Names {
		if _, ok := mcpPresetRegistry[name]; !ok {
			return fmt.Errorf("unknown preset: %s (available: %s)", name, strings.Join(presetNames(), ", "))
		}
	}
	for _, name := range opts.Names {
		p := mcpPresetRegistry[name]
		add := MCPAddOptions{
			Root:    opts.Root,
			Name:    p.Name,
			Command: p.Command,
			Args:    p.Args,
			URL:     p.URL,
			Type:    p.Type,
			Force:   opts.Force,
		}
		if err := MCPAdd(add); err != nil {
			return err
		}
		if p.Note != "" {
			render.Info(p.Note)
		}
	}
	return nil
}

func mcpInstall(args []string) int {
	fs := flag.NewFlagSet("mcp install", flag.ContinueOnError)
	var opts MCPInstallPresetOptions
	addRoot(fs, &opts.Root)
	addForce(fs, &opts.Force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " mcp install NAME [NAME...] [--force]")
		return 1
	}
	opts.Names = positionals
	if err := MCPInstallPreset(opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

func mcpPresets(args []string) int {
	fs := flag.NewFlagSet("mcp presets", flag.ContinueOnError)
	var root string
	addRoot(fs, &root) // accepted for surface symmetry; the registry is static.
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	presets := MCPPresets()
	maxName := len("name")
	for _, p := range presets {
		if len(p.Name) > maxName {
			maxName = len(p.Name)
		}
	}
	fmt.Printf("  %-*s  %-7s  %s\n", maxName, "name", "type", "summary")
	for _, p := range presets {
		fmt.Printf("  %-*s  %-7s  %s\n", maxName, p.Name, p.Transport, p.Summary)
	}
	return 0
}
