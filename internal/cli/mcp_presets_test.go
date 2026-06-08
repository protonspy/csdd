package cli

import (
	"strings"
	"testing"
)

// ---- registry accessor ----

func TestMCPPresetsAccessor(t *testing.T) {
	got := MCPPresets()
	if len(got) != 3 {
		t.Fatalf("expected 3 presets, got %d: %+v", len(got), got)
	}
	// Name-sorted: context7 < github < playwright.
	wantOrder := []string{"context7", "github", "playwright"}
	for i, p := range got {
		if p.Name != wantOrder[i] {
			t.Errorf("preset[%d] = %q, want %q (must be name-sorted)", i, p.Name, wantOrder[i])
		}
		if p.Summary == "" || p.Transport == "" {
			t.Errorf("preset %q missing summary/transport: %+v", p.Name, p)
		}
	}
}

// ---- install: per-preset transport correctness ----

func TestMCPInstallContext7(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, errOut := run(t, "mcp", "install", "context7", "--root", dir); code != 0 {
		t.Fatalf("install context7 should succeed: %s", errOut)
	}
	srv := loadServer(t, dir, "context7")
	if srv.URL != "https://mcp.context7.com/mcp" || srv.Type != "http" {
		t.Errorf("context7 remote config wrong: %+v", srv)
	}
	if srv.Command != "" {
		t.Errorf("remote preset must not carry a command: %+v", srv)
	}
}

func TestMCPInstallPlaywright(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, errOut := run(t, "mcp", "install", "playwright", "--root", dir); code != 0 {
		t.Fatalf("install playwright should succeed: %s", errOut)
	}
	srv := loadServer(t, dir, "playwright")
	if srv.Command != "npx" || len(srv.Args) != 1 || srv.Args[0] != "@playwright/mcp@latest" {
		t.Errorf("playwright stdio config wrong: %+v", srv)
	}
	if srv.URL != "" || srv.Type != "" {
		t.Errorf("stdio preset must not carry url/type: %+v", srv)
	}
}

func TestMCPInstallGithub(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, errOut := run(t, "mcp", "install", "github", "--root", dir); code != 0 {
		t.Fatalf("install github should succeed: %s", errOut)
	}
	srv := loadServer(t, dir, "github")
	if srv.URL != "https://api.githubcopilot.com/mcp/" || srv.Type != "http" {
		t.Errorf("github remote config wrong: %+v", srv)
	}
	if srv.Command != "" || len(srv.Env) != 0 {
		t.Errorf("github preset must store no command and no secret env: %+v", srv)
	}
}

func TestMCPInstallMultiple(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, errOut := run(t, "mcp", "install", "context7", "playwright", "github", "--root", dir); code != 0 {
		t.Fatalf("multi-install should succeed: %s", errOut)
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	for _, name := range []string{"context7", "playwright", "github"} {
		if _, ok := cfg.MCPServers[name]; !ok {
			t.Errorf("expected %q registered after multi-install", name)
		}
	}
}

// ---- install: error paths ----

func TestMCPInstallUnknown(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "mcp", "install", "nope", "--root", dir)
	if code != 1 {
		t.Fatalf("unknown preset should exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "unknown preset") {
		t.Errorf("error should mention 'unknown preset': %q", errOut)
	}
	for _, name := range []string{"context7", "playwright", "github"} {
		if !strings.Contains(errOut, name) {
			t.Errorf("error should list valid preset %q: %q", name, errOut)
		}
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	if _, ok := cfg.MCPServers["nope"]; ok {
		t.Error("unknown preset must not write any entry")
	}
}

func TestMCPInstallUnknownInMultiWritesNothing(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "mcp", "install", "context7", "nope", "--root", dir); code != 1 {
		t.Fatalf("a single unknown name should reject the whole call (exit 1)")
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	if _, ok := cfg.MCPServers["context7"]; ok {
		t.Error("no entry should be written when any name is unknown")
	}
}

func TestMCPInstallDuplicateInMultiIsNotAtomic(t *testing.T) {
	// Documented contract: name validation is up front, but installs go through
	// MCPAdd per preset — so a *duplicate* later in the list does not roll back
	// earlier successful adds (mirrors running two `mcp add` calls).
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "mcp", "install", "github", "--root", dir); code != 0 {
		t.Fatal("seed install of github should succeed")
	}
	// context7 is new, github already exists -> context7 lands, github errors.
	if code, _, _ := run(t, "mcp", "install", "context7", "github", "--root", dir); code != 1 {
		t.Fatalf("a duplicate later in the list should exit 1")
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	if _, ok := cfg.MCPServers["context7"]; !ok {
		t.Error("the earlier (new) preset should have been written before the duplicate failed")
	}
}

func TestMCPInstallMissingName(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "mcp", "install", "--root", dir); code != 1 {
		t.Error("install without a preset name should exit 1")
	}
}

func TestMCPInstallDuplicateNeedsForce(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "mcp", "install", "context7", "--root", dir); code != 0 {
		t.Fatal("first install should succeed")
	}
	if code, _, _ := run(t, "mcp", "install", "context7", "--root", dir); code == 0 {
		t.Error("duplicate install without --force should fail")
	}
	if code, _, _ := run(t, "mcp", "install", "context7", "--force", "--root", dir); code != 0 {
		t.Error("duplicate install with --force should replace")
	}
	srv := loadServer(t, dir, "context7")
	if srv.URL == "" || srv.Type != "http" {
		t.Errorf("forced reinstall should keep the context7 remote config: %+v", srv)
	}
}

func TestMCPInstallForceReplacesManualEntry(t *testing.T) {
	dir := freshWorkspace(t)
	// Pre-add a *different* (stdio) server under the same name.
	if code, _, _ := run(t, "mcp", "add", "context7", "--command", "foo", "--root", dir); code != 0 {
		t.Fatal("manual add should succeed")
	}
	if code, _, _ := run(t, "mcp", "install", "context7", "--force", "--root", dir); code != 0 {
		t.Fatal("forced install should replace the manual entry")
	}
	srv := loadServer(t, dir, "context7")
	if srv.URL != "https://mcp.context7.com/mcp" || srv.Type != "http" || srv.Command != "" {
		t.Errorf("force install should fully overwrite with the preset config: %+v", srv)
	}
}

// ---- install: note emission ----

func TestMCPInstallGithubEmitsAuthNote(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, _ := run(t, "mcp", "install", "github", "--root", dir)
	if code != 0 {
		t.Fatal("install github should succeed")
	}
	if !strings.Contains(strings.ToLower(out), "auth") {
		t.Errorf("github install should surface an auth note:\n%s", out)
	}
}

// ---- presets listing ----

func TestMCPPresetsLists(t *testing.T) {
	code, out, _ := run(t, "mcp", "presets")
	if code != 0 {
		t.Fatalf("mcp presets should exit 0, got %d", code)
	}
	for _, want := range []string{"name", "type", "summary", "context7", "playwright", "github", "http", "stdio"} {
		if !strings.Contains(out, want) {
			t.Errorf("presets listing missing %q:\n%s", want, out)
		}
	}
}

// loadServer reads .mcp.json and returns the named server, failing if absent.
func loadServer(t *testing.T, dir, name string) MCPServer {
	t.Helper()
	cfg, err := loadMCP(mcpJSONPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	srv, ok := cfg.MCPServers[name]
	if !ok {
		t.Fatalf("server %q not found in %+v", name, cfg.MCPServers)
	}
	return srv
}
