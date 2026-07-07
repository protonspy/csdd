package graph

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
)

// adrExtractor indexes the decision records under docs/adr/. Each well-formed
// NNNN-<slug>.md becomes an `adr` node (id from its slug — the citation currency
// a feat's `adr:<slug>` ref resolves to); the superseded-by number is carried as
// a node attr and resolved into an `adr superseded_by adr` edge at assembly, where
// the full number→node map is available (the assembleSpecOwnership precedent).
//
// The extractor claims the whole docs/adr/ subtree so a README index or a
// malformed file is never mis-indexed as a wiki page — it is claimed and returns
// no fragment. It must dispatch BEFORE the wiki extractor, which would otherwise
// treat every docs/ markdown file as a page.
type adrExtractor struct{}

const adrPrefix = "docs/adr/"

// reADRGraphFile mirrors internal/plan's filename grammar; the graph re-parses
// independently so it carries no dependency on the plan feature package (the
// self-containment extract_plan.go keeps toward internal/plan).
var reADRGraphFile = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)

func (adrExtractor) Matches(p string) bool {
	return strings.HasPrefix(p, adrPrefix) && strings.HasSuffix(p, ".md")
}

func (adrExtractor) Extract(src Source) ([]Fragment, error) {
	name := src.Path[len(adrPrefix):]
	if strings.Contains(name, "/") {
		return nil, nil // nested files under docs/adr/ are not records
	}
	m := reADRGraphFile.FindStringSubmatch(name)
	if m == nil {
		return nil, nil // README, malformed filenames: claimed, not indexed
	}
	number, _ := strconv.Atoi(m[1])
	slug := m[2]

	fm := frontmatter.Parse(string(src.Content))
	status := strings.ToLower(strings.TrimSpace(fm.AsString("status", "")))
	if status == "" {
		status = "accepted"
	}
	title := adrTitle(fm.Body)
	label := title
	if label == "" {
		label = slug
	}

	attrs := map[string]any{"number": number, "status": status, "slug": slug}
	if title != "" {
		attrs["title"] = title
	}
	if sb := strings.TrimSpace(fm.AsString("superseded-by", "")); sb != "" {
		if n, err := strconv.Atoi(sb); err == nil {
			attrs["superseded_by"] = n
		}
	}
	return []Fragment{{Nodes: []Node{{
		ID: adrNodeID(slug), Label: label, FileType: TypeADR,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: attrs,
	}}}}, nil
}

// adrNodeID is the canonical id for a decision record — slug-based, so a feat's
// `adr:<slug>` citation (extract_plan's cites edge) resolves to it by slug.
func adrNodeID(slug string) string { return MakeID("adr", slug) }

// adrTitle returns the first `# ` heading of an ADR body, or "".
func adrTitle(body string) string {
	for _, raw := range normLines([]byte(body)) {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return ""
}

// resolveADRSupersession synthesizes `adr superseded_by adr` edges from each
// record's superseded_by number attr, using the whole-graph number→node map. A
// number that names no record produces no edge (validate reports it as a dangling
// supersession); the graph carries only resolvable links.
func resolveADRSupersession(g *Graph) {
	byNumber := map[int]string{}
	for _, n := range g.Nodes {
		if n.FileType == TypeADR {
			if num, ok := attrInt(n.Attrs["number"]); ok {
				byNumber[num] = n.ID
			}
		}
	}
	for _, n := range g.Nodes {
		if n.FileType != TypeADR {
			continue
		}
		target, ok := attrInt(n.Attrs["superseded_by"])
		if !ok {
			continue
		}
		if tid, found := byNumber[target]; found && tid != n.ID {
			g.Links = append(g.Links, Edge{
				Source: n.ID, Target: tid, Relation: RelSupersededBy,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: n.SourceFile,
			})
		}
	}
	g.Links = dedupEdges(g.Links)
}

// attrInt coerces a node attribute to an int, tolerating both the int an
// extractor writes in-memory and the float64 a reloaded graph carries after JSON
// round-tripping.
func attrInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	}
	return 0, false
}
