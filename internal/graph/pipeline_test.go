package graph

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFixture materializes a small but complete csdd workspace under dir,
// crafted to trigger every analyze/wiki-lint finding at least once.
func writeFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"specs/photos/spec.json": `{"feature_name":"photos","language":"go","phase":"tasks","development_flow":"tdd","ready_for_implementation":false}`,
		"specs/photos/requirements.md": `# Requirements
## Requirements
### Requirement 1: Upload
**Objective**
As a user, I want uploads.
**Acceptance Criteria**
1. WHEN a photo is uploaded THEN the system SHALL store it.
2. IF the file is too large THEN the system SHALL reject it.

### Requirement 2: View
**Acceptance Criteria**
1. WHEN a user views THE SYSTEM SHALL list photos.
2. WHERE albums exist THE SYSTEM SHALL group them.
`,
		"specs/photos/design.md": `# Design
## Requirements Traceability
| Requirement | Components | Interfaces | Flows |
|---|---|---|---|
| 1.1 | AlbumService | upload() | upload-flow |
| 3.1-3.5 | RangeComp | x() | y-flow |

## Components and Interfaces

### AlbumService
- **Requirements**: 1.1
- **Dependencies**:
  - Inbound: API
  - External: aws-s3

### GhostService
- **Requirements**: 2.1
`,
		"specs/photos/tasks.md": `# Tasks
## Phase 1: Foundation
- [ ] 1. Implement upload _Boundary: AlbumService_
  - _Requirements: 1.1_
- [ ] 2. Do some plumbing _Boundary: AlbumService_
- [ ] 3. Bad reference
  - _Requirements: 9.9_
- [ ] 4. First cycle node
  - _Requirements: 1.2_
  - _Depends: 5_
- [ ] 5. Second cycle node
  - _Requirements: 2.1_
  - _Depends: 4_
`,
		".claude/skills/wiki/SKILL.md": "---\nname: wiki\ndescription: wiki skill\n---\n# Wiki\n",
		".claude/agents/reviewer.md":   "---\nname: reviewer\ndescription: reviews\nmodel: sonnet\n---\n# Reviewer\n",
		".claude/rules/ears.md":        "# EARS\nUse SHALL.\n",
		".mcp.json":                    `{"mcpServers":{"csdd":{"command":"npx","args":["-y","@protonspy/csdd-mcp"]}}}`,
		"CLAUDE.md":                    "# Project\nEntry point.\n",
		"docs/wiki/index.md": `# Wiki Index
## Pages
- [Overview](pages/overview.md)
- [Ghost](pages/ghost.md)
`,
		"docs/wiki/log.md": `# Log
## [2026-07-06] ingest | good entry
## [bad] wrong format
`,
		"docs/wiki/pages/overview.md": `---
title: Overview
sources: [docs/raw/article.md]
tags: [core]
---
# Overview
See [[Details]] and [[Nonexistent]].
Also see ` + "`internal/graph/model.go`" + ` and ` + "`internal/nope/ghost.go`" + `.
`,
		"docs/wiki/pages/details.md": "# Details\nSome detail here.\n",
		"docs/wiki/pages/orphan.md":  "# Orphan\nNobody links here.\n",
		"docs/raw/article.md":        "Raw article content.\n",
		"docs/raw/unused.md":         "Unprocessed raw content.\n",
		// A real repo file so a reuses edge resolves.
		"internal/graph/model.go": "package graph\n",
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

func buildFixture(t *testing.T) (*Graph, string) {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir)
	// Exclude the go extractor so a lone `package graph` file does not add code
	// nodes to this spec/wiki-focused fixture (its resolution is tested separately).
	ex := []Extractor{&specExtractor{}, &claudeExtractor{}, &stackExtractor{}, &wikiExtractor{}}
	g, err := BuildWith(dir, ex)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return g, dir
}

func nodeByID(g *Graph, id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

func hasEdge(g *Graph, src, rel, tgt string) bool {
	for _, e := range g.Links {
		if e.Source == src && e.Relation == rel && e.Target == tgt {
			return true
		}
	}
	return false
}

func TestBuildNodesAndEdges(t *testing.T) {
	g, _ := buildFixture(t)

	// Spec node exists (from spec.json) and owns its children (R1.1).
	if n := nodeByID(g, "spec_photos"); n == nil || n.FileType != TypeSpec {
		t.Fatalf("missing spec node; got %v", n)
	}
	if !hasEdge(g, "spec_photos", RelOwns, requirementID("photos", "1")) {
		t.Errorf("spec should own requirement 1")
	}
	if !hasEdge(g, "spec_photos", RelOwns, taskID("photos", "1")) {
		t.Errorf("spec should own task 1")
	}

	// Criteria + has_criterion (R1.2, R1.3).
	c11 := criterionID("photos", "1_1")
	if nodeByID(g, c11) == nil {
		t.Fatalf("missing criterion 1.1 (%s)", c11)
	}
	if !hasEdge(g, requirementID("photos", "1"), RelHasCriterion, c11) {
		t.Errorf("requirement 1 should have criterion 1.1")
	}

	// realizes is EXTRACTED with score 1.0 (R1.3).
	var found bool
	for _, e := range g.Links {
		if e.Source == taskID("photos", "1") && e.Relation == RelRealizes && e.Target == c11 {
			found = true
			if e.Confidence != Extracted || e.ConfidenceScore != 1.0 {
				t.Errorf("realizes edge should be EXTRACTED 1.0; got %s %v", e.Confidence, e.ConfidenceScore)
			}
		}
	}
	if !found {
		t.Errorf("missing realizes edge task 1 → criterion 1.1")
	}

	// Traceability: traced_to / implements / exercises.
	if !hasEdge(g, c11, RelTracedTo, designID("photos", "AlbumService")) {
		t.Errorf("missing traced_to crit 1.1 → AlbumService")
	}
	if !hasEdge(g, designID("photos", "AlbumService"), RelImplements, interfaceID("photos", "upload()")) {
		t.Errorf("missing implements AlbumService → upload()")
	}
	if !hasEdge(g, c11, RelExercises, flowID("photos", "upload-flow")) {
		t.Errorf("missing exercises crit 1.1 → upload-flow")
	}

	// in_boundary from task 1.
	if !hasEdge(g, taskID("photos", "1"), RelInBoundary, designID("photos", "AlbumService")) {
		t.Errorf("missing in_boundary task 1 → AlbumService")
	}

	// depends_on both directions (the cycle).
	if !hasEdge(g, taskID("photos", "4"), RelDependsOn, taskID("photos", "5")) {
		t.Errorf("missing depends_on 4 → 5")
	}

	// Task parallel/status attrs.
	if n := nodeByID(g, taskID("photos", "1")); n == nil || n.Attrs["status"] != "pending" {
		t.Errorf("task 1 status wrong: %v", n)
	}

	// .claude nodes.
	for _, id := range []string{"skill_wiki", "agent_reviewer", "mcp_csdd", "entry_claude_md"} {
		if nodeByID(g, id) == nil {
			t.Errorf("missing .claude node %s", id)
		}
	}

	// wiki + raw nodes.
	if nodeByID(g, "wiki_overview") == nil || nodeByID(g, "raw_article_md") == nil {
		t.Errorf("missing wiki/raw nodes")
	}
	// derived_from provenance from frontmatter sources:.
	if !hasEdge(g, "wiki_overview", RelDerivedFrom, "raw_article_md") {
		t.Errorf("missing derived_from overview → article")
	}
	// links_to wikilink resolves to the details page.
	if !hasEdge(g, "wiki_overview", RelLinksTo, "wiki_details") {
		t.Errorf("missing links_to overview → details")
	}

	// A cited path that exists → reuses (EXTRACTED); one that doesn't → references.
	reuseOK, refOK := false, false
	for _, e := range g.Links {
		if e.Target == codeRefID("internal/graph/model.go") && e.Relation == RelReuses && e.Confidence == Extracted {
			reuseOK = true
		}
		if e.Target == codeRefID("internal/nope/ghost.go") && e.Relation == RelReferences {
			refOK = true
		}
	}
	if !reuseOK {
		t.Errorf("cited existing path should be a reuses/EXTRACTED edge")
	}
	if !refOK {
		t.Errorf("cited missing path should be a references edge")
	}
}

func TestByteStableDoubleBuild(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir)
	ex := []Extractor{&specExtractor{}, &claudeExtractor{}, &stackExtractor{}, &wikiExtractor{}, &goExtractor{}}

	g1, err := BuildWith(dir, ex)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := Marshal(g1)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := BuildWith(dir, ex)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Marshal(g2)
	if err != nil {
		t.Fatal(err)
	}
	if string(d1) != string(d2) {
		t.Fatalf("double build not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", d1, d2)
	}
}

func TestAnalyzeFindings(t *testing.T) {
	g, dir := buildFixture(t)
	report := Analyze(g, dir)
	got := map[string]int{}
	for _, f := range report.Findings {
		got[f.Kind]++
	}
	want := []string{
		kindUntestedRequirement, // criterion 2.2
		kindOrphanTask,          // task 2
		kindUnimplementedCompo,  // GhostService / RangeComp
		kindPendingReference,    // task 3 → 9.9
		kindRangeShorthand,      // traceability 3.1-3.5
		kindDependencyCycle,     // tasks 4 ↔ 5
		kindBrokenWikilink,      // [[Nonexistent]]
		kindOrphanPage,          // orphan.md
		kindIndexMissingPage,    // details.md / orphan.md
		kindIndexDanglingEntry,  // pages/ghost.md
		kindLogFormat,           // "## [bad] wrong format"
		kindUnprocessedSource,   // docs/raw/unused.md
		kindNoTechContract,      // no docs/stack.md
	}
	for _, k := range want {
		if got[k] == 0 {
			t.Errorf("expected at least one %q finding; findings: %+v", k, report.Findings)
		}
	}

	// Untested must specifically flag criterion 2.2 and NOT 1.1 (which is realized).
	untested := map[string]bool{}
	for _, f := range report.Findings {
		if f.Kind == kindUntestedRequirement {
			untested[f.NodeID] = true
		}
	}
	if !untested[criterionID("photos", "2_2")] {
		t.Errorf("criterion 2.2 should be untested")
	}
	if untested[criterionID("photos", "1_1")] {
		t.Errorf("criterion 1.1 is realized and must not be untested")
	}
}

func TestWriteBuildDeterministicAndLog(t *testing.T) {
	g, dir := buildFixture(t)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	d1, err := WriteBuild(dir, g, now)
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(GraphJSONPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(d1) {
		t.Errorf("written file differs from returned bytes")
	}
	// log.md gained a build heading in the documented format.
	logData, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(GraphLogRel)))
	if err != nil {
		t.Fatal(err)
	}
	if !reLogEntry.MatchString(firstBuildHeading(string(logData))) {
		t.Errorf("log heading not in documented format: %q", firstBuildHeading(string(logData)))
	}
}

func firstBuildHeading(s string) string {
	for _, line := range splitLines(s) {
		if len(line) > 4 && line[:4] == "## [" {
			return line
		}
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
