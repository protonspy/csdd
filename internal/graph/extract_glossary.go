package graph

import (
	"path"
	"strings"

	"github.com/protonspy/csdd/internal/glossary"
)

// glossaryExtractor indexes docs/glossary.md — the ubiquitous-language contract.
// Each entry becomes a `term` node (id from the normalized canonical term); the
// `references` edges from the feats, specs, and wiki pages that use each term are
// synthesized at assembly (resolveGlossaryRefs), where every identifier-bearing
// node is present and the glossary can be matched against all of them at once.
//
// It must dispatch BEFORE the wiki extractor, which would otherwise treat
// docs/glossary.md as an ordinary wiki page.
type glossaryExtractor struct{}

func (glossaryExtractor) Matches(p string) bool { return p == "docs/glossary.md" }

func (glossaryExtractor) Extract(src Source) ([]Fragment, error) {
	gl := glossary.Parse(string(src.Content))
	var frag Fragment
	for _, term := range gl.Terms {
		attrs := map[string]any{}
		if term.Definition != "" {
			attrs["definition"] = term.Definition
		}
		if len(term.Avoid) > 0 {
			attrs["avoid"] = term.Avoid
		}
		if term.Cluster != "" {
			attrs["cluster"] = term.Cluster
		}
		frag.Nodes = append(frag.Nodes, Node{
			ID: termNodeID(term.Canonical), Label: term.Canonical, FileType: TypeTerm,
			SourceFile: src.Path, SourceLocation: loc(term.Line - 1), Attrs: attrs,
		})
	}
	return []Fragment{frag}, nil
}

// termNodeID is the canonical id for a glossary term — normalized from the
// canonical term so a feat/spec/wiki identifier that token-matches it resolves to
// the same node regardless of spelling.
func termNodeID(canonical string) string { return MakeID("term", canonical) }

// glossaryIdentifier returns the kebab identifier a node is matched by: the feat
// slug, the spec directory name, or the wiki page filename. Nodes with no
// meaningful identifier return "".
func glossaryIdentifier(n Node) string {
	switch {
	case n.FileType == TypeFeat:
		return asString(n.Attrs["slug"])
	case n.FileType == TypeSpec:
		if m := reSpecMember.FindStringSubmatch(n.SourceFile); m != nil {
			return m[1]
		}
		return n.Label
	case isWikiContentPage(n):
		return strings.TrimSuffix(path.Base(n.SourceFile), ".md")
	}
	return ""
}

// resolveGlossaryRefs synthesizes `references` edges from every feat/spec/wiki
// page whose identifier tokens match a term or one of its aliases (R4.2), so
// "where is <term> used?" is a graph query. Both canonical and alias matches
// produce an edge (deduped per term); the avoided-alias distinction is recomputed
// from the glossary by analyze, which surfaces the lint (R3.1).
func resolveGlossaryRefs(g *Graph, root string) {
	gl := glossary.Load(root)
	if !gl.Present {
		return
	}
	termExists := map[string]bool{}
	for _, n := range g.Nodes {
		if n.FileType == TypeTerm {
			termExists[n.ID] = true
		}
	}
	for _, n := range g.Nodes {
		id := glossaryIdentifier(n)
		if id == "" {
			continue
		}
		seen := map[string]bool{}
		for _, m := range gl.Match(id) {
			tid := termNodeID(m.Canonical)
			if !termExists[tid] || seen[tid] {
				continue
			}
			seen[tid] = true
			g.Links = append(g.Links, Edge{
				Source: n.ID, Target: tid, Relation: RelReferences,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: n.SourceFile, Ref: id,
			})
		}
	}
	g.Links = dedupEdges(g.Links)
}
