package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestGraphJSONServedThroughHardenedRoute verifies the web dashboard serves
// docs/graph/graph.json through the same read-only /api/file route as every
// other file — with the Host-header guard applied (R6.3). The Graph tab reads
// exactly this; no new write endpoint is introduced.
func TestGraphJSONServedThroughHardenedRoute(t *testing.T) {
	graphJSON := `{"directed":true,"multigraph":false,"graph":{},"nodes":[{"id":"spec_x","label":"x","file_type":"spec","source_file":"specs/x/spec.json","source_location":"L1"}],"links":[]}`
	root := tempWorkspace(t, map[string]string{
		"docs/graph/graph.json": graphJSON,
		"CLAUDE.md":             "# c\n",
	})
	srv := testServer(t, root)

	// Served through /api/file with a valid (loopback) Host.
	var fc struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}
	if code := getJSON(t, srv.URL+"/api/file?path=docs/graph/graph.json", &fc); code != http.StatusOK {
		t.Fatalf("graph.json should be served; got %d", code)
	}
	if !strings.Contains(fc.Text, "spec_x") {
		t.Errorf("served graph.json missing content: %q", fc.Text)
	}
	// It parses as node-link JSON (what the Graph tab consumes).
	var nl struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(fc.Text), &nl); err != nil || len(nl.Nodes) != 1 {
		t.Fatalf("served payload is not node-link JSON: %v", err)
	}

	// The Host-header guard (DNS-rebinding defense) rejects a foreign Host, so the
	// graph is only reachable through the hardened path.
	req, _ := http.NewRequest("GET", srv.URL+"/api/file?path=docs/graph/graph.json", nil)
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host should be forbidden; got %d", resp.StatusCode)
	}
}
