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
	"regexp"
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
	case "copy":
		return runCopy(rest, templates)
	case "destroy", "uninstall":
		return runDestroy(rest, templates)
	case "steering":
		return runSteering(rest, templates)
	case "spec":
		return runSpec(rest, templates)
	case "plan":
		return runPlan(rest, templates)
	case "sandbox":
		return runSandbox(rest, templates)
	case "skill":
		return runSkill(rest, templates)
	case "agent":
		return runAgent(rest, templates)
	case "mcp":
		return runMCP(rest)
	case "graph":
		return runGraph(rest)
	case "wiki":
		return runWiki(rest, templates)
	case "codewiki":
		return runCodewiki(rest)
	case "export":
		return runExport(rest)
	case "web", "--web":
		return runWeb(rest)
	case "telegram":
		return runTelegram(rest)
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
	_, _ = fmt.Fprintln(w, strings.TrimSpace(helpText()))
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
  copy <kind>/<name>          Copy one shipped artifact into the workspace (rules, skills, agents, commands, hooks, templates, steering); pairs with 'init --exclude'. Bare 'copy' or 'copy <kind>' lists what's available.
  destroy                     Tear the workspace back down (.claude/, CLAUDE.md, .mcp.json, pre-push); keeps specs/. Asks to confirm; --force to skip.
  steering {init,create,list,show,delete,validate}
  spec     {init,list,show,status,generate,approve,validate,test-report,delete}
  plan     {init,list,validate,approve,status,next,brief,generate,run,unblock}   Plans decompose an initiative into feats (docs/plans/<slug>/); each feat becomes one spec.
  sandbox  {init,doctor}                        Scaffold a hardened default-deny-egress devcontainer; doctor proves isolation before bypass mode.
  skill    {create,list,show,add-reference,add-script,add-asset,validate,delete}
  agent    {create,list,show,delete}
  mcp      {add,install,presets,list,show,remove,enable,disable,validate}
  graph    {build,query,path,explain,analyze,export}   The knowledge-base index (a structured brain over the workspace).
  wiki     {init,lint}                 The LLM-authored knowledge base under docs/ (scaffold + health lint).
  codewiki {lint}                      The repo-derived wiki document under docs/raw/ (citation + structure lint).
  export   {kiro,codex}                Convert the workspace to Kiro / Codex format.
  web                         Launch a read-only web dashboard (live spec progress + file viewer).
  telegram {init,run}         Relay plan-run progress + spec-status changes to a Telegram chat (read-only, outbound-only).

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
  %[1]s copy                                                 # list every shipped artifact you can cherry-pick
  %[1]s copy skills/tdd-cycle                                # drop one skill into .claude/skills/
  %[1]s copy steering/product.md                             # add a single baseline steering file
  %[1]s steering create api-conventions \
        --inclusion fileMatch --pattern 'src/api/**/*' --pattern '**/*Controller.*'
  %[1]s steering create observability \
        --inclusion auto --description 'Logging/metrics. Use when adding instrumentation.'
  %[1]s plan list                                            # every plan under docs/plans/ with approval + progress
  %[1]s plan list --json                                     # same rows, machine-readable
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
  %[1]s codewiki lint                                        # lint every repo-derived doc in docs/raw/
  %[1]s codewiki lint docs/raw/acme-widget.md --repo docs/raw/widget   # citations resolve against the checkout
  %[1]s export kiro                                          # .kiro/steering + .kiro/specs
  %[1]s export codex --out ./build                           # AGENTS.md + .codex/config.toml
  %[1]s web                                                  # serve the live dashboard; prints the local URL
  %[1]s web --tunnel                                         # expose it publicly (pinggy by default; forces auth)
  %[1]s telegram init                                        # save bot token + chat_id to .csdd/bot.json (gitignored)
  %[1]s telegram run                                         # relay plan-run + spec-status updates to Telegram
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
			return nil, suggestFlag(fs, err)
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

// reUndefinedFlag matches the flag package's own wording for a name it does not
// know. The stdlib formats that message into a plain error with no type and no
// field carrying the name, so reading it back out of the string is the only way to
// reach what the user actually typed.
var reUndefinedFlag = regexp.MustCompile(`^flag provided but not defined: -+(.+)$`)

// suggestFlag names the closest defined flag when an unknown one is a near miss.
//
// `--squard-limit` for `--squad-limit` is a real report, and the stock message —
// "flag provided but not defined" — reads as "that option does not exist", which is
// how a transposed pair of letters gets mistaken for a missing feature. Naming the
// neighbour turns the same error into the fix.
//
// Only a single candidate within two edits is offered: an ambiguous "did you mean"
// list is noise, and a distant match would be a guess rather than a correction.
func suggestFlag(fs *flag.FlagSet, err error) error {
	m := reUndefinedFlag.FindStringSubmatch(strings.TrimSpace(err.Error()))
	if m == nil {
		return err
	}
	typed := m[1]
	const maxEdits = 2
	best, bestDist, ties := "", maxEdits+1, 0
	fs.VisitAll(func(f *flag.Flag) {
		switch d := editDistance(typed, f.Name); {
		case d < bestDist:
			best, bestDist, ties = f.Name, d, 1
		case d == bestDist:
			ties++
		}
	})
	if best == "" || bestDist > maxEdits || ties > 1 {
		return err
	}
	return fmt.Errorf("%v — did you mean --%s?", err, best)
}

// editDistance is the Levenshtein distance between a and b, over bytes: flag names
// are ASCII, so a rune-aware version would cost more and measure the same thing.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
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

// rejectPositionals errors when a command that takes no positional arguments
// received some, so e.g. `csdd init mydir` (a user expecting `git init DIR`
// semantics) fails loudly instead of silently operating on the cwd.
func rejectPositionals(cmd string, fs *flag.FlagSet) error {
	if extra := fs.Args(); len(extra) > 0 {
		return fmt.Errorf("`%s %s` takes no positional arguments; got %q (did you mean a flag like --root?)", prog(), cmd, extra[0])
	}
	return nil
}

// rejectExtraPositionals errors when more than max positional arguments were
// collected by parseFlags. Unlike rejectPositionals (which reads fs.Args()), it
// inspects the slice parseFlags returns, so it works with the interleaved
// positional/flag parsing the graph commands use. A stray token here is almost
// always a quoting mistake (`graph query auth login` → two positionals) or a typo
// that would otherwise silently swallow the flags that follow it.
func rejectExtraPositionals(cmd string, pos []string, max int) error {
	if len(pos) > max {
		return fmt.Errorf("`%s %s` got an unexpected extra argument %q — quote multi-word values (e.g. `%s graph query \"a b\"`) and put flags before positionals", prog(), cmd, pos[max], prog())
	}
	return nil
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

// isHelpFlag reports whether an action token is really a request for help, so
// `csdd spec --help` prints usage instead of erroring with "unknown action".
func isHelpFlag(s string) bool {
	return s == "-h" || s == "--help" || s == "help"
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
