package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestQueryIDFRanking checks that a rare term outweighs a common one: a node
// matching only the rare term ranks above a node matching only the common term
// (R13.1 tier×IDF scoring).
func TestQueryIDFRanking(t *testing.T) {
	g := New()
	// "apple" appears in many labels (low IDF); "zebra" in exactly one (high IDF).
	for i := 0; i < 12; i++ {
		g.Nodes = append(g.Nodes, Node{ID: MakeID("apple", itoa(i)), Label: fmt.Sprintf("apple item %d", i), FileType: TypeTask})
	}
	g.Nodes = append(g.Nodes, Node{ID: "zebra_solo", Label: "zebra solo", FileType: TypeTask})

	matches := Query(g, "zebra apple", QueryOptions{Budget: 100000})
	if len(matches) == 0 {
		t.Fatal("expected matches")
	}
	if matches[0].Node.ID != "zebra_solo" {
		t.Fatalf("rare-term node should rank first (IDF); got %s", matches[0].Node.ID)
	}
}

// TestQueryBudget checks that a tight token budget truncates results (R13.1).
func TestQueryBudget(t *testing.T) {
	g := New()
	for i := 0; i < 40; i++ {
		g.Nodes = append(g.Nodes, Node{ID: MakeID("node", itoa(i)), Label: fmt.Sprintf("node number %d here", i), FileType: TypeTask})
	}
	big := Query(g, "node", QueryOptions{Budget: 100000})
	small := Query(g, "node", QueryOptions{Budget: 30})
	if len(small) == 0 {
		t.Fatal("budget should still return at least one match")
	}
	if len(small) >= len(big) {
		t.Fatalf("small budget (%d) should return fewer matches than large (%d)", len(small), len(big))
	}
}

// TestQueryHubGating checks that BFS does not expand *through* a super-hub: the
// hub is a neighbor of a leaf, but the hub's other leaves are not pulled in at
// 2 hops (R13.1 hub-gating, floor 50).
func TestQueryHubGating(t *testing.T) {
	g := New()
	g.Nodes = append(g.Nodes, Node{ID: "hub", Label: "central hub", FileType: TypeDesign})
	for i := 0; i < 60; i++ {
		leaf := MakeID("leaf", itoa(i))
		g.Nodes = append(g.Nodes, Node{ID: leaf, Label: fmt.Sprintf("leaf %d", i), FileType: TypeTask})
		g.Links = append(g.Links, Edge{Source: "hub", Target: leaf, Relation: RelContains, Confidence: Extracted, ConfidenceScore: 1})
	}
	// Query leaf 0 with 2 hops. Without gating, leaf0→hub→(59 other leaves).
	matches := Query(g, "leaf 0", QueryOptions{Hops: 2, Budget: 1000000})
	var leaf0 *Match
	for i := range matches {
		if matches[i].Node.ID == MakeID("leaf", "0") {
			leaf0 = &matches[i]
			break
		}
	}
	if leaf0 == nil {
		t.Fatal("leaf 0 not matched")
	}
	sawHub, sawOtherLeaf := false, false
	for _, nb := range leaf0.Neighbors {
		if nb.Node.ID == "hub" {
			sawHub = true
		}
		if nb.Node.ID == MakeID("leaf", "5") {
			sawOtherLeaf = true
		}
	}
	if !sawHub {
		t.Errorf("hub should be a direct neighbor of leaf 0")
	}
	if sawOtherLeaf {
		t.Errorf("hub-gating should stop BFS expanding through the super-hub to other leaves")
	}
}

// TestIncrementalByteIdentical checks that an incremental build's output is
// byte-identical to a full build, on first run and after an edit (R13.3).
func TestIncrementalByteIdentical(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir)

	full1, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	fullBytes1, _ := Marshal(full1)

	inc1, err := BuildIncremental(dir)
	if err != nil {
		t.Fatal(err)
	}
	incBytes1, _ := Marshal(inc1)
	if string(fullBytes1) != string(incBytes1) {
		t.Fatalf("first incremental build differs from full:\n%s\n---\n%s", fullBytes1, incBytes1)
	}

	// Edit one spec file, then rebuild both ways — still byte-identical.
	tasks := filepath.Join(dir, "specs", "photos", "tasks.md")
	edited := "# Tasks\n## Phase 1\n- [ ] 1. Implement upload _Boundary: AlbumService_\n  - _Requirements: 1.1_\n- [ ] 9. New task\n  - _Requirements: 2.1_\n"
	if err := os.WriteFile(tasks, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	full2, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	fullBytes2, _ := Marshal(full2)
	inc2, err := BuildIncremental(dir)
	if err != nil {
		t.Fatal(err)
	}
	incBytes2, _ := Marshal(inc2)
	if string(fullBytes2) != string(incBytes2) {
		t.Fatalf("post-edit incremental build differs from full:\n%s\n---\n%s", fullBytes2, incBytes2)
	}
	if string(fullBytes1) == string(fullBytes2) {
		t.Fatal("edit should have changed the graph")
	}

	// Delete a source; incremental must prune it and still match full.
	if err := os.Remove(tasks); err != nil {
		t.Fatal(err)
	}
	full3, _ := Build(dir)
	fb3, _ := Marshal(full3)
	inc3, _ := BuildIncremental(dir)
	ib3, _ := Marshal(inc3)
	if string(fb3) != string(ib3) {
		t.Fatalf("post-delete incremental build differs from full")
	}
}
