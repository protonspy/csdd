package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNodeLinkRoundTrip(t *testing.T) {
	g := New()
	g.Meta["schema_version"] = float64(1)
	g.Nodes = []Node{
		{
			ID: "spec_foo", Label: "foo", FileType: TypeSpec,
			SourceFile: "specs/foo/spec.json", SourceLocation: "L1",
			Attrs: map[string]any{"phase": "tasks", "development_flow": "tdd"},
		},
		{
			ID: "task_foo_1_1", Label: "Do a thing", FileType: TypeTask,
			SourceFile: "specs/foo/tasks.md", SourceLocation: "L10",
			Attrs: map[string]any{"status": "pending", "parallel": true},
		},
	}
	g.Links = []Edge{
		{Source: "spec_foo", Target: "task_foo_1_1", Relation: RelOwns, Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: "specs/foo/spec.json"},
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// The wire format must be NetworkX node-link (R8.1): the five envelope keys.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	for _, k := range []string{"directed", "multigraph", "graph", "nodes", "links"} {
		if _, ok := envelope[k]; !ok {
			t.Fatalf("node-link JSON missing key %q; got %s", k, data)
		}
	}

	// Attrs must be flattened into the node object, not nested.
	if !strings.Contains(string(data), `"development_flow":"tdd"`) {
		t.Fatalf("expected flattened attr development_flow in %s", data)
	}
	if !strings.Contains(string(data), `"parallel":true`) {
		t.Fatalf("expected flattened attr parallel in %s", data)
	}

	var back Graph
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Nodes) != 2 || len(back.Links) != 1 {
		t.Fatalf("round-trip lost data: %s", back.String())
	}
	if back.Nodes[0].FileType != TypeSpec || back.Nodes[0].Attrs["phase"] != "tasks" {
		t.Fatalf("node 0 corrupted: %+v", back.Nodes[0])
	}
	if back.Nodes[1].Attrs["parallel"] != true {
		t.Fatalf("bool attr lost: %+v", back.Nodes[1].Attrs)
	}
	e := back.Links[0]
	if e.Source != "spec_foo" || e.Relation != RelOwns || e.ConfidenceScore != 1.0 {
		t.Fatalf("edge corrupted: %+v", e)
	}
}

func TestMarshalDeterministic(t *testing.T) {
	// Same graph, marshaled twice, must be byte-identical despite the randomized
	// Go map iteration over Attrs (§5.9 git-diff stability).
	mk := func() *Graph {
		g := New()
		g.Nodes = []Node{{
			ID: "n", Label: "n", FileType: TypeTask,
			Attrs: map[string]any{"z": 1, "a": 2, "m": 3, "status": "done"},
		}}
		return g
	}
	var first string
	for i := 0; i < 20; i++ {
		data, err := json.Marshal(mk())
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = string(data)
			continue
		}
		if string(data) != first {
			t.Fatalf("non-deterministic marshal:\n%s\nvs\n%s", first, data)
		}
	}
	// Attr keys must appear sorted.
	if !strings.Contains(first, `"a":2,"m":3,"status":"done","z":1`) {
		t.Fatalf("attrs not sorted: %s", first)
	}
}
