package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFile is a tiny helper that fails the test if the file can't be read.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// ---------- export kiro ----------

func TestExportKiro(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, errOut := run(t, "spec", "init", "photo-albums", "--root", dir); code != 0 {
		t.Fatalf("spec init: %s", errOut)
	}
	if code, _, errOut := run(t, "spec", "generate", "photo-albums", "--artifact", "requirements", "--root", dir); code != 0 {
		t.Fatalf("spec generate: %s", errOut)
	}

	code, out, errOut := run(t, "export", "kiro", "--root", dir)
	if code != 0 {
		t.Fatalf("export kiro failed: code=%d err=%s", code, errOut)
	}
	if !strings.Contains(out, "exported") {
		t.Errorf("expected summary line, got: %s", out)
	}

	// Steering copies verbatim (frontmatter is already Kiro-compatible).
	src := readFile(t, filepath.Join(dir, ".claude/steering/product.md"))
	dst := readFile(t, filepath.Join(dir, ".kiro/steering/product.md"))
	if src != dst {
		t.Errorf("kiro steering should be a verbatim copy\nsrc:\n%s\ndst:\n%s", src, dst)
	}
	if !strings.Contains(dst, "inclusion: always") {
		t.Errorf("kiro steering lost its frontmatter:\n%s", dst)
	}

	// Specs copy their SDD markdown...
	if _, err := os.Stat(filepath.Join(dir, ".kiro/specs/photo-albums/requirements.md")); err != nil {
		t.Errorf("kiro spec requirements.md missing: %v", err)
	}
	// ...but NOT spec.json (Kiro tracks phase/approval state in-IDE).
	if _, err := os.Stat(filepath.Join(dir, ".kiro/specs/photo-albums/spec.json")); err == nil {
		t.Error("kiro export should not copy spec.json")
	}
}

func TestExportKiroOutAndForce(t *testing.T) {
	dir := freshWorkspace(t)
	outDir := filepath.Join(dir, "build")

	if code, _, errOut := run(t, "export", "kiro", "--root", dir, "--out", outDir); code != 0 {
		t.Fatalf("export --out failed: %s", errOut)
	}
	target := filepath.Join(outDir, ".kiro/steering/product.md")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected export under --out dir: %v", err)
	}

	// Re-export without --force must refuse to clobber.
	if code, _, errOut := run(t, "export", "kiro", "--root", dir, "--out", outDir); code == 0 {
		t.Error("re-export without --force should fail on existing files")
	} else if !strings.Contains(errOut, "refusing to overwrite") {
		t.Errorf("unexpected error: %s", errOut)
	}

	// With --force it overwrites.
	if code, _, errOut := run(t, "export", "kiro", "--root", dir, "--out", outDir, "--force"); code != 0 {
		t.Errorf("export --force should succeed: %s", errOut)
	}
}

// ---------- export codex ----------

func TestExportCodex(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, errOut := run(t, "mcp", "add", "fs",
		"--command", "npx",
		"--arg", "-y", "--arg", "@modelcontextprotocol/server-filesystem", "--arg", ".",
		"--root", dir); code != 0 {
		t.Fatalf("mcp add: %s", errOut)
	}

	code, _, errOut := run(t, "export", "codex", "--root", dir)
	if code != 0 {
		t.Fatalf("export codex failed: code=%d err=%s", code, errOut)
	}

	agents := readFile(t, filepath.Join(dir, "AGENTS.md"))
	// always-on steering is inlined...
	if !strings.Contains(agents, "<!-- steering: product.md (always) -->") {
		t.Errorf("AGENTS.md should inline always-on steering:\n%s", agents)
	}
	// ...the managed @-import block and its markers are gone (Codex can't resolve them)...
	if strings.Contains(agents, steeringMarkerStart) || strings.Contains(agents, "@.claude/steering") {
		t.Errorf("AGENTS.md must not keep Claude @-imports/markers:\n%s", agents)
	}
	// ...and conditional steering is listed as an on-demand pointer.
	if !strings.Contains(agents, "Conditional steering") ||
		!strings.Contains(agents, ".claude/steering/api-conventions.md") {
		t.Errorf("AGENTS.md should list conditional steering:\n%s", agents)
	}

	// MCP becomes Codex TOML.
	toml := readFile(t, filepath.Join(dir, ".codex/config.toml"))
	if !strings.Contains(toml, "[mcp_servers.fs]") || !strings.Contains(toml, `command = "npx"`) {
		t.Errorf("config.toml missing mcp server table:\n%s", toml)
	}
	if !strings.Contains(toml, `args = ["-y", "@modelcontextprotocol/server-filesystem", "."]`) {
		t.Errorf("config.toml missing args array:\n%s", toml)
	}
}

func TestExportCodexNoMCP(t *testing.T) {
	dir := freshWorkspace(t)
	// init registers the csdd server by default; remove it to exercise the no-server path.
	_, _, _ = run(t, "mcp", "remove", csddMCPServerName, "--force", "--root", dir)
	code, out, errOut := run(t, "export", "codex", "--root", dir)
	if code != 0 {
		t.Fatalf("export codex failed: %s", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Error("AGENTS.md should be written even with no MCP servers")
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex/config.toml")); err == nil {
		t.Error("config.toml should be skipped when there are no MCP servers")
	}
	if !strings.Contains(out, "no MCP servers") {
		t.Errorf("expected skip notice, got: %s", out)
	}
}

// ---------- dispatch / errors ----------

func TestExportUnknownTarget(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "export", "vscode", "--root", dir)
	if code == 0 || !strings.Contains(errOut, "unknown export target") {
		t.Errorf("unknown target should fail: code=%d err=%q", code, errOut)
	}
}

func TestExportMissingTarget(t *testing.T) {
	code, _, errOut := run(t, "export")
	if code == 0 || !strings.Contains(errOut, "missing action") {
		t.Errorf("missing target should fail: code=%d err=%q", code, errOut)
	}
}

// ---------- pure helpers ----------

func TestMCPToTOML(t *testing.T) {
	cfg := MCPConfig{MCPServers: map[string]MCPServer{
		"fs":     {Command: "npx", Args: []string{"-y", "srv"}, Env: map[string]string{"TOKEN": "x"}},
		"remote": {URL: "https://example.com/mcp", Type: "http"},
		"off":    {Command: "foo", Disabled: true},
	}}
	out := mcpToTOML(cfg)
	for _, want := range []string{
		"[mcp_servers.fs]",
		`command = "npx"`,
		`args = ["-y", "srv"]`,
		"[mcp_servers.fs.env]",
		`TOKEN = "x"`,
		`url = "https://example.com/mcp"`, // remote server emitted
		"# [mcp_servers.off]",             // disabled server commented out
		"experimental_use_rmcp_client",    // note on remote transport
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mcpToTOML missing %q\n%s", want, out)
		}
	}
}

func TestTOMLKeyAndString(t *testing.T) {
	if got := tomlKey("api-conventions"); got != "api-conventions" {
		t.Errorf("bare key mangled: %q", got)
	}
	if got := tomlKey("has.dot"); got != `"has.dot"` {
		t.Errorf("dotted key should be quoted: %q", got)
	}
	if got := tomlString(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("string escaping wrong: %q", got)
	}
}
