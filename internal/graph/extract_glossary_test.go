package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGlossaryFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"docs/glossary.md": `## Language

**Feat**: One row of a plan's Feats table; becomes exactly one spec.
_Avoid_: feature, story

**Customer**: A person who buys.
_Avoid_: client

**Widget**: An unused term nobody references.
`,
		// A plan whose feat slug uses the canonical term "feat" and another feat
		// whose slug uses an avoided alias "client".
		"docs/plans/g/plan.md": `---
name: g
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | feat-store | store feats | — | M1 | | |
| 2 | thumbs | thumbs | — | M1 | | |
`,
		// A wiki page whose name uses an avoided alias.
		"docs/wiki/pages/client-portal.md": "# Client Portal\n",
		// A spec directory whose name uses the canonical customer term.
		"specs/customer-sync/spec.json": `{"feature_name":"customer-sync"}`,
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func buildGlossaryFixture(t *testing.T) (*Graph, string) {
	t.Helper()
	dir := t.TempDir()
	writeGlossaryFixture(t, dir)
	ex := []Extractor{&specExtractor{}, &planExtractor{}, &glossaryExtractor{}, &wikiExtractor{}}
	g, err := BuildWith(dir, ex)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return g, dir
}

func TestGlossaryExtractorNodesAndEdges(t *testing.T) {
	g, _ := buildGlossaryFixture(t)

	// term node with attrs.
	n := nodeByID(g, termNodeID("Feat"))
	if n == nil || n.FileType != TypeTerm {
		t.Fatalf("term node missing or wrong type: %+v", n)
	}
	if !strings.Contains(asString(n.Attrs["definition"]), "one spec") {
		t.Errorf("term definition attr = %v", n.Attrs["definition"])
	}
	if av := attrStrings(n.Attrs["avoid"]); len(av) != 2 {
		t.Errorf("term avoid attr = %v, want 2", n.Attrs["avoid"])
	}

	// references edge: feat-store (canonical "feat") → term Feat.
	if !hasEdge(g, featNodeID("g", "feat-store"), RelReferences, termNodeID("Feat")) {
		t.Errorf("missing feat→term references edge for canonical use")
	}
	// references edge: customer-sync spec (canonical "customer") → term Customer.
	if !hasEdge(g, specNodeID("customer-sync"), RelReferences, termNodeID("Customer")) {
		t.Errorf("missing spec→term references edge")
	}
	// references edge: client-portal wiki page (alias "client") → term Customer.
	if !hasEdge(g, wikiPageID("docs/wiki/pages/client-portal.md"), RelReferences, termNodeID("Customer")) {
		t.Errorf("missing wiki→term references edge for alias use")
	}
}

func TestGlossaryFindings(t *testing.T) {
	g, dir := buildGlossaryFixture(t)
	report := Analyze(g, dir)
	has := func(kind, corpus, sub string) bool {
		for _, f := range report.Findings {
			if f.Kind == kind && (corpus == "" || f.Corpus == corpus) && (sub == "" || strings.Contains(f.Message, sub)) {
				return true
			}
		}
		return false
	}
	// Avoided alias in a wiki page name → corpusWiki (so wiki lint renders it).
	if !has(kindAvoidedTerm, corpusWiki, "client") {
		t.Errorf("expected avoided_term (wiki) for client-portal")
	}
	// The wiki-lint filter surfaces exactly the wiki subset.
	wiki := WikiFindings(report)
	sawAvoided := false
	for _, f := range wiki {
		if f.Kind == kindAvoidedTerm {
			sawAvoided = true
		}
	}
	if !sawAvoided {
		t.Errorf("wiki lint subset should include the avoided-term finding")
	}
	// Orphan term (Widget) is informational.
	if !has(kindOrphanTerm, "", "orphan term") {
		t.Errorf("expected orphan_term for Widget")
	}
	// Feat and Customer are referenced → not orphans.
	for _, f := range report.Findings {
		if f.Kind == kindOrphanTerm && (f.NodeID == termNodeID("Feat") || f.NodeID == termNodeID("Customer")) {
			t.Errorf("%s is referenced; should not be orphan", f.NodeID)
		}
	}
}
