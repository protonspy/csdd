package cli

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/protonspy/csdd/internal/graph"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/workspace"
)

// runGraph dispatches `csdd graph <action>`. The graph is a structured brain for
// fast, token-cheap, precise consultation of the workspace: build persists the
// index, query/path/explain traverse it, analyze lints the SDD contract.
func runGraph(args []string) int {
	action, rest, err := parseAction("graph", args)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if isHelpFlag(action) {
		graphHelp()
		return 0
	}
	switch action {
	case "build":
		return graphBuild(rest)
	case "query":
		return graphQuery(rest)
	case "path":
		return graphPath(rest)
	case "explain":
		return graphExplain(rest)
	case "analyze":
		return graphAnalyze(rest)
	case "export":
		return graphExport(rest)
	default:
		render.Err("unknown action for `graph`: " + action)
		graphHelp()
		return 1
	}
}

func graphHelp() {
	fmt.Println(`csdd graph — the knowledge-base index (a structured brain over the workspace).

  build            Rebuild docs/graph/graph.json from the corpus (+ appends log.md).
  query "<terms>"  Find nodes by label tier and show their neighborhood.
  path <A> <B>     Shortest path between two nodes.
  explain <label>  A node plus its connections, ordered by neighbor degree.
  analyze          Surface traceability gaps + wiki/tech lints (--strict exits non-zero).
  export           Write docs/graph/graph.html (self-contained visualization).

Common flags: --root DIR, --json.`)
}

// requireWorkspace enforces the .csdd/ marker (R15.2), with an explicit
// legacy-upgrade message when only .claude/ exists (R15.3).
func requireWorkspace(root string) error {
	if dirExists(paths.State(root)) {
		return nil
	}
	if dirExists(paths.Claude(root)) {
		return fmt.Errorf("legacy csdd workspace: found .claude/ but no .csdd/ at %s.\n  Re-run `%s init` to upgrade it in place — nothing is overwritten, only the missing .csdd/ (and other new pieces) are added.", root, prog())
	}
	return fmt.Errorf("not a csdd workspace (no .csdd/ at %s). Run `%s init` first.", root, prog())
}

// resolveGraphRoot resolves --root and enforces the workspace marker.
func resolveGraphRoot(root string) (string, error) {
	r, err := workspace.Resolve(root)
	if err != nil {
		return "", err
	}
	if err := requireWorkspace(r); err != nil {
		return "", err
	}
	return r, nil
}

func graphBuild(args []string) int {
	fs := flag.NewFlagSet("graph build", flag.ContinueOnError)
	var root string
	var jsonOut, full bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	fs.BoolVar(&full, "full", false, "Force a complete rebuild instead of the incremental default.")
	if err := fs.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := resolveGraphRoot(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	// Incremental is the default; --full forces a complete rebuild (R13.3). Both
	// produce byte-identical output.
	build := graph.BuildIncremental
	if full {
		build = graph.Build
	}
	g, err := build(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if _, err := graph.WriteBuild(r, g, time.Now()); err != nil {
		render.Err(err.Error())
		return 1
	}
	if jsonOut {
		return emitJSON(map[string]any{
			"ok": true, "nodes": len(g.Nodes), "edges": len(g.Links),
			"pending": len(g.Pending), "output": graph.GraphJSONRel,
		})
	}
	render.OK(fmt.Sprintf("Built graph: %d nodes, %d edges (%d pending references).", len(g.Nodes), len(g.Links), len(g.Pending)))
	render.Info("Wrote " + graph.GraphJSONRel)
	return 0
}

func graphQuery(args []string) int {
	fs := flag.NewFlagSet("graph query", flag.ContinueOnError)
	var root string
	var jsonOut bool
	var hops, budget int
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	fs.IntVar(&hops, "hops", 1, "Neighborhood radius.")
	fs.IntVar(&budget, "budget", 2000, "Token budget for rendering (M4 engine).")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(pos) == 0 {
		render.Err("usage: " + prog() + " graph query \"<terms>\"")
		return 1
	}
	terms := pos[0]
	g, r, code := loadGraph(root)
	if code != 0 {
		return code
	}
	_ = r
	matches := graph.Query(g, terms, graph.QueryOptions{Hops: hops, Budget: budget})
	if jsonOut {
		return emitJSON(map[string]any{"query": terms, "matches": matches})
	}
	if len(matches) == 0 {
		render.Info("No nodes match " + terms)
		return 0
	}
	for _, m := range matches {
		fmt.Printf("%s  %s  [%s, %s]\n", render.Bold(m.Node.Label), render.Cyan(m.Node.ID), m.Node.FileType, m.Tier)
		for _, nb := range m.Neighbors {
			arrow := "→"
			if nb.Direction == "in" {
				arrow = "←"
			}
			fmt.Printf("    %s %s  %s (%s)\n", arrow, nb.Relation, nb.Node.Label, nb.Node.FileType)
		}
	}
	return 0
}

func graphPath(args []string) int {
	fs := flag.NewFlagSet("graph path", flag.ContinueOnError)
	var root string
	var jsonOut bool
	var maxHops int
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	fs.IntVar(&maxHops, "max-hops", 0, "Maximum path length (0 = unbounded).")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(pos) < 2 {
		render.Err("usage: " + prog() + " graph path <A> <B>")
		return 1
	}
	g, _, code := loadGraph(root)
	if code != 0 {
		return code
	}
	pr, err := graph.Path(g, pos[0], pos[1], maxHops)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if jsonOut {
		return emitJSON(pr)
	}
	fmt.Printf("%s\n", render.Bold(pr.From.Label))
	for _, s := range pr.Steps {
		arrow := "──" + s.Relation + "──▶"
		if s.Direction == "backward" {
			arrow = "◀──" + s.Relation + "──"
		}
		fmt.Printf("  %s %s\n", arrow, s.Node.Label)
	}
	return 0
}

func graphExplain(args []string) int {
	fs := flag.NewFlagSet("graph explain", flag.ContinueOnError)
	var root string
	var jsonOut bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(pos) == 0 {
		render.Err("usage: " + prog() + " graph explain <label>")
		return 1
	}
	g, _, code := loadGraph(root)
	if code != 0 {
		return code
	}
	er, err := graph.Explain(g, pos[0])
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if jsonOut {
		return emitJSON(er)
	}
	fmt.Printf("%s  %s  [%s]\n", render.Bold(er.Node.Label), render.Cyan(er.Node.ID), er.Node.FileType)
	for _, c := range er.Connections {
		arrow := "→"
		if c.Direction == "in" {
			arrow = "←"
		}
		fmt.Printf("  %s %s  %s (deg %d)\n", arrow, c.Relation, c.Node.Label, c.Degree)
	}
	return 0
}

func graphAnalyze(args []string) int {
	fs := flag.NewFlagSet("graph analyze", flag.ContinueOnError)
	var root string
	var jsonOut, strict bool
	addRoot(fs, &root)
	addJSON(fs, &jsonOut)
	fs.BoolVar(&strict, "strict", false, "Exit non-zero when any finding is reported (CI gate).")
	if err := fs.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	g, r, code := loadGraph(root)
	if code != 0 {
		return code
	}
	report := graph.Analyze(g, r)
	if jsonOut {
		exit := 0
		if strict && len(report.Findings) > 0 {
			exit = 2
		}
		_ = emitJSON(report)
		return exit
	}
	if len(report.Findings) == 0 {
		render.OK("No findings. Traceability contract is clean.")
		return 0
	}
	renderFindings(report.Findings)
	if len(report.GodNodes) > 0 {
		render.Info(fmt.Sprintf("Hubs: %s", godNodeSummary(report.GodNodes)))
	}
	render.Warn(fmt.Sprintf("%d finding(s).", len(report.Findings)))
	if strict {
		return 2
	}
	return 0
}

func graphExport(args []string) int {
	fs := flag.NewFlagSet("graph export", flag.ContinueOnError)
	var root, out string
	addRoot(fs, &root)
	fs.StringVar(&out, "out", "", "Output path (default: docs/graph/graph.html).")
	if err := fs.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := resolveGraphRoot(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	g, err := graph.Build(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	path, err := graph.ExportHTML(r, g, out)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("Wrote " + workspace.Relative(r, path))
	return 0
}

// loadGraph resolves the workspace and builds the graph in memory (a full rebuild
// is milliseconds at this scale). Returns the graph, the resolved root, and a
// nonzero exit code on failure.
func loadGraph(root string) (*graph.Graph, string, int) {
	r, err := resolveGraphRoot(root)
	if err != nil {
		render.Err(err.Error())
		return nil, "", 1
	}
	g, err := graph.Build(r)
	if err != nil {
		render.Err(err.Error())
		return nil, "", 1
	}
	return g, r, 0
}

func renderFindings(findings []graph.Finding) {
	for _, f := range findings {
		label := f.Label
		if label != "" {
			label = " — " + label
		}
		loc := ""
		if f.File != "" {
			loc = "  (" + f.File + ")"
		}
		render.Info(fmt.Sprintf("[%s] %s%s%s", f.Corpus, f.Message, label, loc))
	}
}

func godNodeSummary(hubs []graph.GodNode) string {
	s := ""
	for i, h := range hubs {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s(%d)", h.Label, h.Degree)
		if i >= 4 {
			break
		}
	}
	return s
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
