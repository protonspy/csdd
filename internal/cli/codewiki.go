package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/codewiki"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/workspace"
)

// runCodewiki dispatches `csdd codewiki <action>`. A codewiki document is the
// codewiki-format distillation of a source checkout dropped under docs/raw/,
// authored by the `codewiki` skill. The CLI never writes it — it only lints,
// exactly as it does for the wiki and the glossary.
func runCodewiki(args []string) int {
	action, rest, err := parseAction("codewiki", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if isHelpFlag(action) {
		codewikiHelp()
		return 0
	}
	switch action {
	case "lint":
		return codewikiLint(rest)
	default:
		render.Err("unknown action for `codewiki`: " + action)
		codewikiHelp()
		return 1
	}
}

func codewikiHelp() {
	fmt.Println(`csdd codewiki — the repo-derived wiki document under docs/raw/.

  lint [FILE...]   Check the Structure/section sync, slugs, and every
                   [path:start-end]() citation against the checkout it names.
                   With no FILE, lints every docs/raw/*.md carrying a header.

Flags: --root DIR, --repo DIR (the checkout citations resolve against), --json.

The document itself is authored by the ` + "`codewiki`" + ` skill (/csdd-codewiki);
once lint is clean, ingest it with /csdd-wiki-ingest.`)
}

func codewikiLint(args []string) int {
	fs := flag.NewFlagSet("codewiki lint", flag.ContinueOnError)
	var root, repo string
	var jsonOut bool
	addRoot(fs, &root)
	fs.StringVar(&repo, "repo", "", "checkout the citations resolve against (default: the header's src:, else a sibling directory)")
	addJSON(fs, &jsonOut)
	targets, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	targets, code := codewikiTargets(r, targets)
	if code != 0 {
		return code
	}

	type docReport struct {
		Path     string             `json:"path"`
		Repo     string             `json:"repo,omitempty"`
		Findings []codewiki.Finding `json:"findings"`
	}
	reports := make([]docReport, 0, len(targets))
	faults := 0
	for _, t := range targets {
		raw, err := os.ReadFile(t) //nolint:gosec // operator-supplied path, by design
		if err != nil {
			render.Err(err.Error())
			return 1
		}
		checkout := codewiki.ResolveRepo(r, t, repo, codewiki.Parse(string(raw)).Header)
		findings, err := codewiki.Lint(t, checkout)
		if err != nil {
			render.Err(err.Error())
			return 1
		}
		faults += codewiki.Faults(findings)
		rep := docReport{Path: workspace.Relative(r, t), Findings: findings}
		if checkout != "" {
			rep.Repo = workspace.Relative(r, checkout)
		}
		reports = append(reports, rep)
	}

	if jsonOut {
		_ = emitJSON(map[string]any{"documents": reports, "faults": faults})
		if faults > 0 {
			return 2
		}
		return 0
	}
	for _, rep := range reports {
		if len(reports) > 1 || len(rep.Findings) > 0 {
			label := rep.Path
			if rep.Repo != "" {
				label += "  (checkout: " + rep.Repo + ")"
			}
			render.Info(render.Bold(label))
		}
		for _, f := range rep.Findings {
			renderCodewikiFinding(rep.Path, f)
		}
	}
	if faults == 0 {
		render.OK(fmt.Sprintf("Codewiki is sound: no findings across %d document(s).", len(reports)))
		return 0
	}
	render.Warn(fmt.Sprintf("%d codewiki finding(s).", faults))
	return 2 // CI-gateable, like `wiki lint`
}

// codewikiTargets resolves the documents to lint: the operator's arguments, or —
// when there are none — every provenance-carrying .md in the dropzone.
func codewikiTargets(root string, args []string) ([]string, int) {
	if len(args) > 0 {
		out := make([]string, 0, len(args))
		for _, a := range args {
			p := a
			if !filepath.IsAbs(p) {
				if _, err := os.Stat(p); err != nil {
					p = filepath.Join(root, filepath.FromSlash(a))
				}
			}
			if info, err := os.Stat(p); err != nil || info.IsDir() {
				render.Err("not a document: " + a)
				return nil, 1
			}
			out = append(out, p)
		}
		return out, 0
	}
	found, err := codewiki.Discover(paths.DocsRaw(root))
	if err != nil {
		render.Err("no docs/raw/ to lint — run `csdd wiki init` first")
		return nil, 1
	}
	if len(found) == 0 {
		render.Info("No codewiki documents under docs/raw/ — compile one with /csdd-codewiki <checkout>.")
		return nil, 0
	}
	return found, 0
}

func renderCodewikiFinding(doc string, f codewiki.Finding) {
	loc := doc
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", doc, f.Line)
	}
	msg := fmt.Sprintf("[%s] %s  (%s)", f.Kind, f.Message, loc)
	if f.Informational {
		msg += " " + strings.TrimSpace("— informational")
	}
	render.Info(msg)
}
