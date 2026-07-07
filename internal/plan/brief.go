package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/paths"
)

// forbiddenActions is the verbatim R9.6 contract every brief carries: the session
// authors artifacts and returns a verdict, nothing more. The runner owns
// approvals, commits, and all plan/graph/state writes.
var forbiddenActions = []string{
	"Do NOT edit plan.md, plan.json, docs/graph/, or .csdd/ — these are the runner's; touching them fails the step.",
	"Do NOT run `csdd spec approve`, `csdd plan approve`, or `git commit` — the runner performs approvals and commits only after the gates pass.",
	"Do NOT adopt a technology that is not in docs/stack.md — that is an open decision; stop and report it in your verdict instead.",
	"Do NOT make a decision that is hard to reverse, surprising without context, and the result of a real trade-off unless a cited ADR or stack row already covers it — that is an OPEN DECISION; return a `blocked` verdict with a revision proposal, never improvise.",
	"If you hit an unforeseen problem you cannot solve within this step's contract, return a `blocked` verdict with a revision proposal — never improvise around the plan.",
}

// Brief assembles the deterministic context pack for one step (R7). It draws only
// on explicit content — the feat row, its seeds, resolved stack rows, resolved
// wiki refs (path + description, never the body), the Executor Notes, and the
// step contract — so the same plan and step always produce a byte-identical
// brief (R7.3). It inlines stack rows in full but never wiki bodies (R7.2).
func Brief(root string, doc *PlanDoc, step Step) (string, error) {
	feat, ok := doc.Feat(step.Feat)
	if !ok {
		return "", fmt.Errorf("feat %q is not in plan %q", step.Feat, doc.Slug)
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("# Brief — %s / %s\n\n", doc.Slug, feat.Slug)
	w("You are a fresh session executing ONE step of an approved csdd plan. Do exactly\n")
	w("this step's contract, then return the verdict schema. Nothing more.\n\n")

	// 1. Step contract.
	produce, gates := stepContract(doc, step)
	w("## Step: %s\n\n", step.Step)
	if step.Reason != "" {
		w("%s\n\n", step.Reason)
	}
	w("**Produce:** %s\n\n", produce)
	w("**Gates that will run (all must pass before this step advances):**\n")
	for _, g := range gates {
		w("- %s\n", g)
	}
	w("\n**Forbidden actions:**\n")
	for _, f := range forbiddenActions {
		w("- %s\n", f)
	}
	w("\n")

	// 2. Feat context.
	w("## Feat: %s\n\n", feat.Slug)
	w("- Objective: %s\n", orDash(feat.Objective))
	w("- Milestone: %s\n", orDash(feat.Milestone))
	if len(feat.Depends) > 0 {
		w("- Depends on (already done): %s\n", strings.Join(feat.Depends, ", "))
	}
	w("\n")

	// 3. Stack rows — inlined in full (they are one-line contracts).
	if len(feat.StackRefs) > 0 {
		rows := decidedRows(root)
		w("## Tech contract (docs/stack.md — use ONLY these)\n\n")
		for _, name := range feat.StackRefs {
			if r, ok := rows[normalizeTechName(name)]; ok {
				w("- **%s** (%s) — version %s. %s", orDash(r.Choice), orDash(r.Domain), orDash(r.Version), orDash(r.Why))
				if r.Refs != "" {
					w(" Refs: %s", r.Refs)
				}
				w("\n")
			} else {
				w("- **%s** — WARNING: not found in the Decided table (validate should have caught this).\n", name)
			}
		}
		w("\n")
	}

	// 3b. Decisions — cited ADRs inlined in full (they are short by format, the
	// stack-row treatment; the why travels with the what, principle 4).
	if len(feat.ADRRefs) > 0 {
		adrs := ScanADRs(root)
		w("## Decisions (docs/adr — the why)\n\n")
		for _, slug := range feat.ADRRefs {
			adr, res := adrs.Resolve(slug)
			if res != ADRResolved {
				w("- **adr:%s** — WARNING: does not resolve to a docs/adr record (validate should have caught this).\n", slug)
				continue
			}
			w("### %s (adr:%s)\n\n", orDash(adr.Title), adr.Slug)
			if adr.Status == ADRStatusSuperseded {
				w("_status: superseded")
				if adr.SupersededBy != 0 {
					w(" by %s", fourDigit(adr.SupersededBy))
				}
				w("_\n\n")
			}
			if adr.Body != "" {
				w("%s\n\n", adr.Body)
			}
		}
	}

	// 4. Wiki refs — path + description only (the session reads what it needs).
	if len(feat.WikiRefs) > 0 {
		w("## Reference pages (read what you need; do not assume the content)\n\n")
		for _, ref := range feat.WikiRefs {
			path, desc := wikiRefInfo(root, ref)
			if path == "" {
				w("- [[%s]] — WARNING: no matching page under docs/wiki/pages/.\n", ref)
				continue
			}
			if desc != "" {
				w("- %s — %s\n", path, desc)
			} else {
				w("- %s\n", path)
			}
		}
		w("\n")
	}

	// 5. Seeds — pre-authored artifacts to fold into this feat's spec.
	if seeds := featSeedFiles(root, doc.Slug, feat.Slug); len(seeds) > 0 {
		w("## Seeds (pre-authored inputs for this feat's spec)\n\n")
		for _, s := range seeds {
			w("- %s\n", s)
		}
		w("\n")
	}

	// 6. Executor Notes — verbatim.
	if doc.ExecutorNotes != "" {
		w("## Executor Notes\n\n%s\n\n", doc.ExecutorNotes)
	}

	// 7. Graph-first consultation.
	w("## Consult the knowledge graph BEFORE reading code\n\n")
	w("- `csdd graph query <term>` — find the nodes for a concept.\n")
	w("- `csdd graph explain <id>` — see a node's neighborhood and provenance.\n")
	w("- `csdd graph path <a> <b>` — trace how two artifacts connect.\n")
	w("Traverse the graph to locate the relevant code; do not grep the whole tree.\n")

	return b.String(), nil
}

// stepContract returns the artifact to produce and the gates that will run for a
// step (R7.1). Spec-phase steps are gated by the spec validator; an implementation
// task is gated by the plan's own Quality Gates plus graph traceability.
func stepContract(doc *PlanDoc, step Step) (produce string, gates []string) {
	feat := step.Feat
	switch {
	case step.Step == StepSpecRequirements:
		return fmt.Sprintf("specs/%s/requirements.md (EARS acceptance criteria) via `csdd spec generate %s --artifact requirements`", feat, feat),
			[]string{fmt.Sprintf("`csdd spec validate %s`", feat)}
	case step.Step == StepSpecDesign:
		return fmt.Sprintf("specs/%s/design.md (components, traceability, file plan) via `csdd spec generate %s --artifact design`", feat, feat),
			[]string{fmt.Sprintf("`csdd spec validate %s`", feat)}
	case step.Step == StepSpecTasks:
		return fmt.Sprintf("specs/%s/tasks.md (boundary/depends-annotated tasks) via `csdd spec generate %s --artifact tasks`", feat, feat),
			[]string{fmt.Sprintf("`csdd spec validate %s`", feat)}
	case strings.HasPrefix(step.Step, StepTaskPrefix):
		id := strings.TrimSpace(strings.TrimPrefix(step.Step, StepTaskPrefix))
		g := planGateCommands(doc)
		g = append(g, "`csdd graph analyze --strict` (traceability holds)")
		return fmt.Sprintf("the implementation and tests for task %s in specs/%s/tasks.md; check the box when done", id, feat), g
	default:
		return step.Step, planGateCommands(doc)
	}
}

// planGateCommands renders the plan's Quality Gates as backticked commands, or a
// placeholder note when the plan declared none.
func planGateCommands(doc *PlanDoc) []string {
	if len(doc.Gates) == 0 {
		return []string{"(no Quality Gates declared in the plan)"}
	}
	out := make([]string, 0, len(doc.Gates))
	for _, g := range doc.Gates {
		out = append(out, fmt.Sprintf("`%s` (%s)", g.Command, g.Label))
	}
	return out
}

// wikiRefInfo resolves a [[wiki-page]] ref to its workspace path and frontmatter
// description (or title), reading only the frontmatter — never inlining the body
// (R7.2). Returns empty path when no page matches.
func wikiRefInfo(root, ref string) (path, desc string) {
	dir := filepath.Join(paths.DocsWiki(root), "pages")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	target := normalizeWikiSlug(ref)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if normalizeWikiSlug(strings.TrimSuffix(e.Name(), ".md")) != target {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("docs", "wiki", "pages", e.Name()))
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return rel, ""
		}
		fm := frontmatter.Parse(string(data))
		d := fm.AsString("description", "")
		if d == "" {
			d = fm.AsString("title", "")
		}
		return rel, d
	}
	return "", ""
}

// featSeedFiles returns the workspace paths of every file under
// docs/plans/<slug>/seeds/<feat>/, sorted, for the brief's seed list.
func featSeedFiles(root, slug, feat string) []string {
	dir := filepath.Join(seedsDir(Dir(root, slug)), feat)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join("docs", "plans", slug, "seeds", feat, e.Name())))
	}
	sort.Strings(out)
	return out
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
