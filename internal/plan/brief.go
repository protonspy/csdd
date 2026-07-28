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

// FeatBrief assembles the context pack for one whole feat (R7): what this feat is,
// what governs it, what it is likely to touch, and what verifies it.
//
// It is CONTEXT and nothing else. It carries no role preamble, no development
// process, and no verdict protocol — those were written for the autonomous runner,
// which addressed a headless session that had this text and nothing else. A brief is
// read now by a session that already has its CLAUDE.md, its skills and a human in the
// room; telling it who it is and how to work would be restating, worse, what it is
// already governed by. What only the plan knows is the feat: that is what this holds.
//
// It draws on explicit content — the feat row, its seeds, the governing ADR/stack
// refs (slugs/names/paths only — never ADR bodies, stack row details, or wiki
// descriptions), the Executor Notes, the plan's quality gates — plus the feat's
// stored context pack when one exists. Compliance with the governors is enforced
// mechanically (designConformance requires design.md to cite each governing ADR,
// `plan validate` rejects tech outside the Decided stack, the code-reviewer gate
// rejects it in code), not by inlining their bodies (R7.2).
//
// It stays deterministic (R7.3): the pack is read from disk like the seeds are, so
// the same workspace state always renders the same brief for the same workspace
// state — which is what makes it reviewable with `csdd plan brief` before anyone
// works from it.
func FeatBrief(root string, doc *PlanDoc, feat Feat) (string, error) {
	if _, ok := doc.Feat(feat.Slug); !ok {
		return "", fmt.Errorf("feat %q is not in plan %q", feat.Slug, doc.Slug)
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	// 1. The feat itself.
	w("# Feat: %s — plan %s\n\n", feat.Slug, doc.Slug)
	w("- Objective: %s\n", orDash(feat.Objective))
	w("- Milestone: %s\n", orDash(feat.Milestone))
	if len(feat.Depends) > 0 {
		w("- Depends on (delivered earlier in this plan): %s\n", strings.Join(feat.Depends, ", "))
	}
	w("\n")

	// 2. Governing stack — refs only. The brief used to inline each Decided row
	// (choice/version/why) so a design could not drift from the stack; with the
	// bodies gone, compliance is enforced mechanically — `csdd spec validate` +
	// `plan validate` reject tech outside the Decided table, and the
	// `code-reviewer` gate rejects it in code. The session fetches a row's detail
	// via `csdd graph query <term>` when it needs it.
	if len(feat.StackRefs) > 0 {
		rows := decidedRows(root)
		w("## Governing stack (docs/stack.md Decided — use ONLY these)\n\n")
		w("Fetch a row's version/why via `csdd graph query <term>`; do NOT introduce tech outside this list — the `code-reviewer` gate rejects it.\n\n")
		for _, name := range feat.StackRefs {
			if _, ok := rows[normalizeTechName(name)]; ok {
				w("- stack:%s\n", name)
			} else {
				w("- stack:%s — WARNING: not found in the Decided table (validate should have caught this).\n", name)
			}
		}
		w("\n")
	}

	// 2b. Governing decisions — refs only. The brief used to inline each cited
	// ADR's title + body so a design could not silently ignore the decisions it
	// was bound to; with the bodies gone, `designConformance` requires the
	// authored design.md to cite each governing adr:<slug>, and the session
	// fetches the decision itself via `csdd graph explain adr:<slug>`.
	if len(feat.ADRRefs) > 0 {
		adrs := ScanADRs(root)
		w("## Governing decisions (docs/adr — fetch the why; cite each in design)\n\n")
		w("The brief does not inline ADR bodies. For each governor run `csdd graph explain adr:<slug>` for the decision; `csdd spec validate` requires your design.md to cite it.\n\n")
		for _, slug := range feat.ADRRefs {
			if _, res := adrs.Resolve(slug); res != ADRResolved {
				w("- adr:%s — WARNING: does not resolve to a docs/adr record (validate should have caught this).\n", slug)
				continue
			}
			w("- adr:%s\n", slug)
		}
		w("\n")
	}

	// 3. Wiki refs — path only (the session reads what it needs, when it needs it).
	if len(feat.WikiRefs) > 0 {
		w("## Reference pages (read what you need; do not assume the content)\n\n")
		for _, ref := range feat.WikiRefs {
			path, _ := wikiRefInfo(root, ref)
			if path == "" {
				w("- [[%s]] — WARNING: no matching page under docs/wiki/pages/.\n", ref)
				continue
			}
			w("- %s\n", path)
		}
		w("\n")
	}

	// 4. Seeds — pre-authored artifacts to fold into this feat's spec.
	if seeds := featSeedFiles(root, doc.Slug, feat.Slug); len(seeds) > 0 {
		w("## Seeds (pre-authored inputs for this feat's spec)\n\n")
		for _, s := range seeds {
			w("- %s\n", s)
		}
		w("\n")
	}

	// 5. The discovered half: where this feat lives in the tree, what constrains it,
	// what is already there. Read from disk, so the brief stays deterministic.
	pack, err := LoadPack(root, doc.Slug, feat.Slug)
	if err != nil {
		// A corrupt pack is worth naming, but never worth failing a brief over: the
		// rest of it is exact.
		w("_(the stored context pack for this feat could not be read: %s)_\n\n", firstLine(err.Error()))
	}
	renderPack(&b, pack)

	// 6. Executor Notes — verbatim.
	if doc.ExecutorNotes != "" {
		w("## Executor Notes\n\n%s\n\n", doc.ExecutorNotes)
	}

	// 7. The plan's own verification contract.
	w("## Quality gates for this plan\n\n")
	w("These must pass for this feat:\n\n")
	for _, g := range planGateCommands(doc) {
		w("- %s\n", g)
	}

	return b.String(), nil
}

// renderPack writes the discovered half of the brief. Nothing here is authored by
// csdd or by the plan: every line came from the enrichment pass and survived
// VerifyPack, so the section exists only when there is verified content to put in it.
func renderPack(b *strings.Builder, p *EnrichPack) {
	if p.Empty() {
		return
	}
	w := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }

	if len(p.Touches) > 0 {
		w("## Where this feat lives\n\n")
		for _, t := range p.Touches {
			w("- `%s` — %s\n", t.Path, t.Why)
		}
		w("\n")
	}
	if len(p.Governors) > 0 {
		w("## Governing constraints\n\n")
		for _, g := range p.Governors {
			if g.Declared {
				w("- %s — %s\n", g.ID, g.Constraint)
				continue
			}
			w("- %s (discovered) — %s\n", g.ID, g.Constraint)
		}
		w("\nA **(discovered)** governor is not in the plan's Refs column: use it as context,\n")
		w("but `spec validate` only requires your design to cite the declared ones.\n\n")
	}
	if len(p.Exists) > 0 {
		w("## Already there (do not redo)\n\n")
		for _, s := range p.Exists {
			w("- %s\n", s)
		}
		w("\n")
	}
	if len(p.Missing) > 0 {
		w("## Still missing\n\n")
		for _, s := range p.Missing {
			w("- %s\n", s)
		}
		w("\n")
	}
	if len(p.Traps) > 0 {
		w("## Pitfalls found in this repository\n\n")
		for _, s := range p.Traps {
			w("- %s\n", s)
		}
		w("\n")
	}
	if p.Flow.Choice != "" {
		w("## Suggested flow: `%s`\n\n%s\n\n", p.Flow.Choice, p.Flow.Why)
		w("The flow is yours to decide — this is a reading of the context, not an order.\n\n")
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
