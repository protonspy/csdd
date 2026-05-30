package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/livelo/csdd/internal/paths"
	"github.com/livelo/csdd/internal/render"
	"github.com/livelo/csdd/internal/validator"
	"github.com/livelo/csdd/internal/workspace"
)

// MCPConfig mirrors .mcp.json at the repo root — the workspace-scope Model
// Context Protocol server configuration agents read on every session.
type MCPConfig struct {
	MCPServers map[string]MCPServer `json:"mcpServers"`
}

// MCPServer is one entry under mcpServers. A stdio server uses Command/Args;
// a remote server uses URL/Type. Every field is omitempty so the on-disk file
// stays minimal and reviewable.
type MCPServer struct {
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	URL         string            `json:"url,omitempty"`
	Type        string            `json:"type,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
	AutoApprove []string          `json:"autoApprove,omitempty"`
}

// mcpRemoteTypes enumerates the valid transports for a remote (URL) server.
var mcpRemoteTypes = []string{"sse", "http"}

func runMCP(args []string) int {
	action, rest, err := parseAction("mcp", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	switch action {
	case "add":
		return mcpAdd(rest)
	case "list":
		return mcpList(rest)
	case "show":
		return mcpShow(rest)
	case "remove":
		return mcpRemove(rest)
	case "enable":
		return mcpToggle(rest, false)
	case "disable":
		return mcpToggle(rest, true)
	case "validate":
		return mcpValidate(rest)
	default:
		render.Err("unknown mcp action: " + action)
		return 1
	}
}

// mcpFile resolves the path to the repo-root .mcp.json. The file need not exist
// yet — loadMCP treats a missing file as an empty config so add/list can proceed.
func mcpFile(root string) (string, error) {
	return paths.MCP(root), nil
}

// loadMCP reads mcp.json. A missing file yields an empty config so list/add can
// proceed; a malformed file is a hard error (callers must fix it).
func loadMCP(path string) (MCPConfig, error) {
	var cfg MCPConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.MCPServers = map[string]MCPServer{}
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("mcp.json is not valid JSON: %w", err)
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]MCPServer{}
	}
	return cfg, nil
}

func saveMCP(path string, cfg MCPConfig) error {
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]MCPServer{}
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// MCPAddOptions is the headless equivalent shared by the CLI and the TUI.
type MCPAddOptions struct {
	Root        string
	Name        string
	Command     string
	Args        []string
	Env         []string // raw "K=V" pairs; parsed in MCPAdd
	URL         string
	Type        string
	Disabled    bool
	AutoApprove []string
	Force       bool
}

func mcpAdd(args []string) int {
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	var opts MCPAddOptions
	var argv, env, auto stringSliceFlag
	addRoot(fs, &opts.Root)
	fs.StringVar(&opts.Command, "command", "", "Executable for a stdio server.")
	fs.Var(&argv, "arg", "Argument for the command (repeatable).")
	fs.Var(&env, "env", "Environment variable K=V (repeatable).")
	fs.StringVar(&opts.URL, "url", "", "Endpoint for a remote server.")
	fs.StringVar(&opts.Type, "type", "", "Remote transport: sse|http (default http when --url is set).")
	fs.BoolVar(&opts.Disabled, "disabled", false, "Add the server in a disabled state.")
	fs.Var(&auto, "auto-approve", "Tool to auto-approve (repeatable; least privilege: avoid).")
	addForce(fs, &opts.Force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd mcp add NAME (--command CMD [--arg A]... | --url URL [--type sse|http]) [--env K=V]... [--disabled] [--force]")
		return 1
	}
	opts.Name = positionals[0]
	opts.Args = argv.values
	opts.Env = env.values
	opts.AutoApprove = auto.values
	if err := MCPAdd(opts); err != nil {
		render.Err(err.Error())
		return 1
	}
	return 0
}

// MCPAdd inserts or replaces a server entry in mcp.json.
func MCPAdd(opts MCPAddOptions) error {
	if err := workspace.KebabCheck(opts.Name, "mcp server"); err != nil {
		return err
	}
	r, err := workspace.Resolve(opts.Root)
	if err != nil {
		return err
	}
	path, err := mcpFile(r)
	if err != nil {
		return err
	}
	cfg, err := loadMCP(path)
	if err != nil {
		return err
	}
	if _, exists := cfg.MCPServers[opts.Name]; exists && !opts.Force {
		return fmt.Errorf("mcp server already exists: %s (pass --force to replace)", opts.Name)
	}
	srv, err := buildMCPServer(opts)
	if err != nil {
		return err
	}
	cfg.MCPServers[opts.Name] = srv
	if err := saveMCP(path, cfg); err != nil {
		return err
	}
	transport := "stdio"
	if srv.URL != "" {
		transport = srv.Type
	}
	render.OK("added mcp server '" + opts.Name + "' (" + transport + ") to " + workspace.Relative(r, path))
	if len(srv.AutoApprove) > 0 {
		render.Warn("autoApprove set on '" + opts.Name + "' — auto-approving tools bypasses human review; keep it minimal")
	}
	return nil
}

// buildMCPServer validates the transport choice and assembles the entry.
func buildMCPServer(opts MCPAddOptions) (MCPServer, error) {
	var srv MCPServer
	hasCmd := strings.TrimSpace(opts.Command) != ""
	hasURL := strings.TrimSpace(opts.URL) != ""
	if hasCmd == hasURL {
		return srv, fmt.Errorf("specify exactly one of --command (stdio) or --url (remote)")
	}
	env, err := parseEnvKV(opts.Env)
	if err != nil {
		return srv, err
	}
	if hasURL {
		t := opts.Type
		if t == "" {
			t = "http"
		}
		if !containsString(mcpRemoteTypes, t) {
			return srv, fmt.Errorf("--type must be one of %v for a remote server", mcpRemoteTypes)
		}
		srv.URL = opts.URL
		srv.Type = t
	} else {
		if opts.Type != "" && opts.Type != "stdio" {
			return srv, fmt.Errorf("--type %q is only valid with --url; stdio servers omit it", opts.Type)
		}
		srv.Command = opts.Command
		srv.Args = opts.Args
	}
	srv.Env = env
	srv.Disabled = opts.Disabled
	srv.AutoApprove = opts.AutoApprove
	return srv, nil
}

func parseEnvKV(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, p := range pairs {
		i := strings.Index(p, "=")
		if i <= 0 {
			return nil, fmt.Errorf("--env must be in K=V form: got %q", p)
		}
		key := strings.TrimSpace(p[:i])
		if key == "" {
			return nil, fmt.Errorf("--env key is empty in %q", p)
		}
		out[key] = p[i+1:]
	}
	return out, nil
}

func mcpList(args []string) int {
	r, _, code := mcpResolveAndLoad(args)
	if code != 0 {
		return code
	}
	cfg := r.cfg
	if len(cfg.MCPServers) == 0 {
		render.Info("no mcp servers configured")
		return 0
	}
	names := sortedServerNames(cfg)
	maxName := len("name")
	for _, n := range names {
		if len(n) > maxName {
			maxName = len(n)
		}
	}
	fmt.Printf("  %-*s  %-7s  %-9s  %s\n", maxName, "name", "type", "state", "endpoint")
	for _, n := range names {
		s := cfg.MCPServers[n]
		transport := "stdio"
		endpoint := s.Command
		if len(s.Args) > 0 {
			endpoint += " " + strings.Join(s.Args, " ")
		}
		if s.URL != "" {
			transport = s.Type
			endpoint = s.URL
		}
		// Pad the plain state first, then colorize, so ANSI codes don't break
		// column alignment.
		state := fmt.Sprintf("%-9s", "enabled")
		state = render.Green(state)
		if s.Disabled {
			state = render.Yellow(fmt.Sprintf("%-9s", "disabled"))
		}
		fmt.Printf("  %-*s  %-7s  %s  %s\n", maxName, n, transport, state, truncateStr(endpoint, 60))
	}
	return 0
}

func mcpShow(args []string) int {
	res, name, code := mcpResolveLoadNamed(args, "show")
	if code != 0 {
		return code
	}
	s, ok := res.cfg.MCPServers[name]
	if !ok {
		render.Err("mcp server not found: " + name)
		return 1
	}
	b, err := json.MarshalIndent(map[string]MCPServer{name: s}, "", "  ")
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	fmt.Println(render.Bold(name))
	fmt.Println(string(b))
	if len(s.AutoApprove) > 0 {
		render.Warn("autoApprove bypasses human review for: " + strings.Join(s.AutoApprove, ", "))
	}
	return 0
}

func mcpRemove(args []string) int {
	fs := flag.NewFlagSet("mcp remove", flag.ContinueOnError)
	var root string
	var force bool
	addRoot(fs, &root)
	addForce(fs, &force)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd mcp remove NAME --force")
		return 1
	}
	name := positionals[0]
	if !force {
		render.Err("refusing to remove mcp server '" + name + "' without --force")
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	path, err := mcpFile(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	cfg, err := loadMCP(path)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if _, ok := cfg.MCPServers[name]; !ok {
		render.Err("mcp server not found: " + name)
		return 1
	}
	delete(cfg.MCPServers, name)
	if err := saveMCP(path, cfg); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("removed mcp server '" + name + "'")
	return 0
}

func mcpToggle(args []string, disabled bool) int {
	verb := "enable"
	if disabled {
		verb = "disable"
	}
	fs := flag.NewFlagSet("mcp "+verb, flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd mcp " + verb + " NAME")
		return 1
	}
	name := positionals[0]
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	path, err := mcpFile(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	cfg, err := loadMCP(path)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	s, ok := cfg.MCPServers[name]
	if !ok {
		render.Err("mcp server not found: " + name)
		return 1
	}
	s.Disabled = disabled
	cfg.MCPServers[name] = s
	if err := saveMCP(path, cfg); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("mcp server '" + name + "' " + verb + "d")
	return 0
}

func mcpValidate(args []string) int {
	res, _, code := mcpResolveAndLoad(args)
	if code != 0 {
		return code
	}
	issues := validateMCPConfig(res.cfg)
	if len(issues) == 0 {
		render.OK(fmt.Sprintf("mcp.json valid (%d server(s))", len(res.cfg.MCPServers)))
		return 0
	}
	for _, i := range issues {
		render.Err(i.String())
	}
	return 2
}

// validateMCPConfig runs the mechanical checks on a parsed config.
func validateMCPConfig(cfg MCPConfig) []validator.Issue {
	var issues []validator.Issue
	for _, name := range sortedServerNames(cfg) {
		s := cfg.MCPServers[name]
		if strings.TrimSpace(name) == "" {
			issues = append(issues, validator.Issue{File: "mcp.json", Msg: "server name must not be empty"})
			continue
		}
		hasCmd := strings.TrimSpace(s.Command) != ""
		hasURL := strings.TrimSpace(s.URL) != ""
		switch {
		case hasCmd && hasURL:
			issues = append(issues, validator.Issue{File: "mcp.json", Msg: fmt.Sprintf("server '%s' sets both command and url; pick one transport", name)})
		case !hasCmd && !hasURL:
			issues = append(issues, validator.Issue{File: "mcp.json", Msg: fmt.Sprintf("server '%s' has neither command (stdio) nor url (remote)", name)})
		case hasURL && !containsString(mcpRemoteTypes, s.Type):
			issues = append(issues, validator.Issue{File: "mcp.json", Msg: fmt.Sprintf("server '%s' remote transport must be sse|http (got %q)", name, s.Type)})
		case hasCmd && s.Type != "" && s.Type != "stdio":
			issues = append(issues, validator.Issue{File: "mcp.json", Msg: fmt.Sprintf("server '%s' is stdio but sets type=%q", name, s.Type)})
		}
		for k := range s.Env {
			if strings.TrimSpace(k) == "" {
				issues = append(issues, validator.Issue{File: "mcp.json", Msg: fmt.Sprintf("server '%s' has an empty env key", name)})
			}
		}
	}
	return issues
}

func sortedServerNames(cfg MCPConfig) []string {
	names := make([]string, 0, len(cfg.MCPServers))
	for n := range cfg.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// mcpResult bundles a resolved root + loaded config for the read commands.
type mcpResult struct {
	root string
	cfg  MCPConfig
}

func mcpResolveAndLoad(args []string) (mcpResult, []string, int) {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	var root string
	addRoot(fs, &root)
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return mcpResult{}, nil, failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return mcpResult{}, nil, 1
	}
	path, err := mcpFile(r)
	if err != nil {
		render.Err(err.Error())
		return mcpResult{}, nil, 1
	}
	cfg, err := loadMCP(path)
	if err != nil {
		render.Err(err.Error())
		return mcpResult{}, nil, 1
	}
	return mcpResult{root: r, cfg: cfg}, positionals, 0
}

func mcpResolveLoadNamed(args []string, action string) (mcpResult, string, int) {
	res, positionals, code := mcpResolveAndLoad(args)
	if code != 0 {
		return res, "", code
	}
	if len(positionals) < 1 {
		render.Err("usage: csdd mcp " + action + " NAME")
		return res, "", 1
	}
	return res, positionals[0], 0
}
