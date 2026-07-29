package cli

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/protonspy/csdd/internal/plan"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/workspace"
)

// runSandbox dispatches `csdd sandbox <action>`: init (scaffold the hardened
// devcontainer) and doctor (prove isolation before bypass mode).
func runSandbox(args []string, templates embed.FS) int {
	action, rest, err := parseAction("sandbox", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if isHelpFlag(action) {
		help(os.Stdout)
		return 0
	}
	switch action {
	case "init":
		return sandboxInit(rest, templates)
	case "doctor":
		return sandboxDoctor(rest)
	default:
		render.Err("unknown sandbox action: " + action)
		return 1
	}
}

func sandboxInit(args []string, templates embed.FS) int {
	fs := flag.NewFlagSet("sandbox init", flag.ContinueOnError)
	var root string
	var hardened, force bool
	var features, allowDomains stringSliceFlag
	addRoot(fs, &root)
	fs.Var(&features, "feature", "Add a toolchain devcontainer feature (repeatable): "+strings.Join(plan.SandboxFeatureNames(), "|"))
	fs.Var(&allowDomains, "allow-domain", "Add a host to the egress allow-list (repeatable).")
	fs.BoolVar(&hardened, "hardened", false, "Restrict the vscode user's sudo to the firewall script (recommended for unattended, bypass-mode agent work).")
	addForce(fs, &force)
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	written, err := plan.SandboxInit(templates, plan.SandboxOptions{
		Root:         r,
		Features:     features.values,
		AllowDomains: allowDomains.values,
		Hardened:     hardened,
		Force:        force,
	})
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if len(written) == 0 {
		render.Info(".devcontainer/ already scaffolded; nothing changed (--force to regenerate)")
		return 0
	}
	for _, name := range written {
		render.OK("wrote .devcontainer/" + name)
	}
	render.Info("next: open the folder in your IDE (\"Reopen in Container\"), then inside run `" + prog() + " sandbox doctor`")
	return 0
}

// sandboxProbes lets tests drive `sandbox doctor` without a real container or
// network. Its zero value means real probes (Doctor fills the defaults).
var sandboxProbes plan.DoctorProbes

func sandboxDoctor(args []string) int {
	fs := flag.NewFlagSet("sandbox doctor", flag.ContinueOnError)
	var jsonOut bool
	addJSON(fs, &jsonOut)
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	report := plan.Doctor(sandboxProbes)
	if jsonOut {
		emitJSON(report)
		if report.OK {
			return 0
		}
		return 1
	}
	for _, c := range report.Checks {
		mark := render.Red("✗")
		if c.OK {
			mark = render.Green("✓")
		}
		fmt.Printf("  %s %-18s %s\n", mark, c.Name, c.Detail)
	}
	if report.OK {
		render.OK("sandbox verified: isolation is enforced")
		return 0
	}
	render.Err("sandbox NOT verified — do not pass --dangerously-skip-permissions here")
	return 1
}
