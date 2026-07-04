// Package cli implements every CLI subcommand. The TUI is a layer above this
// package — both surfaces ultimately call the same operation helpers below,
// guaranteeing that an AI agent driving the binary by flags has access to
// every capability the TUI exposes.
package cli

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/protonspy/csdd/internal/render"
)

// version is the build version string. It defaults to "dev" for local builds
// and is overridden at release time via the linker:
//
//	go build -ldflags "-X github.com/protonspy/csdd/internal/cli.version=v1.2.3"
var version = "dev"

// Run dispatches `csdd <resource> <action> ...` to the appropriate handler.
// args is os.Args[1:]. templates carries the embedded template tree.
func Run(args []string, templates embed.FS) int {
	if len(args) == 0 {
		help(os.Stdout)
		return 0
	}
	var err error
	args, err = normalizeLeadingGlobalFlags(args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	resource := args[0]
	rest := args[1:]
	switch resource {
	case "-h", "--help", "help":
		help(os.Stdout)
		return 0
	case "-v", "--version", "version":
		fmt.Println("csdd " + version)
		return 0
	case "init":
		return runInit(rest, templates)
	case "update":
		return runUpdate(rest, templates)
	case "clean":
		return runClean(rest, templates)
	case "destroy", "uninstall":
		return runDestroy(rest, templates)
	case "steering":
		return runSteering(rest, templates)
	case "spec":
		return runSpec(rest, templates)
	case "skill":
		return runSkill(rest, templates)
	case "agent":
		return runAgent(rest, templates)
	case "mcp":
		return runMCP(rest)
	case "export":
		return runExport(rest)
	case "web", "--web":
		return runWeb(rest)
	case "tui":
		// main.go intercepts this so the TUI can be wired without a cmd → tui cycle.
		render.Err("internal: 'tui' must be handled by main")
		return 1
	default:
		render.Err("unknown command: " + resource)
		help(os.Stderr)
		return 1
	}
}

// normalizeLeadingGlobalFlags supports the documented form
// `csdd --root PATH <resource> <action>` while preserving the resource-level
// parsers as the single source of truth for flag semantics.
func normalizeLeadingGlobalFlags(args []string) ([]string, error) {
	var globals []string
	rest := args
	for len(rest) > 0 {
		switch rest[0] {
		case "--root":
			if len(rest) < 2 {
				return nil, fmt.Errorf("--root requires PATH")
			}
			globals = append(globals, rest[0], rest[1])
			rest = rest[2:]
		case "--force":
			globals = append(globals, rest[0])
			rest = rest[1:]
		default:
			if len(globals) == 0 {
				return args, nil
			}
			return append(append([]string{}, rest...), globals...), nil
		}
	}
	return args, nil
}

// prog is the program name echoed in help text and usage/guidance messages.
// The npm launcher sets CSDD_PROG to "npx @protonspy/csdd" when csdd is run via
// npx, so the help reflects the exact command the user typed; a global install
// or a plain `go`-built binary falls back to the bare name "csdd".
func prog() string {
	if p := os.Getenv("CSDD_PROG"); p != "" {
		return p
	}
	return "csdd"
}

func help(w *os.File) {
	fmt.Fprintln(w, strings.TrimSpace(helpText()))
}

func helpText() string {
	return fmt.Sprintf(`
csdd — manage Claude Code workflow artifacts (steering, specs, skills, custom agents).

USAGE
  %[1]s                       Launch the interactive TUI.
  %[1]s tui                   Launch the TUI explicitly.
  %[1]s <resource> <action> [flags]

RESOURCES
  init                        Bootstrap a Claude Code workspace.
  update                      Refresh csdd-managed artifacts to this version (confirms before overriding your edits; keeps them as .old).
  clean                       Remove the *-N.old backups update left under the workspace.
  destroy                     Tear the workspace back down (.claude/, CLAUDE.md, .mcp.json, pre-push); keeps specs/. Asks to confirm; --force to skip.
  steering {init,create,list,show,delete,validate}
  spec     {init,list,show,status,generate,approve,validate,test-report,delete}
  skill    {create,list,show,add-reference,add-script,add-asset,validate,delete}
  agent    {create,list,show,delete}
  mcp      {add,install,presets,list,show,remove,enable,disable,validate}
  export   {kiro,codex}                Convert the workspace to Kiro / Codex format.
  web                         Launch a read-only web dashboard (live spec progress + file viewer).

GLOBAL FLAGS
  --root PATH        Project root (default: nearest enclosing .claude/).
  --force            Override safety checks (delete, regenerate, approve-with-issues).
  -h, --help         Show this help.
  -v, --version      Show version.

EXAMPLES
  %[1]s init --with-baseline
  %[1]s update --dry-run                                     # preview what a new csdd version would change
  %[1]s update                                               # apply; your edits are kept as <file>-N.old
  %[1]s destroy --dry-run                                    # preview exactly what removing the workspace would delete
  %[1]s destroy --force                                      # remove the workspace for a fresh re-install (keeps specs/)
  %[1]s steering create api-conventions \
        --inclusion fileMatch --pattern 'src/api/**/*' --pattern '**/*Controller.*'
  %[1]s steering create observability \
        --inclusion auto --description 'Logging/metrics. Use when adding instrumentation.'
  %[1]s spec init photo-albums
  %[1]s spec generate photo-albums --artifact requirements
  %[1]s spec approve photo-albums --phase requirements
  %[1]s spec test-report photo-albums --junit junit.xml --coverage coverage/lcov.info   # → specs/<f>/test-report.json
  %[1]s spec test-report photo-albums --lang go --path tests/   # auto-discover reports (python|typescript|java|go|rust)
  %[1]s skill create spec-tasks \
        --description 'Generate tasks.md with boundary/depends annotations.'   # .claude/skills/
  %[1]s agent create code-reviewer \
        --description 'Read-only adversarial reviewer' --tools Read --tools Grep   # .claude/agents/
  %[1]s mcp add filesystem \
        --command npx --arg -y --arg '@modelcontextprotocol/server-filesystem' --arg .   # .mcp.json
  %[1]s mcp add linear --url https://mcp.linear.app/mcp --type http
  %[1]s mcp validate
  %[1]s export kiro                                          # .kiro/steering + .kiro/specs
  %[1]s export codex --out ./build                           # AGENTS.md + .codex/config.toml
  %[1]s web                                                  # serve the live dashboard; prints the local URL
  %[1]s web --tunnel                                         # expose it publicly (pinggy by default; forces auth)
`, prog())
}

// parseFlags wraps fs.Parse to allow positional arguments to appear before
// flags (e.g., `csdd steering create api-conventions --inclusion always`).
// Go's stdlib flag.Parse stops at the first non-flag token, so we resume
// parsing after consuming each positional. The returned slice contains all
// positional arguments in original order.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		remaining := fs.Args()
		if len(remaining) == 0 {
			break
		}
		positionals = append(positionals, remaining[0])
		rest = remaining[1:]
	}
	return positionals, nil
}

// stringSliceFlag implements flag.Value to support repeatable flags like
// --pattern foo --pattern bar and --tools Read --tools Grep.
type stringSliceFlag struct {
	values []string
}

func (s *stringSliceFlag) String() string { return strings.Join(s.values, ",") }
func (s *stringSliceFlag) Set(v string) error {
	s.values = append(s.values, v)
	return nil
}

// addRoot binds --root to the given destination.
func addRoot(fs *flag.FlagSet, dst *string) {
	fs.StringVar(dst, "root", "", "Project root (default: nearest enclosing .claude/).")
}

// addForce binds --force to the given destination.
func addForce(fs *flag.FlagSet, dst *bool) {
	fs.BoolVar(dst, "force", false, "Override safety checks.")
}

// parseAction extracts the action subcommand and returns the remaining args.
func parseAction(resource string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("missing action for `%s %s`", prog(), resource)
	}
	return args[0], args[1:], nil
}

// failOnFlagParse short-circuits when a flag parse fails.
func failOnFlagParse(err error) int {
	if err == nil {
		return 0
	}
	if err == flag.ErrHelp {
		return 0
	}
	render.Err(err.Error())
	return 1
}

// containsString reports whether needle is in haystack.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
