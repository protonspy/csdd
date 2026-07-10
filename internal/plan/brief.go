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

// missionSteps is the whole-feat contract every brief carries: the session owns
// the entire spec lifecycle and the implementation, drives its own git, and returns
// a verdict. There is no runner-side gate — the loop trusts the session — so the
// checks the runner used to enforce become checks the session runs itself before
// declaring the feat done (see selfChecks).
var missionSteps = []string{
	"Author the spec if it does not exist yet: `csdd spec init <feat>`, then generate, validate, and approve each phase — `csdd spec generate <feat> --artifact requirements|design|tasks`, `csdd spec validate <feat>`, `csdd spec approve <feat> --phase requirements|design|tasks`.",
	"Implement every task in specs/<feat>/tasks.md, checking each box as you finish it.",
	"You own git and the csdd dev-cycle: branch, commit via /csdd-commit (the pre-push gate runs there), and open the PR.",
	"Record any technology or hard-to-reverse trade-off the contract does not already cover — a docs/stack.md Decided row, plus a docs/adr record when the why needs more than a line. Prefer the option that deviates least from the decided stack.",
}

// forbiddenActions is the short list the session must not touch: the approved plan
// is the contract, and .csdd/ is the loop's operational state.
var forbiddenActions = []string{
	"Do NOT edit plan.md or plan.json — the approved plan is the contract. Leave .csdd/ (the loop's operational state, including progress.json) alone.",
}

// FeatBrief assembles the deterministic mission pack for one whole feat (R7). It
// draws only on explicit content — the feat row, its seeds, resolved stack rows,
// resolved wiki refs (path + description, never the body), the Executor Notes, and
// the feat's own quality gates — so the same plan and feat always produce a
// byte-identical brief (R7.3). It inlines stack rows in full but never wiki bodies
// (R7.2). The autonomous run context (handoff, failure trail) is appended after
// this by the runner, so this prefix stays stable across a feat's sessions.
func FeatBrief(root string, doc *PlanDoc, feat Feat) (string, error) {
	if _, ok := doc.Feat(feat.Slug); !ok {
		return "", fmt.Errorf("feat %q is not in plan %q", feat.Slug, doc.Slug)
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("# Mission — %s / %s\n\n", doc.Slug, feat.Slug)
	w("You are a fresh session in a self-correcting autonomous loop. Your mission is to\n")
	w("fully deliver ONE feat of an approved csdd plan: %s. Drive the entire spec\n", feat.Slug)
	w("lifecycle and its implementation yourself, then return the verdict schema.\n\n")

	// 1. The mission contract.
	w("## Your mission (own the whole flow)\n\n")
	for i, s := range missionSteps {
		w("%d. %s\n", i+1, s)
	}
	w("\n**Forbidden actions:**\n")
	for _, f := range forbiddenActions {
		w("- %s\n", f)
	}
	w("\n**Verdict protocol:**\n")
	w("- `done` — the WHOLE feat is delivered and every self-check below passes. The loop trusts this and moves to the next feat, so never claim done on hope.\n")
	w("- `continue` — honest partial work: you are out of room before the feat is complete. Put the handoff for your successor in `summary` (what is done, what remains, what to try next).\n")
	w("\n")

	// 2. Feat context.
	w("## Feat: %s\n\n", feat.Slug)
	w("- Objective: %s\n", orDash(feat.Objective))
	w("- Milestone: %s\n", orDash(feat.Milestone))
	if len(feat.Depends) > 0 {
		w("- Depends on (delivered earlier in the plan): %s\n", strings.Join(feat.Depends, ", "))
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
	w("Traverse the graph to locate the relevant code; do not grep the whole tree.\n\n")

	// 8. Self-checks — what the runner used to gate on, now the session runs itself.
	w("## Before you declare `done` — run these checks YOURSELF\n\n")
	w("The loop does NOT verify your work; it trusts your verdict. So the feat is done\n")
	w("only when ALL of these pass and every task box in specs/%s/tasks.md is checked:\n\n", feat.Slug)
	w("- `csdd spec validate %s` (the spec is structurally sound: EARS, traceability, tasks)\n", feat.Slug)
	w("- `csdd graph analyze --strict` (spec↔task↔component traceability holds)\n")
	for _, g := range planGateCommands(doc) {
		w("- %s\n", g)
	}
	w("\nOnly then return `{\"status\":\"done\"}`. If you ran out of room first, return\n")
	w("`{\"status\":\"continue\"}` with the handoff in `summary`.\n")

	return b.String(), nil
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
