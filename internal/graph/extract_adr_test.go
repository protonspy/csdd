package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeADRFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"docs/plans/dr/plan.md": `---
name: dr
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | store | Storage layer | — | M1 | | adr:pick-store adr:ghost |
`,
		"docs/adr/0001-pick-store.md": "# Store under docs/adr\n\nContext, decision, why.\n",
		"docs/adr/0002-old-way.md":    "---\nstatus: superseded\nsuperseded-by: 0003\n---\n# The old way\n\nreplaced\n",
		"docs/adr/0003-new-way.md":    "# The new way\n\nthe successor\n",
		"docs/adr/0004-orphan.md":     "# An orphan decision\n\nnobody cites this\n",
		"docs/adr/README.md":          "# Decision records\n\nindex, not an ADR\n",
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

func buildADRFixture(t *testing.T) (*Graph, string) {
	t.Helper()
	dir := t.TempDir()
	writeADRFixture(t, dir)
	ex := []Extractor{&planExtractor{}, &adrExtractor{}, &wikiExtractor{}}
	g, err := BuildWith(dir, ex)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return g, dir
}

func TestADRExtractorNodesAndEdges(t *testing.T) {
	g, _ := buildADRFixture(t)

	// adr node with attrs.
	n := nodeByID(g, adrNodeID("pick-store"))
	if n == nil || n.FileType != TypeADR {
		t.Fatalf("adr node missing or wrong type: %+v", n)
	}
	if num, _ := attrInt(n.Attrs["number"]); num != 1 {
		t.Errorf("adr number attr = %v, want 1", n.Attrs["number"])
	}
	if n.Attrs["status"] != "accepted" {
		t.Errorf("adr status attr = %v, want accepted", n.Attrs["status"])
	}
	if n.Attrs["title"] != "Store under docs/adr" {
		t.Errorf("adr title attr = %v", n.Attrs["title"])
	}

	// README.md must not become an adr node (or a wiki page).
	if nodeByID(g, adrNodeID("readme")) != nil {
		t.Errorf("README.md should not be an adr node")
	}

	// feat cites adr (resolved).
	if !hasEdge(g, featNodeID("dr", "store"), RelCites, adrNodeID("pick-store")) {
		t.Errorf("missing feat cites adr edge")
	}
	// superseded_by edge 0002 → 0003.
	if !hasEdge(g, adrNodeID("old-way"), RelSupersededBy, adrNodeID("new-way")) {
		t.Errorf("missing adr superseded_by adr edge")
	}
}

func TestADRExtractorFindings(t *testing.T) {
	g, dir := buildADRFixture(t)
	report := Analyze(g, dir)
	has := func(kind, sub string) bool {
		for _, f := range report.Findings {
			if f.Kind == kind && (sub == "" || strings.Contains(f.Message, sub)) {
				return true
			}
		}
		return false
	}
	if !has(kindBrokenDecisionRef, "adr:ghost") {
		t.Errorf("expected broken_decision_ref for adr:ghost")
	}
	if !has(kindOrphanDecision, "") {
		t.Errorf("expected orphan_decision for 0004-orphan")
	}
	// pick-store IS cited → not orphan.
	for _, f := range report.Findings {
		if f.Kind == kindOrphanDecision && f.NodeID == adrNodeID("pick-store") {
			t.Errorf("pick-store is cited; should not be orphan")
		}
	}
	// The broken decision ref is plan-corpus (parity with broken_plan_ref).
	for _, f := range report.Findings {
		if f.Kind == kindBrokenDecisionRef && f.Corpus != corpusPlan {
			t.Errorf("broken_decision_ref should be plan corpus, got %q", f.Corpus)
		}
	}
}
