package web

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"testing"
)

// gzipString returns the gzip-compressed bytes of s, for seeding a graph.json.gz
// fixture on disk (the CLI is the only real author; the test stands in for it).
func gzipString(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestGraphServedThroughHardenedRoute verifies the web dashboard serves the
// compressed knowledge graph through the dedicated read-only /api/graph route —
// gzip on the wire, transparently decompressed by the client — with the
// Host-header guard applied (R6.3). The Graph tab reads exactly this.
func TestGraphServedThroughHardenedRoute(t *testing.T) {
	graphJSON := `{"directed":true,"multigraph":false,"graph":{},"nodes":[{"id":"spec_x","label":"x","file_type":"spec","source_file":"specs/x/spec.json","source_location":"L1"}],"links":[]}`
	root := tempWorkspace(t, map[string]string{
		"docs/graph/graph.json.gz": gzipString(t, graphJSON),
		"CLAUDE.md":                "# c\n",
	})
	srv := testServer(t, root)

	// Served through /api/graph with a valid (loopback) Host. The HTTP client
	// auto-negotiates gzip and hands back the decompressed node-link JSON.
	var nl struct {
		Nodes []map[string]any `json:"nodes"`
		Links []map[string]any `json:"links"`
	}
	if code := getJSON(t, srv.URL+"/api/graph", &nl); code != http.StatusOK {
		t.Fatalf("graph should be served; got %d", code)
	}
	if len(nl.Nodes) != 1 || nl.Nodes[0]["id"] != "spec_x" {
		t.Fatalf("served payload is not the expected node-link graph: %+v", nl)
	}

	// The response advertises gzip Content-Encoding (zero server-side decompression).
	resp, err := http.Get(srv.URL + "/api/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !resp.Uncompressed {
		t.Errorf("expected a gzip-encoded response the client decompressed")
	}

	// A missing graph yields 404 (the Graph tab shows a "run csdd graph build" hint).
	bare := testServer(t, tempWorkspace(t, map[string]string{"CLAUDE.md": "# c\n"}))
	if code := getJSON(t, bare.URL+"/api/graph", nil); code != http.StatusNotFound {
		t.Errorf("absent graph should 404; got %d", code)
	}

	// The Host-header guard (DNS-rebinding defense) rejects a foreign Host, so the
	// graph is only reachable through the hardened path.
	req, _ := http.NewRequest("GET", srv.URL+"/api/graph", nil)
	req.Host = "evil.example.com"
	fresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer fresp.Body.Close()
	if fresp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host should be forbidden; got %d", fresp.StatusCode)
	}
}
