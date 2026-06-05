package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newMux(root, newHub()))
	t.Cleanup(srv.Close)
	return srv
}

func sampleWorkspace(t *testing.T) string {
	return tempWorkspace(t, map[string]string{
		"specs/photo-albums/spec.json": `{"feature_name":"photo-albums","phase":"tasks-generated","approvals":{"tasks":{"generated":true}}}`,
		"specs/photo-albums/tasks.md":  "## Phase 1: Foundation\n\n- [x] 1. done\n  - _Requirements: 1.1_\n",
		".claude/steering/product.md":  "# Product\n",
		".mcp.json":                    `{"mcpServers":{"linear":{}}}`,
		"CLAUDE.md":                    "# Claude\n",
	})
}

func getJSON(t *testing.T, url string, v any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
	return resp.StatusCode
}

func TestHealth(t *testing.T) {
	srv := testServer(t, sampleWorkspace(t))
	var body struct {
		OK      bool `json:"ok"`
		Version int  `json:"version"`
	}
	if code := getJSON(t, srv.URL+"/api/health", &body); code != 200 || !body.OK {
		t.Fatalf("health: code=%d ok=%v", code, body.OK)
	}
}

func TestOverviewEndpoint(t *testing.T) {
	srv := testServer(t, sampleWorkspace(t))
	var ov struct {
		Specs []struct {
			Feature string `json:"feature"`
		} `json:"specs"`
		Steering []struct{} `json:"steering"`
		Version  int        `json:"version"`
	}
	if code := getJSON(t, srv.URL+"/api/overview", &ov); code != 200 {
		t.Fatalf("overview code = %d", code)
	}
	if len(ov.Specs) != 1 || ov.Specs[0].Feature != "photo-albums" {
		t.Errorf("specs = %+v", ov.Specs)
	}
	if len(ov.Steering) != 1 {
		t.Errorf("steering = %d, want 1", len(ov.Steering))
	}
}

func TestSpecEndpoint(t *testing.T) {
	srv := testServer(t, sampleWorkspace(t))
	var d struct {
		Feature string `json:"feature"`
		Phases  []struct {
			Name string `json:"name"`
		} `json:"phases"`
	}
	if code := getJSON(t, srv.URL+"/api/spec/photo-albums", &d); code != 200 {
		t.Fatalf("spec code = %d", code)
	}
	if d.Feature != "photo-albums" || len(d.Phases) != 1 {
		t.Errorf("spec detail = %+v", d)
	}
	if code := getJSON(t, srv.URL+"/api/spec/ghost", nil); code != http.StatusNotFound {
		t.Errorf("unknown spec code = %d, want 404", code)
	}
}

func TestTreeEndpoint(t *testing.T) {
	srv := testServer(t, sampleWorkspace(t))
	var wt struct {
		Csdd []struct {
			Name string `json:"name"`
		} `json:"csdd"`
		Project []struct {
			Name string `json:"name"`
		} `json:"project"`
	}
	if code := getJSON(t, srv.URL+"/api/tree", &wt); code != 200 {
		t.Fatalf("tree code = %d", code)
	}
	csdd := map[string]bool{}
	for _, n := range wt.Csdd {
		csdd[n.Name] = true
	}
	if !csdd["specs"] || !csdd[".claude"] {
		t.Errorf("csdd group = %v", csdd)
	}
}

func TestFileEndpoint(t *testing.T) {
	srv := testServer(t, sampleWorkspace(t))
	var fc struct {
		Lang string `json:"lang"`
		Text string `json:"text"`
	}
	if code := getJSON(t, srv.URL+"/api/file?path=specs/photo-albums/tasks.md", &fc); code != 200 {
		t.Fatalf("file code = %d", code)
	}
	if fc.Lang != "markdown" || !strings.Contains(fc.Text, "Phase 1") {
		t.Errorf("file content = %+v", fc)
	}

	for _, bad := range []string{"../../etc/passwd", "go.mod", ""} {
		if code := getJSON(t, srv.URL+"/api/file?path="+bad, nil); code != http.StatusNotFound {
			t.Errorf("file path %q code = %d, want 404", bad, code)
		}
	}
}

func TestIndexServed(t *testing.T) {
	srv := testServer(t, sampleWorkspace(t))
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("index code = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("index content-type = %q", ct)
	}
}
