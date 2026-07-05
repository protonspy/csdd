package web

import (
	"encoding/json"
	"io"
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
	srv := httptest.NewServer(newMux(root, newHub(), newAuth(false, "")))
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

// TestFileBlockedPaths proves the web layer never serves the pinggy token file
// or anything under .git, even though they exist in the workspace and auth is
// off. Both should 404, and the token's contents must not appear in the body.
func TestFileBlockedPaths(t *testing.T) {
	root := tempWorkspace(t, map[string]string{
		".pinggy-token": "super-secret-token",
		".git/config":   "[core]\n\trepositoryformatversion = 0\n",
		"CLAUDE.md":     "# Claude\n",
	})
	srv := testServer(t, root)

	for _, bad := range []string{".pinggy-token", "./.pinggy-token", ".git/config", ".git", ".git/refs/heads/main"} {
		resp, err := http.Get(srv.URL + "/api/file?path=" + bad)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("blocked path %q → %d, want 404", bad, resp.StatusCode)
		}
		if strings.Contains(string(body), "super-secret-token") {
			t.Errorf("blocked path %q leaked the token in the body: %s", bad, body)
		}
	}

	// A normal file is still served (guard is not over-broad).
	if code := getJSON(t, srv.URL+"/api/file?path=CLAUDE.md", nil); code != http.StatusOK {
		t.Errorf("CLAUDE.md → %d, want 200", code)
	}
}

// TestFileNotFoundHidesPath confirms fix 7: a missing file's 404 body carries a
// generic message, not the absolute filesystem path from the underlying error.
func TestFileNotFoundHidesPath(t *testing.T) {
	root := sampleWorkspace(t)
	srv := testServer(t, root)
	resp, err := http.Get(srv.URL + "/api/file?path=does/not/exist.md")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file → %d, want 404", resp.StatusCode)
	}
	if strings.Contains(string(body), root) || strings.Contains(string(body), "exist.md") {
		t.Errorf("404 body leaked path details: %s", body)
	}
}

// TestHostHeaderValidation covers the DNS-rebinding guard: loopback names and an
// explicitly-allowed host pass; any other Host is rejected with 403 on every
// route (here the public /api/health), independent of auth.
func TestHostHeaderValidation(t *testing.T) {
	root := sampleWorkspace(t)
	handler := newMux(root, newHub(), newAuth(false, ""), "dash.example.com")

	cases := []struct {
		host string
		want int
	}{
		{"localhost:7777", http.StatusOK},
		{"127.0.0.1:7777", http.StatusOK},
		{"[::1]:7777", http.StatusOK},
		{"dash.example.com", http.StatusOK},        // explicitly allowed host
		{"DASH.EXAMPLE.COM:443", http.StatusOK},    // case-insensitive
		{"evil.example.com", http.StatusForbidden}, // rebinding attacker's domain
		{"192.168.0.5:7777", http.StatusForbidden}, // some LAN IP, not allowed
		{"", http.StatusForbidden},                 // missing Host
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("Host %q → %d, want %d", c.host, rec.Code, c.want)
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
