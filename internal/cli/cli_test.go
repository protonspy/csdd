package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/protonspy/csdd/internal/templater"
)

// capture swaps stdout/stderr for pipes during f() and returns whatever was
// printed. It is the only safe way to assert against the CLI surface since
// the render package writes directly to os.Stdout/Stderr.
func capture(t *testing.T, f func() int) (int, string, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr
	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, rOut) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, rErr) }()
	code := f()
	_ = wOut.Close()
	_ = wErr.Close()
	wg.Wait()
	os.Stdout, os.Stderr = oldOut, oldErr
	return code, outBuf.String(), errBuf.String()
}

// run invokes the CLI dispatcher with args, returning exit code + captured output.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return capture(t, func() int { return Run(args, templater.FS) })
}

// freshWorkspace creates an empty directory and runs `csdd init --root` so the
// rest of the test has a normal Claude Code layout to operate on.
func freshWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// A complete workspace for tests exercises every tree, so opt into the
	// hooks and pre-push components that a bare `csdd init` now leaves out.
	if code, _, errOut := run(t, "init", "--root", dir, "--with-baseline", "--hooks", "--prepush"); code != 0 {
		t.Fatalf("init failed (code=%d): %s", code, errOut)
	}
	return dir
}

// ---------- top-level dispatch ----------

func TestHelpAndVersion(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "help"} {
		code, out, _ := run(t, arg)
		if code != 0 || !strings.Contains(out, "csdd") {
			t.Errorf("%s should print help with exit 0, got code=%d out=%q", arg, code, out)
		}
	}
	for _, arg := range []string{"--version", "-v", "version"} {
		code, out, _ := run(t, arg)
		if code != 0 || !strings.Contains(out, "csdd") {
			t.Errorf("%s should print version, got code=%d out=%q", arg, code, out)
		}
	}
}

// TestProgName covers the dynamic program name: help and usage strings echo the
// bare binary name by default, but switch to whatever CSDD_PROG holds (the npm
// launcher sets it to "npx @protonspy/csdd" for npx invocations).
func TestProgName(t *testing.T) {
	t.Run("default is bare csdd", func(t *testing.T) {
		t.Setenv("CSDD_PROG", "")
		_, help, _ := run(t, "--help")
		if !strings.Contains(help, "csdd spec generate") {
			t.Errorf("help should show bare csdd commands, got %q", help)
		}
		if strings.Contains(help, "npx @protonspy/csdd") {
			t.Errorf("help should not show the npx prefix by default, got %q", help)
		}
		if _, _, errOut := run(t, "spec", "init"); !strings.Contains(errOut, "usage: csdd spec init") {
			t.Errorf("usage should show bare csdd, got %q", errOut)
		}
	})

	t.Run("CSDD_PROG is echoed verbatim", func(t *testing.T) {
		t.Setenv("CSDD_PROG", "npx @protonspy/csdd")
		_, help, _ := run(t, "--help")
		if !strings.Contains(help, "npx @protonspy/csdd spec generate") {
			t.Errorf("help should echo CSDD_PROG, got %q", help)
		}
		if _, _, errOut := run(t, "spec", "init"); !strings.Contains(errOut, "usage: npx @protonspy/csdd spec init") {
			t.Errorf("usage should echo CSDD_PROG, got %q", errOut)
		}
	})
}

func TestUnknownResource(t *testing.T) {
	code, _, errOut := run(t, "nonsense")
	if code == 0 {
		t.Error("unknown resource should fail")
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("expected unknown-command error, got %q", errOut)
	}
}

func TestEmptyArgsShowsHelp(t *testing.T) {
	code, out, _ := run(t)
	if code != 0 || !strings.Contains(out, "csdd") {
		t.Errorf("empty args should print help, got code=%d out=%q", code, out)
	}
}

func TestTuiSentinelFailsInCmd(t *testing.T) {
	// cli.Run isn't supposed to handle "tui" — main.go must intercept it.
	code, _, errOut := run(t, "tui")
	if code == 0 || !strings.Contains(errOut, "internal") {
		t.Errorf("tui should fail under cli.Run, got code=%d err=%q", code, errOut)
	}
}

// ---------- init ----------

func TestInitCreatesLayout(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := run(t, "init", "--root", dir, "--with-baseline")
	if code != 0 {
		t.Fatalf("init failed: %d", code)
	}
	if !strings.Contains(out, "Initialized Claude Code workspace") {
		t.Errorf("missing initialization message: %s", out)
	}
	for _, p := range []string{
		".claude/steering/product.md",
		".claude/steering/tech.md",
		".claude/steering/structure.md",
		".claude/steering/security.md",
		".claude/steering/testing.md",
		".claude/steering/api-conventions.md",
		".mcp.json",
		".claude/rules/ears-format.md",
		".claude/templates/specs/requirements.md",
		".claude/templates/specs/design.md",
		".claude/templates/specs/tasks.md",
		".claude/templates/specs/init.json",
		".claude/templates/steering/product.md",
		".claude/templates/steering-custom/custom.md",
		".claude/skills",
		".claude/agents",
		"CLAUDE.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("init missing %s: %v", p, err)
		}
	}
	// csdd.md and the standalone guide are no longer scaffolded — CLAUDE.md is the
	// single, self-contained entry point.
	for _, p := range []string{"csdd.md", "docs/guides/claude-code-sdd.md"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("init should no longer create %s", p)
		}
	}
	// AGENTS.md is intentionally no longer scaffolded — CLAUDE.md is the sole entry point.
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Error("init should not create AGENTS.md anymore")
	}
}

// TestInitExcludeAgentsSkills verifies --exclude skips exactly the named
// component trees while still scaffolding everything else.
func TestInitExcludeAgentsSkills(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := run(t, "init", "--root", dir, "--exclude", "agents,skills")
	if code != 0 {
		t.Fatalf("init --exclude failed (code=%d): %s", code, errOut)
	}
	if !strings.Contains(out, "excluded: agents, skills") {
		t.Errorf("expected an excluded summary, got:\n%s", out)
	}
	// Excluded: only the two named trees.
	for _, p := range []string{".claude/agents", ".claude/skills"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("--exclude should have skipped %s", p)
		}
	}
	// Still present: the rest of the full workspace. (hooks and pre-push are
	// opt-in, so they are absent here — see TestInitDefaultOmitsOptInComponents.)
	for _, p := range []string{
		"CLAUDE.md",
		".mcp.json",
		".claude/settings.json",
		".claude/rules/ears-format.md",
		".claude/templates/specs/requirements.md",
		".claude/commands",
		"specs",
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("--exclude agents,skills should still create %s: %v", p, err)
		}
	}
}

// TestInitExcludeRepeatableFlag confirms --exclude accepts repetition as well as
// comma lists, and that both forms compose.
func TestInitExcludeRepeatableFlag(t *testing.T) {
	dir := t.TempDir()
	if code, _, errOut := run(t, "init", "--root", dir, "--exclude", "agents", "--exclude", "rules,commands"); code != 0 {
		t.Fatalf("init failed (code=%d): %s", code, errOut)
	}
	for _, p := range []string{".claude/agents", ".claude/rules", ".claude/commands"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("repeated/comma --exclude should have skipped %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/skills")); err != nil {
		t.Errorf("skills was not excluded, it should exist: %v", err)
	}
}

// TestInitExcludeUnknownComponent rejects a typo instead of silently ignoring it.
func TestInitExcludeUnknownComponent(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := run(t, "init", "--root", dir, "--exclude", "agent")
	if code != 1 {
		t.Fatalf("unknown --exclude value should exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "unknown --exclude component") {
		t.Errorf("expected an unknown-component error, got:\n%s", errOut)
	}
	// Nothing should have been scaffolded on the failed parse.
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
		t.Error("init should not scaffold when --exclude parsing fails")
	}
}

func TestInitIdempotent(t *testing.T) {
	dir := t.TempDir()
	_, _, _ = run(t, "init", "--root", dir, "--with-baseline")
	code, out, _ := run(t, "init", "--root", dir, "--with-baseline")
	if code != 0 {
		t.Fatalf("second init failed: %d", code)
	}
	if !strings.Contains(out, "directories created: 0") || !strings.Contains(out, "files created: 0") {
		t.Errorf("second init should be a no-op: %s", out)
	}
}

func TestInitWithoutBaseline(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := run(t, "init", "--root", dir)
	if code != 0 {
		t.Fatalf("init failed: %d", code)
	}
	if !strings.Contains(out, "csdd steering init") {
		t.Errorf("non-baseline init should hint at steering init: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/steering/product.md")); err == nil {
		t.Error("init without --with-baseline should not create product.md")
	}
}

func TestInitRegistersMCPServer(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := run(t, "init", "--root", dir)
	if code != 0 {
		t.Fatalf("init failed: %d", code)
	}
	if !strings.Contains(out, "registered the csdd MCP server") {
		t.Errorf("init should report registering the csdd MCP server: %s", out)
	}
	cfg, err := loadMCP(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("load .mcp.json: %v", err)
	}
	srv, ok := cfg.MCPServers[csddMCPServerName]
	if !ok {
		t.Fatalf("csdd MCP server not registered: %+v", cfg.MCPServers)
	}
	if srv.Command != "npx" || strings.Join(srv.Args, " ") != "-y @protonspy/csdd-mcp" {
		t.Errorf("unexpected server entry: command=%q args=%v", srv.Command, srv.Args)
	}
	if srv.URL != "" || srv.Disabled || len(srv.AutoApprove) > 0 {
		t.Errorf("expected a plain enabled stdio server, got %+v", srv)
	}
	// Idempotent: a second init must not re-register (and so not re-announce it).
	_, out2, _ := run(t, "init", "--root", dir)
	if strings.Contains(out2, "registered the csdd MCP server") {
		t.Errorf("second init should not re-register the MCP server: %s", out2)
	}
}

func TestInitNoMCP(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := run(t, "init", "--root", dir, "--no-mcp")
	if code != 0 {
		t.Fatalf("init failed: %d", code)
	}
	if strings.Contains(out, "registered the csdd MCP server") {
		t.Errorf("--no-mcp should not register the server: %s", out)
	}
	cfg, err := loadMCP(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("load .mcp.json: %v", err)
	}
	if _, ok := cfg.MCPServers[csddMCPServerName]; ok {
		t.Errorf("--no-mcp should leave .mcp.json without the csdd server: %+v", cfg.MCPServers)
	}
}

// ---------- steering ----------

func TestSteeringInit(t *testing.T) {
	dir := freshWorkspace(t)
	// Remove baseline to test the steering init command.
	for _, f := range []string{"product.md", "tech.md", "structure.md"} {
		_ = os.Remove(filepath.Join(dir, ".claude/steering", f))
	}
	code, out, _ := run(t, "steering", "init", "--root", dir)
	if code != 0 {
		t.Fatalf("steering init: %d", code)
	}
	for _, name := range []string{"product.md", "tech.md", "structure.md", "security.md", "testing.md", "api-conventions.md"} {
		if _, err := os.Stat(filepath.Join(dir, ".claude/steering", name)); err != nil {
			t.Errorf("missing %s after steering init", name)
		}
	}
	if !strings.Contains(out, "standard steering files created") {
		t.Errorf("expected counter line in steering init output: %s", out)
	}
}

func TestSteeringCreateAllInclusionModes(t *testing.T) {
	dir := freshWorkspace(t)
	cases := []struct {
		name      string
		args      []string
		mustEmit  string
		shouldErr bool
	}{
		{"always", []string{"steering", "create", "sec", "--inclusion", "always", "--root", dir}, "inclusion: always", false},
		{"manual", []string{"steering", "create", "manual-x", "--inclusion", "manual", "--root", dir}, "inclusion: manual", false},
		{"fileMatch", []string{"steering", "create", "api", "--inclusion", "fileMatch",
			"--pattern", "src/api/**/*", "--pattern", "**/*Controller.*", "--root", dir},
			`fileMatchPattern: ["src/api/**/*", "**/*Controller.*"]`, false,
		},
		{"auto", []string{"steering", "create", "obs", "--inclusion", "auto",
			"--description", "Logging trigger", "--root", dir}, "description: Logging trigger", false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, errOut := run(t, c.args...)
			if c.shouldErr != (code != 0) {
				t.Fatalf("%s: code=%d err=%s", c.name, code, errOut)
			}
			content, _ := os.ReadFile(filepath.Join(dir, ".claude/steering", c.args[2]+".md"))
			if !strings.Contains(string(content), c.mustEmit) {
				t.Errorf("%s: file missing %q\n%s", c.name, c.mustEmit, content)
			}
		})
	}
}

func TestSteeringCreateValidationErrors(t *testing.T) {
	dir := freshWorkspace(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing inclusion", []string{"steering", "create", "thing", "--root", dir},
			"--inclusion must be one of"},
		{"fileMatch missing pattern", []string{"steering", "create", "f", "--inclusion", "fileMatch", "--root", dir},
			"requires at least one --pattern"},
		{"auto missing description", []string{"steering", "create", "a", "--inclusion", "auto", "--root", dir},
			"requires --description"},
		{"kebab violation", []string{"steering", "create", "BadName", "--inclusion", "always", "--root", dir},
			"kebab-case"},
		{"missing name", []string{"steering", "create", "--inclusion", "always", "--root", dir},
			"usage: csdd steering create"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, errOut := run(t, c.args...)
			if code == 0 {
				t.Errorf("%s: expected non-zero exit", c.name)
			}
			if !strings.Contains(errOut, c.want) {
				t.Errorf("%s: expected %q in stderr, got %q", c.name, c.want, errOut)
			}
		})
	}
}

func TestSteeringCreateNoOverwrite(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "steering", "create", "dup", "--inclusion", "always", "--root", dir); code != 0 {
		t.Fatal("first create failed")
	}
	code, _, errOut := run(t, "steering", "create", "dup", "--inclusion", "always", "--root", dir)
	if code == 0 || !strings.Contains(errOut, "refusing to overwrite") {
		t.Errorf("second create should refuse overwrite: code=%d err=%q", code, errOut)
	}
	// With --force it succeeds.
	if code, _, _ := run(t, "steering", "create", "dup", "--inclusion", "always", "--root", dir, "--force"); code != 0 {
		t.Error("--force should permit overwrite")
	}
}

func TestSteeringListAndFilter(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "steering", "create", "obs", "--inclusion", "auto", "--description", "x", "--root", dir)
	_, out, _ := run(t, "steering", "list", "--root", dir)
	if !strings.Contains(out, "product.md") || !strings.Contains(out, "obs.md") {
		t.Errorf("list missing entries: %s", out)
	}
	_, filteredOut, _ := run(t, "steering", "list", "--inclusion", "auto", "--root", dir)
	if !strings.Contains(filteredOut, "obs.md") || strings.Contains(filteredOut, "product.md") {
		t.Errorf("filter --inclusion auto failed: %s", filteredOut)
	}
}

func TestSteeringListEmpty(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".claude/steering"), 0o755)
	_, out, _ := run(t, "steering", "list", "--root", dir)
	if !strings.Contains(out, "no steering files found") {
		t.Errorf("empty dir should report no files: %s", out)
	}
}

func TestSteeringShowAndDelete(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, _ := run(t, "steering", "show", "product", "--root", dir)
	if code != 0 || !strings.Contains(out, "# Product") {
		t.Errorf("show product failed: code=%d out=%q", code, out)
	}
	// show missing returns 1.
	code, _, _ = run(t, "steering", "show", "nope", "--root", dir)
	if code == 0 {
		t.Error("show missing should fail")
	}
	// delete foundational without --force is refused.
	if code, _, _ := run(t, "steering", "delete", "product", "--root", dir); code == 0 {
		t.Error("delete foundational without --force should fail")
	}
	// custom file delete works without force.
	_, _, _ = run(t, "steering", "create", "custom-a", "--inclusion", "always", "--root", dir)
	if code, _, _ := run(t, "steering", "delete", "custom-a", "--root", dir); code != 0 {
		t.Error("delete custom should succeed")
	}
	// foundational with --force works.
	if code, _, _ := run(t, "steering", "delete", "product", "--root", dir, "--force"); code != 0 {
		t.Error("delete with --force should succeed")
	}
	// delete missing should fail.
	if code, _, _ := run(t, "steering", "delete", "ghost", "--root", dir, "--force"); code == 0 {
		t.Error("delete ghost should fail")
	}
}

func TestSteeringValidate(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "steering", "validate", "--root", dir)
	if code != 0 {
		t.Errorf("validate should pass on fresh workspace, got %d", code)
	}
	// Corrupt a file and re-validate.
	bad := filepath.Join(dir, ".claude/steering/bad.md")
	_ = os.WriteFile(bad, []byte("---\ninclusion: weird\n---\n"), 0o644)
	if code, _, _ := run(t, "steering", "validate", "--root", dir); code != 2 {
		t.Errorf("validate with bad file should exit 2, got %d", code)
	}
	// Single-file validate.
	if code, _, _ := run(t, "steering", "validate", "bad", "--root", dir); code != 2 {
		t.Errorf("single-file validate should exit 2")
	}
}

func TestSteeringMissingWorkspace(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := run(t, "steering", "list", "--root", dir)
	if code == 0 || !strings.Contains(errOut, "no .claude/steering") {
		t.Errorf("steering against bare dir should fail: code=%d err=%q", code, errOut)
	}
}

func TestSteeringUnknownAction(t *testing.T) {
	code, _, errOut := run(t, "steering", "nonsense")
	if code == 0 || !strings.Contains(errOut, "unknown steering action") {
		t.Errorf("unknown action should fail: code=%d err=%q", code, errOut)
	}
}

// ---------- spec ----------

func TestSpecInitAndShow(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, _ := run(t, "spec", "init", "photo-albums", "--root", dir)
	if code != 0 {
		t.Fatalf("spec init: %d", code)
	}
	if !strings.Contains(filepath.ToSlash(out), "specs/photo-albums/") {
		t.Errorf("expected path in output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "specs/photo-albums/spec.json")); err != nil {
		t.Error("spec.json missing after init")
	}
	// Second init refuses.
	code, _, _ = run(t, "spec", "init", "photo-albums", "--root", dir)
	if code == 0 {
		t.Error("duplicate spec init should fail")
	}
	// show.
	_, showOut, _ := run(t, "spec", "show", "photo-albums", "--root", dir)
	if !strings.Contains(showOut, "feature: photo-albums") {
		t.Errorf("show output: %s", showOut)
	}
	// show missing.
	if code, _, _ := run(t, "spec", "show", "ghost", "--root", dir); code == 0 {
		t.Error("show ghost should fail")
	}
}

func TestSpecInitInvalidName(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "spec", "init", "BadCase", "--root", dir)
	if code == 0 || !strings.Contains(errOut, "kebab-case") {
		t.Errorf("invalid name should fail: code=%d err=%q", code, errOut)
	}
	// Missing positional.
	if code, _, _ := run(t, "spec", "init", "--root", dir); code == 0 {
		t.Error("spec init without name should fail")
	}
}

func TestSpecGeneratePhaseGate(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "phase-test", "--root", dir)
	// Cannot generate design without requirements approved.
	code, _, errOut := run(t, "spec", "generate", "phase-test", "--artifact", "design", "--root", dir)
	if code == 0 || !strings.Contains(errOut, "phase gate") {
		t.Errorf("design without requirements should hit phase gate: code=%d err=%q", code, errOut)
	}
	// --force bypasses.
	if code, _, _ := run(t, "spec", "generate", "phase-test", "--artifact", "design", "--force", "--root", dir); code != 0 {
		t.Error("--force should bypass phase gate")
	}
	// Generate requirements legitimately.
	if code, _, _ := run(t, "spec", "generate", "phase-test", "--artifact", "requirements", "--force", "--root", dir); code != 0 {
		t.Error("generate requirements failed")
	}
	// Cannot regenerate without --force.
	if code, _, _ := run(t, "spec", "generate", "phase-test", "--artifact", "requirements", "--root", dir); code == 0 {
		t.Error("regenerate without --force should fail")
	}
}

func TestSpecGenerateBadArtifact(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "x", "--root", dir)
	if code, _, _ := run(t, "spec", "generate", "x", "--artifact", "bogus", "--root", dir); code == 0 {
		t.Error("bogus artifact should fail")
	}
	if code, _, _ := run(t, "spec", "generate", "missing", "--artifact", "requirements", "--root", dir); code == 0 {
		t.Error("generate against missing spec should fail")
	}
	if code, _, _ := run(t, "spec", "generate", "x", "--root", dir); code == 0 {
		t.Error("missing --artifact should fail")
	}
}

func TestSpecApproveCascadeAndReady(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "demo", "--root", dir)
	for _, art := range []string{"requirements", "design", "tasks"} {
		if code, _, _ := run(t, "spec", "generate", "demo", "--artifact", art, "--force", "--root", dir); code != 0 {
			t.Fatalf("generate %s failed", art)
		}
		if code, _, _ := run(t, "spec", "approve", "demo", "--phase", art, "--force", "--root", dir); code != 0 {
			t.Fatalf("approve %s failed", art)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "specs/demo/spec.json"))
	if !strings.Contains(string(data), `"ready_for_implementation": true`) {
		t.Errorf("after full approval, ready_for_implementation should be true. Got: %s", data)
	}
}

func TestSpecApproveValidations(t *testing.T) {
	dir := freshWorkspace(t)
	// Approve unknown spec.
	if code, _, _ := run(t, "spec", "approve", "ghost", "--phase", "requirements", "--root", dir); code == 0 {
		t.Error("approve ghost should fail")
	}
	// Missing --phase.
	if code, _, _ := run(t, "spec", "approve", "x", "--root", dir); code == 0 {
		t.Error("approve missing phase should fail")
	}
	// Invalid --phase.
	_, _, _ = run(t, "spec", "init", "x", "--root", dir)
	if code, _, _ := run(t, "spec", "approve", "x", "--phase", "bogus", "--root", dir); code == 0 {
		t.Error("invalid phase should fail")
	}
	// Approve phase not yet generated.
	if code, _, _ := run(t, "spec", "approve", "x", "--phase", "requirements", "--root", dir); code == 0 {
		t.Error("approve before generation should fail")
	}
	// Generate, then approve without --force on a spec with issues (template requirements are valid;
	// to force validation issues we corrupt the file).
	_, _, _ = run(t, "spec", "generate", "x", "--artifact", "requirements", "--force", "--root", dir)
	_ = os.WriteFile(filepath.Join(dir, "specs/x/requirements.md"),
		[]byte("# Requirements\n## Requirements\n### Requirement 1: bad\n1. should be SHALL\n"), 0o644)
	if code, _, _ := run(t, "spec", "approve", "x", "--phase", "requirements", "--root", dir); code == 0 {
		t.Error("approve with validation issues (no force) should fail")
	}
	// With --force it goes through.
	if code, _, _ := run(t, "spec", "approve", "x", "--phase", "requirements", "--force", "--root", dir); code != 0 {
		t.Error("approve with --force should succeed")
	}
	_, _, _ = run(t, "spec", "init", "ordered", "--root", dir)
	_, _, _ = run(t, "spec", "generate", "ordered", "--artifact", "design", "--force", "--root", dir)
	if code, _, errOut := run(t, "spec", "approve", "ordered", "--phase", "design", "--root", dir); code == 0 || !strings.Contains(errOut, "phase gate") {
		t.Errorf("approve design before requirements approval should fail: code=%d err=%q", code, errOut)
	}
}

func TestSpecApproveDirectlyAuthoredArtifact(t *testing.T) {
	// A phase authored straight to disk (a human or a `plan run` session writing
	// requirements.md without `spec generate`) must still be validatable and
	// approvable: approval certifies the content, not the generation route.
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "authored", "--root", dir)
	valid := "# Requirements\n\n### Requirement 1: R\n\n**Acceptance Criteria**\n1. WHEN a user acts THE SYSTEM SHALL respond.\n"
	if err := os.WriteFile(filepath.Join(dir, "specs/authored/requirements.md"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := run(t, "spec", "validate", "authored", "--root", dir); code != 0 {
		t.Errorf("validate should scope to the authored phase, not report 'no generated artifacts': %s", e)
	}
	if code, _, e := run(t, "spec", "approve", "authored", "--phase", "requirements", "--root", dir); code != 0 {
		t.Errorf("approve of a directly-authored artifact should succeed: %s", e)
	}
	parsed, err := loadSpecJSON(filepath.Join(dir, "specs", "authored"))
	if err != nil {
		t.Fatal(err)
	}
	if a := parsed.Approvals["requirements"]; !a.Approved || !a.Generated {
		t.Errorf("approval should mark the phase approved and backfill generated, got %+v", a)
	}
}

func TestSpecListEmpty(t *testing.T) {
	dir := freshWorkspace(t)
	_, out, _ := run(t, "spec", "list", "--root", dir)
	if !strings.Contains(out, "no specs found") {
		t.Errorf("empty list should report none: %s", out)
	}
}

func TestSpecListAndStatus(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "alpha", "--root", dir)
	_, _, _ = run(t, "spec", "init", "beta", "--root", dir)
	_, out, _ := run(t, "spec", "list", "--root", dir)
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("spec list missing entries: %s", out)
	}
	// status combines show + validate
	code, statusOut, statusErr := run(t, "spec", "status", "alpha", "--root", dir)
	if code != 2 || !strings.Contains(statusErr, "no generated artifacts") {
		t.Errorf("status command failed: code=%d out=%q err=%q", code, statusOut, statusErr)
	}
}

func TestSpecValidateMissing(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "spec", "validate", "ghost", "--root", dir); code == 0 {
		t.Error("validate ghost should fail")
	}
	if code, _, _ := run(t, "spec", "validate", "--root", dir); code == 0 {
		t.Error("validate without feature should fail")
	}
}

func TestSpecDelete(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "doomed", "--root", dir)
	if code, _, _ := run(t, "spec", "delete", "doomed", "--root", dir); code == 0 {
		t.Error("delete without --force should fail")
	}
	if code, _, _ := run(t, "spec", "delete", "doomed", "--force", "--root", dir); code != 0 {
		t.Error("delete with --force should succeed")
	}
	if code, _, _ := run(t, "spec", "delete", "ghost", "--force", "--root", dir); code == 0 {
		t.Error("delete ghost should fail")
	}
	if code, _, _ := run(t, "spec", "delete", "--force", "--root", dir); code == 0 {
		t.Error("delete without name should fail")
	}
}

func TestSpecUnknownAction(t *testing.T) {
	if code, _, errOut := run(t, "spec", "nonsense"); code == 0 || !strings.Contains(errOut, "unknown spec action") {
		t.Errorf("unknown spec action should fail: code=%d err=%q", code, errOut)
	}
}

// ---------- skill ----------

func TestSkillCreateAndStructure(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "skill", "create", "demo-skill",
		"--description", "Demo skill activation trigger.",
		"--root", dir)
	if code != 0 {
		t.Fatal("skill create failed")
	}
	skillDir := filepath.Join(dir, ".claude/skills/demo-skill")
	for _, sub := range []string{"SKILL.md", "references", "assets", "scripts"} {
		if _, err := os.Stat(filepath.Join(skillDir, sub)); err != nil {
			t.Errorf("skill missing %s", sub)
		}
	}
}

func TestSkillCreateErrors(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "skill", "create", "x", "--root", dir); code == 0 {
		t.Error("missing --description should fail")
	}
	if code, _, _ := run(t, "skill", "create", "BadName", "--description", "x", "--root", dir); code == 0 {
		t.Error("non-kebab name should fail")
	}
	if code, _, _ := run(t, "skill", "create", "--description", "x", "--root", dir); code == 0 {
		t.Error("missing name should fail")
	}
	_, _, _ = run(t, "skill", "create", "dup", "--description", "x", "--root", dir)
	if code, _, _ := run(t, "skill", "create", "dup", "--description", "x", "--root", dir); code == 0 {
		t.Error("duplicate skill should fail")
	}
}

func TestSkillListShowAddDelete(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "skill", "create", "demo", "--description", "Demo trigger.", "--root", dir)
	// list
	_, out, _ := run(t, "skill", "list", "--root", dir)
	if !strings.Contains(out, "demo") {
		t.Errorf("skill list missing entry: %s", out)
	}
	// show
	code, showOut, _ := run(t, "skill", "show", "demo", "--root", dir)
	if code != 0 || !strings.Contains(showOut, "SKILL.md") {
		t.Errorf("show failed: %s", showOut)
	}
	if code, _, _ := run(t, "skill", "show", "ghost", "--root", dir); code == 0 {
		t.Error("show ghost should fail")
	}
	// add-reference/script/asset
	for _, kind := range []string{"add-reference", "add-script", "add-asset"} {
		if code, _, _ := run(t, "skill", kind, "demo", kind+".md", "--root", dir); code != 0 {
			t.Errorf("%s failed", kind)
		}
		// adding same file twice should fail
		if code, _, _ := run(t, "skill", kind, "demo", kind+".md", "--root", dir); code == 0 {
			t.Errorf("%s duplicate should fail", kind)
		}
	}
	if code, _, _ := run(t, "skill", "add-reference", "ghost", "f", "--root", dir); code == 0 {
		t.Error("add-reference against ghost skill should fail")
	}
	if code, _, _ := run(t, "skill", "add-reference", "demo", "--root", dir); code == 0 {
		t.Error("add-reference without filename should fail")
	}
	if code, _, _ := run(t, "skill", "add-reference", "demo", "../escape.md", "--root", dir); code == 0 {
		t.Error("add-reference with path traversal should fail")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/skills/demo/escape.md")); err == nil {
		t.Error("path traversal wrote outside references/")
	}
	// delete
	if code, _, _ := run(t, "skill", "delete", "demo", "--root", dir); code == 0 {
		t.Error("skill delete without --force should fail")
	}
	if code, _, _ := run(t, "skill", "delete", "demo", "--force", "--root", dir); code != 0 {
		t.Error("skill delete with --force should succeed")
	}
	if code, _, _ := run(t, "skill", "delete", "ghost", "--force", "--root", dir); code == 0 {
		t.Error("delete ghost should fail")
	}
}

func TestSkillValidate(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "skill", "create", "good", "--description", "ok.", "--root", dir)
	// Add a reference and validate — should warn about missing load trigger.
	_, _, _ = run(t, "skill", "add-reference", "good", "lonely.md", "--root", dir)
	code, _, _ := run(t, "skill", "validate", "good", "--root", dir)
	if code != 2 {
		t.Errorf("expected exit 2 for unreferenced ref, got %d", code)
	}
	if code, _, _ := run(t, "skill", "validate", "ghost", "--root", dir); code == 0 {
		t.Error("validate ghost should fail")
	}
	if code, _, _ := run(t, "skill", "validate", "--root", dir); code == 0 {
		t.Error("validate without name should fail")
	}
}

func TestSkillListEmpty(t *testing.T) {
	// A bare directory (not initialized) has no shipped skills, so the empty
	// path is exercised. `csdd init` ships default workflow skills.
	dir := t.TempDir()
	_, out, _ := run(t, "skill", "list", "--root", dir)
	if !strings.Contains(out, "no skills found") {
		t.Errorf("empty skill list: %s", out)
	}
}

func TestSkillListIncludesShippedDefaults(t *testing.T) {
	dir := freshWorkspace(t)
	_, out, _ := run(t, "skill", "list", "--root", dir)
	for _, want := range []string{"tdd-cycle", "verify-change", "safe-refactor", "pr-review"} {
		if !strings.Contains(out, want) {
			t.Errorf("skill list missing shipped default %q:\n%s", want, out)
		}
	}
}

// TestShippedWorkflowSkillsValidate proves the two BMAD-style workflows shipped
// by `csdd init` (wf:product/discovery upstream, wf:development downstream) pass
// csdd's own skill validator — required headings, line/token budget, reference
// triggers. This is the mechanical contract the workflows promise users. It also
// guards the spec-brainstorm on-ramp shipped alongside them.
func TestShippedWorkflowSkillsValidate(t *testing.T) {
	dir := freshWorkspace(t)
	workflowSkills := []string{
		"discovery-product-brief", "discovery-research", "discovery-prfaq",
		"discovery-prd", "discovery-ux-spec", "discovery-handoff",
		"dev-architecture", "dev-epics-stories", "dev-readiness-check",
		"dev-sprint", "dev-retrospective",
	}
	// The shipped discipline skills (test-first, verification, review, refactor) must
	// validate too — a regression guard for their required headings (e.g. ## Goal).
	disciplineSkills := []string{"tdd-cycle", "verify-change", "pr-review", "safe-refactor", "plan-dev"}
	for _, name := range append(workflowSkills, disciplineSkills...) {
		code, out, errOut := run(t, "skill", "validate", name, "--root", dir)
		if code != 0 {
			t.Errorf("shipped skill %q failed validation (code=%d):\n%s%s", name, code, out, errOut)
		}
	}
	// The spec-brainstorm on-ramp (the question-driven front-end to `csdd spec`) must
	// validate too — a regression guard for its required headings.
	if code, out, errOut := run(t, "skill", "validate", "spec-brainstorm", "--root", dir); code != 0 {
		t.Errorf("shipped skill %q failed validation (code=%d):\n%s%s", "spec-brainstorm", code, out, errOut)
	}
	// The quick-prd on-ramp (lightweight single-feature PRD) must validate too.
	if code, out, errOut := run(t, "skill", "validate", "quick-prd", "--root", dir); code != 0 {
		t.Errorf("shipped skill %q failed validation (code=%d):\n%s%s", "quick-prd", code, out, errOut)
	}
	// Both orchestrator agents must scaffold as shown-able artifacts.
	for _, name := range []string{"wf-product-discovery", "wf-development"} {
		if code, showOut, _ := run(t, "agent", "show", name, "--root", dir); code != 0 || !strings.Contains(showOut, "name: "+name) {
			t.Errorf("shipped agent %q not scaffolded by init: code=%d out=%q", name, code, showOut)
		}
	}
}

func TestSkillUnknownAction(t *testing.T) {
	if code, _, errOut := run(t, "skill", "nonsense"); code == 0 || !strings.Contains(errOut, "unknown skill action") {
		t.Errorf("unknown skill action should fail: code=%d err=%q", code, errOut)
	}
}

// ---------- agent ----------

func TestAgentCreate(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "agent", "create", "custom-reviewer",
		"--description", "Read-only adversarial reviewer.",
		"--tools", "Read", "--tools", "Grep",
		"--model", "sonnet",
		"--root", dir)
	if code != 0 {
		t.Fatal("agent create failed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".claude/agents/custom-reviewer.md"))
	for _, want := range []string{"name: custom-reviewer", "tools: Read, Grep", "model: sonnet"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("agent missing %q\n%s", want, data)
		}
	}
}

func TestAgentCreateDefaults(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "agent", "create", "default-tools",
		"--description", "Defaults.", "--root", dir)
	data, _ := os.ReadFile(filepath.Join(dir, ".claude/agents/default-tools.md"))
	if !strings.Contains(string(data), "tools: Read, Grep") {
		t.Errorf("default tools should be Read,Grep:\n%s", data)
	}
	if strings.Contains(string(data), "model:") {
		t.Error("default agent must not include model line")
	}
}

func TestAgentCreateErrors(t *testing.T) {
	dir := freshWorkspace(t)
	cases := [][]string{
		{"agent", "create", "x", "--root", dir}, // no description
		{"agent", "create", "BadName", "--description", "d", "--root", dir},
		{"agent", "create", "--description", "d", "--root", dir}, // no name
	}
	for _, args := range cases {
		if code, _, _ := run(t, args...); code == 0 {
			t.Errorf("%v should fail", args)
		}
	}
	_, _, _ = run(t, "agent", "create", "dup", "--description", "d", "--root", dir)
	if code, _, _ := run(t, "agent", "create", "dup", "--description", "d", "--root", dir); code == 0 {
		t.Error("duplicate agent should fail")
	}
	// --force overwrites.
	if code, _, _ := run(t, "agent", "create", "dup", "--description", "d2", "--force", "--root", dir); code != 0 {
		t.Error("--force should permit overwrite")
	}
}

func TestAgentListShowDelete(t *testing.T) {
	// Bare dir: no shipped agents, so the empty path is exercised. `csdd init`
	// ships default reviewer sub-agents (code-reviewer, test-designer, ...).
	dir := t.TempDir()
	_, out, _ := run(t, "agent", "list", "--root", dir)
	if !strings.Contains(out, "no agents found") {
		t.Errorf("empty agent list: %s", out)
	}
	_, _, _ = run(t, "agent", "create", "rev", "--description", "d", "--root", dir)
	_, out, _ = run(t, "agent", "list", "--root", dir)
	if !strings.Contains(out, "rev") {
		t.Errorf("agent list missing entry: %s", out)
	}
	code, showOut, _ := run(t, "agent", "show", "rev", "--root", dir)
	if code != 0 || !strings.Contains(showOut, "name: rev") {
		t.Errorf("agent show failed: %s", showOut)
	}
	if code, _, _ := run(t, "agent", "show", "ghost", "--root", dir); code == 0 {
		t.Error("show ghost should fail")
	}
	if code, _, _ := run(t, "agent", "delete", "rev", "--root", dir); code == 0 {
		t.Error("delete without --force should fail")
	}
	if code, _, _ := run(t, "agent", "delete", "rev", "--force", "--root", dir); code != 0 {
		t.Error("delete with --force should succeed")
	}
	if code, _, _ := run(t, "agent", "delete", "ghost", "--force", "--root", dir); code == 0 {
		t.Error("delete ghost should fail")
	}
	if code, _, _ := run(t, "agent", "delete", "--force", "--root", dir); code == 0 {
		t.Error("delete without name should fail")
	}
}

func TestAgentUnknownAction(t *testing.T) {
	if code, _, errOut := run(t, "agent", "nonsense"); code == 0 || !strings.Contains(errOut, "unknown agent action") {
		t.Errorf("unknown agent action should fail: code=%d err=%q", code, errOut)
	}
}

// ---------- parseFlags positional ordering ----------

func TestParseFlagsAllowsPositionalsBeforeFlags(t *testing.T) {
	dir := freshWorkspace(t)
	// The whole point: this command order (positional first) used to break
	// because stdlib flag.Parse stops at the first non-flag token.
	code, _, errOut := run(t, "steering", "create", "api-conv", "--inclusion", "fileMatch",
		"--pattern", "src/api/**/*", "--root", dir)
	if code != 0 {
		t.Fatalf("positional-first command rejected: %s", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/steering/api-conv.md")); err != nil {
		t.Error("expected file from positional-first invocation")
	}
}

func TestLeadingGlobalRootFlag(t *testing.T) {
	dir := freshWorkspace(t)
	code, out, errOut := run(t, "--root", dir, "spec", "list")
	if code != 0 {
		t.Fatalf("leading --root should work: code=%d err=%s", code, errOut)
	}
	if !strings.Contains(out, "no specs found") {
		t.Errorf("unexpected output for leading --root:\n%s", out)
	}
}
