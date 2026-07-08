package graph

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCommentedTasksExcludedAndInlineDepends covers two extraction fixes: an
// HTML-commented task block never becomes a node, and an inline `_Depends: N_` on
// a task title emits a depends_on edge (parity with the annotation-line form).
func TestCommentedTasksExcludedAndInlineDepends(t *testing.T) {
	dir := t.TempDir()
	tasks := `# Tasks
- [ ] 1. First _Boundary: Comp_
  - _Requirements: 1.1_
- [ ] 2. Second _Depends: 1_
  - _Requirements: 1.1_

<!--
- [ ] 9. Ghost commented task _Boundary: Comp_
  - _Requirements: 9.9_
-->
`
	writeFile(t, dir, "specs/x/spec.json", `{"feature_name":"x","phase":"tasks"}`)
	writeFile(t, dir, "specs/x/tasks.md", tasks)

	g, err := BuildWith(dir, []Extractor{&specExtractor{}})
	if err != nil {
		t.Fatal(err)
	}
	if nodeByID(g, taskID("x", "9")) != nil {
		t.Error("commented-out task 9 must not be indexed")
	}
	if !hasEdge(g, taskID("x", "2"), RelDependsOn, taskID("x", "1")) {
		t.Error("inline _Depends: 1_ on task 2 must emit a depends_on edge")
	}
	// The commented annotation must not surface as a pending reference either.
	for _, p := range g.Pending {
		if p.Ref == "9.9" {
			t.Errorf("commented annotation 9.9 leaked as a pending ref: %+v", p)
		}
	}
}

// TestDetectCollisions asserts distinct artifacts sharing a normalized ID are
// flagged, while a tech node legitimately merged across contract and manifest
// (different labels, same ID) is not.
func TestDetectCollisions(t *testing.T) {
	nodes := []Node{
		{ID: "crit_a_1_1_1", Label: "criterion text one", FileType: TypeCriterion, SourceFile: "specs/a-1/requirements.md"},
		{ID: "crit_a_1_1_1", Label: "criterion text two", FileType: TypeCriterion, SourceFile: "specs/a/requirements.md"},
		{ID: "tech_chi", Label: "chi", FileType: TypeTech, SourceFile: "docs/stack.md"},
		{ID: "tech_chi", Label: "github.com/go-chi/chi/v5", FileType: TypeTech, SourceFile: "go.mod"},
		{ID: "spec_ok", Label: "ok", FileType: TypeSpec, SourceFile: "specs/ok/spec.json"},
	}
	cols := detectCollisions(nodes)
	if len(cols) != 1 {
		t.Fatalf("expected exactly 1 collision (the criterion); got %d: %+v", len(cols), cols)
	}
	if cols[0].ID != "crit_a_1_1_1" {
		t.Errorf("wrong collision id: %s", cols[0].ID)
	}
	if len(cols[0].Labels) != 2 || len(cols[0].Files) != 2 {
		t.Errorf("collision should carry both labels and files: %+v", cols[0])
	}
}

// TestLoadStateDiscardsOldVersion asserts an incremental cache written by a prior
// schema version is discarded (never reused), so a change to extraction logic
// cannot serve stale fragments for a file whose content hash is unchanged.
func TestLoadStateDiscardsOldVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".csdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := `{"version":1,"sources":{"specs/x/tasks.md":{"hash":"h","fragments":[]}}}`
	if err := os.WriteFile(statePath(dir), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	st := loadState(dir)
	if len(st.Sources) != 0 {
		t.Errorf("stale version-1 cache was not discarded: %+v", st.Sources)
	}
	if st.Version != stateVersion {
		t.Errorf("discarded state should carry the current version; got %d", st.Version)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
