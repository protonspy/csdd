package graph

import (
	"os"
	"path/filepath"
	"testing"
)

// writePlanFixture lays down a workspace with a plan, one existing spec (upload)
// and one absent spec (thumbs), a stack contract, and one wiki page, crafted to
// exercise every plan node/edge and every plan finding.
func writePlanFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"docs/plans/photos/plan.md": `---
name: photos
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest photos | — | M1 | | stack:go [[storage-design]] |
| 2 | thumbs | Thumbnails | upload | M1 | P | [[missing-page]] |
| 3 | ring-a | A | ring-b | M2 | | |
| 4 | ring-b | B | ring-a | M2 | | |
`,
		// upload's spec exists → its plans edge resolves; no other feat plans it.
		"specs/upload/spec.json": `{"feature_name":"upload","ready_for_implementation":false}`,
		// unplanned-spot has a spec but no feat plans it → unplanned_spec finding.
		"specs/unplanned-spot/spec.json": `{"feature_name":"unplanned-spot"}`,
		"docs/stack.md": `# Tech contract
## Decided
| Domain | Choice | Version | Why | Refs |
|---|---|---|---|---|
| Language | Go | 1.22 | speed | — |
`,
		"docs/wiki/pages/storage-design.md": "# Storage Design\n",
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

func buildPlanFixture(t *testing.T) (*Graph, string) {
	t.Helper()
	dir := t.TempDir()
	writePlanFixture(t, dir)
	ex := []Extractor{&specExtractor{}, &stackExtractor{}, &planExtractor{}, &wikiExtractor{}}
	g, err := BuildWith(dir, ex)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return g, dir
}

func TestPlanExtractorNodesAndEdges(t *testing.T) {
	g, _ := buildPlanFixture(t)

	planID := planNodeID("photos")
	if n := nodeByID(g, planID); n == nil || n.FileType != TypePlan {
		t.Fatalf("plan node missing or wrong type: %+v", n)
	}
	uploadFeat := featNodeID("photos", "upload")
	if n := nodeByID(g, uploadFeat); n == nil || n.FileType != TypeFeat {
		t.Fatalf("upload feat node missing: %+v", n)
	}

	// plan owns feat.
	if !hasEdge(g, planID, RelOwns, uploadFeat) {
		t.Errorf("missing plan owns feat edge")
	}
	// feat plans spec — resolves because specs/upload/ exists.
	if !hasEdge(g, uploadFeat, RelPlans, specNodeID("upload")) {
		t.Errorf("missing (resolved) feat plans spec edge")
	}
	// feat plans spec for thumbs is dropped (spec absent) — that's the normal
	// pre-generation state, not a finding.
	if hasEdge(g, featNodeID("photos", "thumbs"), RelPlans, specNodeID("thumbs")) {
		t.Errorf("thumbs plans edge should be unresolved/dropped (no spec yet)")
	}
	// feat depends_on feat.
	if !hasEdge(g, featNodeID("photos", "thumbs"), RelDependsOn, uploadFeat) {
		t.Errorf("missing feat depends_on feat edge")
	}
	// feat references wiki page (resolved).
	if !hasEdge(g, uploadFeat, RelReferences, wikiTargetID("storage-design")) {
		t.Errorf("missing feat references wiki edge")
	}
	// feat uses_tech.
	if !hasEdge(g, uploadFeat, RelUsesTech, techID("go")) {
		t.Errorf("missing feat uses_tech edge")
	}
}

func TestPlanExtractorFindings(t *testing.T) {
	g, dir := buildPlanFixture(t)
	report := Analyze(g, dir)

	has := func(kind string) bool {
		for _, f := range report.Findings {
			if f.Kind == kind {
				return true
			}
		}
		return false
	}
	if !has(kindUnplannedSpec) {
		t.Errorf("expected unplanned_spec finding (unplanned-spot)")
	}
	if !has(kindBrokenPlanRef) {
		t.Errorf("expected broken_plan_ref finding ([[missing-page]])")
	}
	if !has(kindFeatDependencyCycle) {
		t.Errorf("expected feat_dependency_cycle finding (ring-a ↔ ring-b)")
	}
	// The feat cycle must NOT be mislabeled as a task cycle.
	for _, f := range report.Findings {
		if f.Kind == kindDependencyCycle {
			t.Errorf("feat cycle should not be reported as a task dependency cycle: %+v", f)
		}
	}
	// upload IS planned → no unplanned finding for it.
	for _, f := range report.Findings {
		if f.Kind == kindUnplannedSpec && f.NodeID == specNodeID("upload") {
			t.Errorf("upload is planned; should not be flagged unplanned")
		}
	}
}
