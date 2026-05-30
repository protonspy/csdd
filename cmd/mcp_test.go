package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mcpJSONPath(dir string) string {
	return filepath.Join(dir, ".mcp.json")
}

func TestMCPAddStdioAndRemote(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "mcp", "add", "filesystem",
		"--command", "npx", "--arg", "-y", "--arg", "@scope/server", "--arg", ".",
		"--env", "TOKEN=abc", "--root", dir); code != 0 {
		t.Fatal("stdio add should succeed")
	}
	if code, _, _ := run(t, "mcp", "add", "linear",
		"--url", "https://mcp.linear.app/sse", "--type", "sse", "--root", dir); code != 0 {
		t.Fatal("remote add should succeed")
	}
	cfg, err := loadMCP(mcpJSONPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	fsrv, ok := cfg.MCPServers["filesystem"]
	if !ok || fsrv.Command != "npx" || len(fsrv.Args) != 3 || fsrv.Env["TOKEN"] != "abc" {
		t.Errorf("stdio server not stored correctly: %+v", fsrv)
	}
	lin, ok := cfg.MCPServers["linear"]
	if !ok || lin.URL == "" || lin.Type != "sse" {
		t.Errorf("remote server not stored correctly: %+v", lin)
	}
	// stdio server must not carry a type, remote must not carry a command.
	if fsrv.Type != "" || lin.Command != "" {
		t.Errorf("transport fields leaked across server kinds: %+v / %+v", fsrv, lin)
	}
}

func TestMCPAddDefaultsRemoteTypeToHTTP(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "mcp", "add", "remote", "--url", "https://x/y", "--root", dir); code != 0 {
		t.Fatal("remote add without --type should default to http")
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	if cfg.MCPServers["remote"].Type != "http" {
		t.Errorf("expected default type http, got %q", cfg.MCPServers["remote"].Type)
	}
}

func TestMCPAddErrors(t *testing.T) {
	dir := freshWorkspace(t)
	cases := [][]string{
		{"mcp", "add", "x", "--command", "c", "--url", "u", "--root", dir}, // both transports
		{"mcp", "add", "x", "--root", dir},                                 // neither transport
		{"mcp", "add", "x", "--url", "u", "--type", "ftp", "--root", dir},  // bad remote type
		{"mcp", "add", "x", "--command", "c", "--env", "NOEQ", "--root", dir},
		{"mcp", "add", "Bad_Name", "--command", "c", "--root", dir}, // non-kebab
	}
	for _, args := range cases {
		t.Run(strings.Join(args[2:4], " "), func(t *testing.T) {
			if code, _, _ := run(t, args...); code == 0 {
				t.Errorf("%v should fail", args)
			}
		})
	}
}

func TestMCPAddDuplicateNeedsForce(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "mcp", "add", "dup", "--command", "a", "--root", dir)
	if code, _, _ := run(t, "mcp", "add", "dup", "--command", "b", "--root", dir); code == 0 {
		t.Error("duplicate add without --force should fail")
	}
	if code, _, _ := run(t, "mcp", "add", "dup", "--command", "b", "--force", "--root", dir); code != 0 {
		t.Error("duplicate add with --force should replace")
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	if cfg.MCPServers["dup"].Command != "b" {
		t.Errorf("force should have replaced the entry, got %q", cfg.MCPServers["dup"].Command)
	}
}

func TestMCPListAndShow(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "mcp", "add", "filesystem", "--command", "npx", "--arg", "-y", "--root", dir)
	code, out, _ := run(t, "mcp", "list", "--root", dir)
	if code != 0 || !strings.Contains(out, "filesystem") || !strings.Contains(out, "stdio") {
		t.Errorf("list output missing data:\n%s", out)
	}
	code, out, _ = run(t, "mcp", "show", "filesystem", "--root", dir)
	if code != 0 || !strings.Contains(out, "\"command\": \"npx\"") {
		t.Errorf("show output missing command:\n%s", out)
	}
	if code, _, _ := run(t, "mcp", "show", "ghost", "--root", dir); code == 0 {
		t.Error("show of unknown server should fail")
	}
}

func TestMCPListEmpty(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, _ := run(t, "mcp", "list", "--root", dir)
	if code != 0 || !strings.Contains(out, "no mcp servers") {
		t.Errorf("empty list should report none:\n%s", out)
	}
}

func TestMCPEnableDisable(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "mcp", "add", "srv", "--command", "c", "--root", dir)
	if code, _, _ := run(t, "mcp", "disable", "srv", "--root", dir); code != 0 {
		t.Fatal("disable failed")
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	if !cfg.MCPServers["srv"].Disabled {
		t.Error("server should be disabled")
	}
	if code, _, _ := run(t, "mcp", "enable", "srv", "--root", dir); code != 0 {
		t.Fatal("enable failed")
	}
	cfg, _ = loadMCP(mcpJSONPath(dir))
	if cfg.MCPServers["srv"].Disabled {
		t.Error("server should be enabled")
	}
	if code, _, _ := run(t, "mcp", "disable", "ghost", "--root", dir); code == 0 {
		t.Error("toggling unknown server should fail")
	}
}

func TestMCPRemove(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "mcp", "add", "srv", "--command", "c", "--root", dir)
	if code, _, _ := run(t, "mcp", "remove", "srv", "--root", dir); code == 0 {
		t.Error("remove without --force should fail")
	}
	if code, _, _ := run(t, "mcp", "remove", "srv", "--force", "--root", dir); code != 0 {
		t.Error("remove with --force should succeed")
	}
	cfg, _ := loadMCP(mcpJSONPath(dir))
	if _, ok := cfg.MCPServers["srv"]; ok {
		t.Error("server should be gone after remove")
	}
	if code, _, _ := run(t, "mcp", "remove", "ghost", "--force", "--root", dir); code == 0 {
		t.Error("removing unknown server should fail")
	}
}

func TestMCPValidate(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "mcp", "add", "ok", "--command", "c", "--root", dir)
	if code, _, _ := run(t, "mcp", "validate", "--root", dir); code != 0 {
		t.Error("valid config should pass")
	}
	// Hand-write a structurally invalid config (both transports set).
	bad := `{"mcpServers":{"broken":{"command":"c","url":"http://x","type":"http"}}}`
	if err := os.WriteFile(mcpJSONPath(dir), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "mcp", "validate", "--root", dir); code != 2 {
		t.Error("config with both command and url should fail validation (exit 2)")
	}
}

func TestMCPMalformedJSON(t *testing.T) {
	dir := freshWorkspace(t)
	if err := os.WriteFile(mcpJSONPath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "mcp", "list", "--root", dir); code == 0 {
		t.Error("malformed mcp.json should make list fail")
	}
}

func TestMCPUnknownAction(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "mcp", "frobnicate", "--root", dir); code == 0 {
		t.Error("unknown mcp action should fail")
	}
	if code, _, _ := run(t, "mcp"); code == 0 {
		t.Error("mcp without an action should fail")
	}
}

// ---- direct unit tests for the helpers ----

func TestParseEnvKV(t *testing.T) {
	got, err := parseEnvKV([]string{"A=1", "B=x=y"})
	if err != nil {
		t.Fatal(err)
	}
	if got["A"] != "1" || got["B"] != "x=y" {
		t.Errorf("parseEnvKV: %#v", got)
	}
	if _, err := parseEnvKV([]string{"NOEQ"}); err == nil {
		t.Error("missing '=' should error")
	}
	if _, err := parseEnvKV([]string{"=v"}); err == nil {
		t.Error("empty key should error")
	}
	if got, _ := parseEnvKV(nil); got != nil {
		t.Error("nil input should yield nil map")
	}
}

func TestValidateMCPConfigDirect(t *testing.T) {
	cfg := MCPConfig{MCPServers: map[string]MCPServer{
		"good-stdio":  {Command: "c"},
		"good-remote": {URL: "u", Type: "http"},
		"empty":       {},
		"both":        {Command: "c", URL: "u", Type: "http"},
		"bad-type":    {URL: "u", Type: "ftp"},
		"stdio-typed": {Command: "c", Type: "http"},
	}}
	issues := validateMCPConfig(cfg)
	// Expect exactly the four broken servers to be flagged.
	if len(issues) != 4 {
		t.Fatalf("expected 4 issues, got %d: %v", len(issues), issues)
	}
	joined := ""
	for _, i := range issues {
		joined += i.String() + "\n"
	}
	for _, want := range []string{"empty", "both", "bad-type", "stdio-typed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected an issue mentioning %q:\n%s", want, joined)
		}
	}
}
