package graph

import "testing"

func TestQueryTiers(t *testing.T) {
	g, _ := buildFixture(t)

	// Exact label match beats substring; results are deterministic.
	matches := Query(g, "AlbumService", QueryOptions{})
	if len(matches) == 0 {
		t.Fatal("expected AlbumService match")
	}
	if matches[0].Node.ID != designID("photos", "AlbumService") {
		t.Fatalf("expected AlbumService first; got %s", matches[0].Node.ID)
	}
	if matches[0].Tier != tierExact {
		t.Errorf("expected exact tier; got %s", matches[0].Tier)
	}
	// The neighborhood is populated (1-hop).
	if len(matches[0].Neighbors) == 0 {
		t.Errorf("expected neighbors for AlbumService")
	}

	// Prefix vs substring ordering: "Upload" matches task 1's label as a prefix.
	up := Query(g, "Implement", QueryOptions{})
	if len(up) == 0 || up[0].Tier != tierPrefix {
		t.Errorf("expected a prefix match for 'Implement'; got %+v", up)
	}

	// Determinism: same query twice yields identical ordering.
	a := Query(g, "e", QueryOptions{})
	b := Query(g, "e", QueryOptions{})
	if len(a) != len(b) {
		t.Fatalf("nondeterministic length")
	}
	for i := range a {
		if a[i].Node.ID != b[i].Node.ID {
			t.Fatalf("nondeterministic order at %d", i)
		}
	}
}

func TestPath(t *testing.T) {
	g, _ := buildFixture(t)

	// task 1 → AlbumService is a single in_boundary hop.
	pr, err := Path(g, "Implement upload", "AlbumService", 0)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if len(pr.Steps) != 1 {
		t.Fatalf("expected 1 step; got %d (%+v)", len(pr.Steps), pr.Steps)
	}
	if pr.Steps[0].Relation != RelInBoundary || pr.Steps[0].Direction != "forward" {
		t.Errorf("expected forward in_boundary; got %+v", pr.Steps[0])
	}

	// A→B resolving to the same node is an error (R2.2).
	if _, err := Path(g, "AlbumService", "AlbumService", 0); err == nil {
		t.Error("expected error for identical endpoints")
	}
}

func TestExplain(t *testing.T) {
	g, _ := buildFixture(t)
	er, err := Explain(g, "AlbumService")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if er.Node.ID != designID("photos", "AlbumService") {
		t.Fatalf("wrong node: %s", er.Node.ID)
	}
	// Connections are ordered by neighbor degree, descending.
	for i := 1; i < len(er.Connections); i++ {
		if er.Connections[i-1].Degree < er.Connections[i].Degree {
			t.Errorf("connections not ordered by degree: %+v", er.Connections)
		}
	}
}
