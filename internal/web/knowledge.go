package web

import (
	"os"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/glossary"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/plan"
)

// Read models for the three knowledge-base surfaces that had no HTTP route:
// decision records, the tech contract, and the glossary. Each mirrors what the
// CLI already parses — the parsers are imported, never re-implemented, so the
// dashboard and the gates cannot drift into disagreeing about what a workspace
// says.
//
// Every one of them carries `cited_by`: the feats whose Refs cell points at this
// record/row/term. That is the reverse of the citation, and it is the thing a
// reader actually wants ("who depends on this decision?").

// webADR is one decision record. SupersededBySlug is resolved here rather than
// in the UI so a superseded record can link straight at its successor.
type webADR struct {
	Number           int      `json:"number"`
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	Body             string   `json:"body"`
	Status           string   `json:"status"`
	SupersededBy     int      `json:"superseded_by,omitempty"`
	SupersededBySlug string   `json:"superseded_by_slug,omitempty"`
	File             string   `json:"file"`
	CitedBy          []string `json:"cited_by"`
}

// adrOverview is GET /api/adr. Present is false when docs/adr/ has not been
// created, so the UI can explain the triple gate instead of showing an empty list.
type adrOverview struct {
	Present bool     `json:"present"`
	Records []webADR `json:"records"`
}

// webStackRow is one row of the docs/stack.md Decided table. Name is the
// citation key — what a `stack:<name>` token has to match.
type webStackRow struct {
	Name    string   `json:"name"`
	Domain  string   `json:"domain"`
	Choice  string   `json:"choice"`
	Version string   `json:"version"`
	Why     string   `json:"why"`
	Refs    []string `json:"refs"`
	CitedBy []string `json:"cited_by"`
}

// stackOverview is GET /api/stack.
type stackOverview struct {
	Present bool          `json:"present"`
	Rows    []webStackRow `json:"rows"`
}

// webTerm is one glossary entry: the canonical name, its definition, and the
// synonyms it bans.
type webTerm struct {
	Canonical  string   `json:"canonical"`
	Cluster    string   `json:"cluster,omitempty"`
	Definition string   `json:"definition"`
	Avoid      []string `json:"avoid,omitempty"`
	Line       int      `json:"line,omitempty"`
	CitedBy    []string `json:"cited_by"`
}

// glossaryOverview is GET /api/glossary.
type glossaryOverview struct {
	Present bool      `json:"present"`
	Terms   []webTerm `json:"terms"`
	Issues  []string  `json:"issues,omitempty"`
}

// citationIndex is who cites what, derived from every plan's feat table — the
// one place in the workspace where citations are machine-readable. Keys match
// how each kind resolves: an ADR by slug, a technology by its normalized name,
// a wiki page by its slug.
type citationIndex struct {
	adr   map[string][]string
	stack map[string][]string
	wiki  map[string][]string
	// planOf maps a feat slug to the plan holding it, so a `feat:` citation can
	// be routed without a second scan.
	planOf map[string]string
}

func newCitationIndex() citationIndex {
	return citationIndex{
		adr:    map[string][]string{},
		stack:  map[string][]string{},
		wiki:   map[string][]string{},
		planOf: map[string]string{},
	}
}

// loadCitations walks every plan once. A plan that fails to load is skipped —
// the index stays useful rather than the whole page failing on one bad file.
func loadCitations(root string) citationIndex {
	idx := newCitationIndex()
	slugs, err := plan.List(root)
	if err != nil {
		return idx
	}
	add := func(m map[string][]string, key, citer string) {
		if key == "" {
			return
		}
		for _, existing := range m[key] {
			if existing == citer {
				return
			}
		}
		m[key] = append(m[key], citer)
	}
	for _, planSlug := range slugs {
		doc, err := plan.Load(root, planSlug)
		if err != nil {
			continue
		}
		for _, f := range doc.Feats {
			citer := "feat:" + f.Slug
			idx.planOf[f.Slug] = planSlug
			for _, s := range f.ADRRefs {
				add(idx.adr, s, citer)
			}
			for _, s := range f.StackRefs {
				add(idx.stack, plan.NormalizeTechName(s), citer)
			}
			for _, s := range f.WikiRefs {
				add(idx.wiki, wikiLinkBase(s), citer)
			}
		}
	}
	return idx
}

// loadADRs reads docs/adr/ into the read model, newest-numbered last (the order
// the directory is meant to be read in — it is append-only).
func loadADRs(root string, idx citationIndex) adrOverview {
	set := plan.ScanADRs(root)
	ov := adrOverview{Present: set.Present, Records: []webADR{}}
	bySlug := map[int]string{}
	for _, a := range set.All {
		bySlug[a.Number] = a.Slug
	}
	for _, a := range set.All {
		rec := webADR{
			Number: a.Number, Slug: a.Slug, Title: a.Title, Body: a.Body,
			Status: a.Status, SupersededBy: a.SupersededBy, File: a.File,
			CitedBy: idx.adr[a.Slug],
		}
		if rec.CitedBy == nil {
			rec.CitedBy = []string{}
		}
		if a.SupersededBy != 0 {
			rec.SupersededBySlug = bySlug[a.SupersededBy]
		}
		ov.Records = append(ov.Records, rec)
	}
	sort.SliceStable(ov.Records, func(i, j int) bool { return ov.Records[i].Number < ov.Records[j].Number })
	return ov
}

// loadStack reads the docs/stack.md Decided table. Rows keep the table's own
// order — the contract is a document, and its order is editorial.
func loadStack(root string, idx citationIndex) stackOverview {
	ov := stackOverview{Rows: []webStackRow{}}
	if _, err := os.Stat(paths.Stack(root)); err != nil {
		return ov
	}
	ov.Present = true
	for name, row := range plan.DecidedRows(root) {
		cited := idx.stack[name]
		if cited == nil {
			cited = []string{}
		}
		ov.Rows = append(ov.Rows, webStackRow{
			Name: name, Domain: row.Domain, Choice: row.Choice, Version: row.Version,
			Why: row.Why, Refs: refTokensIn(row.Refs), CitedBy: cited,
		})
	}
	// DecidedRows is a map, so impose a stable order: domain, then choice.
	sort.SliceStable(ov.Rows, func(i, j int) bool {
		if ov.Rows[i].Domain != ov.Rows[j].Domain {
			return ov.Rows[i].Domain < ov.Rows[j].Domain
		}
		return ov.Rows[i].Choice < ov.Rows[j].Choice
	})
	return ov
}

// loadGlossary reads docs/glossary.md. CitedBy is left empty: glossary terms are
// matched against identifiers by the linter, not cited by a Refs token, so there
// is no citation to reverse.
func loadGlossary(root string) glossaryOverview {
	g := glossary.Load(root)
	ov := glossaryOverview{Present: g.Present, Terms: []webTerm{}}
	for _, t := range g.Terms {
		ov.Terms = append(ov.Terms, webTerm{
			Canonical: t.Canonical, Cluster: t.Cluster, Definition: t.Definition,
			Avoid: t.Avoid, Line: t.Line, CitedBy: []string{},
		})
	}
	for _, iss := range g.Issues() {
		ov.Issues = append(ov.Issues, iss.Msg)
	}
	return ov
}

// refTokensIn pulls the citation tokens out of a free-text cell (a stack row's
// Refs column), dropping everything that is not a token. Whitespace-separated,
// exactly as `csdd plan validate` reads a feat's Refs cell.
func refTokensIn(cell string) []string {
	out := []string{}
	for _, field := range strings.Fields(cell) {
		if kind, _ := parseRefToken(field); kind != "" {
			out = append(out, field)
		}
	}
	return out
}
