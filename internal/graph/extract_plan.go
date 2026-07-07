package graph

import (
	"path"
	"regexp"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
)

// planExtractor indexes the plan layer under docs/plans/<slug>/: the plan.md
// becomes a plan node plus a feat node per row of its `## Feats` table, wired to
// the specs they plan, the feats they depend on, the wiki pages they reference,
// and the tech they use. It claims the whole <slug>/ subtree so seeds and state
// files are never mis-indexed as wiki pages (a seed requirements.md would
// otherwise collide with real wiki pages on its base name).
//
// Extraction performs no I/O (the Extractor contract), so a feat's `plans` edge
// is emitted unconditionally and resolves at assembly iff the spec exists; a
// feat whose spec is not yet generated simply has an unresolved (silently
// dropped) plans edge — the normal pre-generation state, not a finding.
type planExtractor struct{}

const plansPrefix = "docs/plans/"

func (planExtractor) Matches(p string) bool {
	if !strings.HasPrefix(p, plansPrefix) {
		return false
	}
	// Only files inside a <slug>/ subdirectory are the structured plan layout;
	// a top-level docs/plans/PLAN-*.md draft stays an ordinary wiki page.
	return strings.Contains(p[len(plansPrefix):], "/")
}

func (planExtractor) Extract(src Source) ([]Fragment, error) {
	if path.Base(src.Path) != "plan.md" {
		return nil, nil // seeds, plan.json, log.md are claimed but not indexed
	}
	slug := planSlugFromPath(src.Path)
	if slug == "" {
		return nil, nil
	}
	return extractPlan(slug, src), nil
}

// planSlugFromPath extracts <slug> from docs/plans/<slug>/plan.md.
func planSlugFromPath(p string) string {
	rest := strings.TrimPrefix(p, plansPrefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return ""
}

func planNodeID(slug string) string       { return MakeID("plan", slug) }
func featNodeID(slug, feat string) string { return MakeID("feat", slug, feat) }

// planFeatRow is one parsed feat, kept local to the graph so extraction stays
// self-contained (the same posture extract_spec.go takes toward the validator).
type planFeatRow struct {
	num       string
	slug      string
	objective string
	depends   []string
	milestone string
	parallel  bool
	wikiRefs  []string
	stackRefs []string
	adrRefs   []string
	line      int
}

func extractPlan(slug string, src Source) []Fragment {
	fm := frontmatter.Parse(string(src.Content))
	name := fm.AsString("name", "")
	if name == "" {
		name = slug
	}
	planID := planNodeID(slug)
	planAttrs := map[string]any{}
	if s := fm.AsString("status", ""); s != "" {
		planAttrs["status"] = s
	}
	frag := Fragment{Nodes: []Node{{
		ID: planID, Label: name, FileType: TypePlan,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: planAttrs,
	}}}

	rows := parsePlanFeats(src.Content)
	known := map[string]bool{}
	for _, r := range rows {
		known[r.slug] = true
	}
	for _, r := range rows {
		fid := featNodeID(slug, r.slug)
		attrs := map[string]any{"plan": slug, "parallel": r.parallel}
		if r.num != "" {
			attrs["number"] = r.num
		}
		if r.milestone != "" {
			attrs["milestone"] = r.milestone
		}
		if len(r.wikiRefs) > 0 {
			attrs["wiki_refs"] = r.wikiRefs
		}
		if len(r.stackRefs) > 0 {
			attrs["stack_refs"] = r.stackRefs
		}
		if len(r.adrRefs) > 0 {
			attrs["adr_refs"] = r.adrRefs
		}
		// The feat slug is carried explicitly so the glossary assembly pass can
		// token-match it against canonical terms (the node id is normalized and
		// the label is the objective, so neither recovers the slug reliably).
		attrs["slug"] = r.slug
		label := r.slug
		if r.objective != "" {
			label = r.objective
		}
		frag.Nodes = append(frag.Nodes, Node{
			ID: fid, Label: label, FileType: TypeFeat,
			SourceFile: src.Path, SourceLocation: loc(r.line - 1), Attrs: attrs,
		})
		// plan owns feat.
		frag.Edges = append(frag.Edges, Edge{
			Source: planID, Target: fid, Relation: RelOwns,
			Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
		})
		// feat plans spec (resolves iff the spec folder exists).
		frag.Edges = append(frag.Edges, Edge{
			Source: fid, Target: specNodeID(r.slug), Relation: RelPlans,
			Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: r.slug,
		})
		// feat depends_on feat — only for deps that name a real feat of this plan,
		// so the graph never carries a dangling dependency (plan validate reports
		// unknown deps separately).
		for _, dep := range r.depends {
			if known[dep] {
				frag.Edges = append(frag.Edges, Edge{
					Source: fid, Target: featNodeID(slug, dep), Relation: RelDependsOn,
					Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: dep,
				})
			}
		}
		// feat references wiki pages; broken ones are surfaced from the node Attrs
		// in analyze (planFindings), not from a dangling edge.
		for _, page := range r.wikiRefs {
			frag.Edges = append(frag.Edges, Edge{
				Source: fid, Target: wikiTargetID(page), Relation: RelReferences,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: page,
			})
		}
		// feat uses_tech.
		for _, tech := range r.stackRefs {
			frag.Edges = append(frag.Edges, Edge{
				Source: fid, Target: techID(tech), Relation: RelUsesTech,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: tech,
			})
		}
		// feat cites adr; a broken citation is surfaced from the node's adr_refs
		// attr in analyze (planFindings), not from a dangling edge — the wiki-ref
		// precedent above.
		for _, ref := range r.adrRefs {
			frag.Edges = append(frag.Edges, Edge{
				Source: fid, Target: adrNodeID(ref), Relation: RelCites,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: ref,
			})
		}
	}
	return []Fragment{frag}
}

// parsePlanFeats scans a plan.md for its `## Feats` table and returns one row per
// feat, matched by header column name. It mirrors the internal/plan grammar; the
// graph re-parses independently so it carries no dependency on the plan feature
// package (the same self-containment extract_spec.go keeps toward the validator).
func parsePlanFeats(content []byte) []planFeatRow {
	lines := normLines(content)
	section := ""
	var cols map[string]int
	var out []planFeatRow
	for i, raw := range lines {
		trimmed := strings.TrimSpace(rtrim(raw))
		if strings.HasPrefix(trimmed, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			cols = nil
			continue
		}
		if section != "feats" || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitCells(trimmed)
		if isTableSeparator(cells) {
			continue
		}
		if cols == nil {
			cols = map[string]int{}
			for idx, c := range cells {
				cols[strings.ToLower(strings.TrimSpace(c))] = idx
			}
			continue
		}
		get := func(name string) string {
			idx, ok := cols[name]
			if !ok || idx >= len(cells) {
				return ""
			}
			return strings.TrimSpace(cells[idx])
		}
		slug := get("feat")
		if slug == "" {
			continue
		}
		r := planFeatRow{
			num:       get("#"),
			slug:      slug,
			objective: collapseSpaces(get("objective")),
			depends:   planSplitDepends(get("depends")),
			milestone: get("milestone"),
			parallel:  planParallel(get("(p)")),
			line:      i + 1,
		}
		r.wikiRefs, r.stackRefs, r.adrRefs = planTokenizeRefs(get("refs"))
		out = append(out, r)
	}
	return out
}

func planSplitDepends(cell string) []string {
	cell = strings.TrimSpace(cell)
	if cell == "" || cell == "-" || cell == "—" || strings.EqualFold(cell, "none") {
		return nil
	}
	var out []string
	for _, part := range strings.Split(cell, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func planParallel(cell string) bool {
	switch strings.ToLower(strings.TrimSpace(cell)) {
	case "p", "(p)", "yes", "y", "x", "true", "✓", "*":
		return true
	}
	return false
}

// planTokenizeRefs splits a Refs cell on whitespace into [[wiki]], stack:, and
// adr: tokens, reusing the wiki-link regex so a plan ref and a wiki body resolve
// the same target. Only well-formed (kebab-case) adr slugs are returned, so the
// graph never mints a cites edge to a garbage id (validate reports the malformed
// slug against the citing feat).
func planTokenizeRefs(cell string) (wiki, stack, adr []string) {
	for _, tok := range strings.Fields(cell) {
		if m := reWikiLink.FindStringSubmatch(tok); m != nil {
			if t := strings.TrimSpace(m[1]); t != "" && !strings.ContainsAny(t, "<>") {
				wiki = append(wiki, t)
			}
			continue
		}
		if strings.HasPrefix(tok, "stack:") {
			if name := strings.TrimSpace(strings.TrimPrefix(tok, "stack:")); name != "" {
				stack = append(stack, name)
			}
			continue
		}
		if strings.HasPrefix(tok, "adr:") {
			if slug := strings.TrimSpace(strings.TrimPrefix(tok, "adr:")); reADRRefSlug.MatchString(slug) {
				adr = append(adr, slug)
			}
		}
	}
	return wiki, stack, adr
}

// reADRRefSlug is the kebab-case grammar a graph-side adr citation must satisfy
// before it becomes a cites edge (mirrors internal/plan's ValidADRSlug).
var reADRRefSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
