package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/cli"
	"github.com/protonspy/csdd/internal/templater"
)

// freshTUIWorkspace bootstraps a full Claude Code workspace (with baseline steering)
// so wizard submit() handlers — which call straight into the cli package — have
// the directory tree they expect.
func freshTUIWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if code := cli.Run([]string{"init", "--with-baseline", "--root", dir}, templater.FS); code != 0 {
		t.Fatalf("init failed: exit %d", code)
	}
	return dir
}

func TestValidationHelpers(t *testing.T) {
	if err := validateKebab("api-conventions"); err != nil {
		t.Errorf("valid kebab rejected: %v", err)
	}
	if validateKebab("Not Kebab") == nil {
		t.Error("invalid kebab accepted")
	}
	if validateNonEmpty("x") != nil {
		t.Error("non-empty value rejected")
	}
	if validateNonEmpty("   ") == nil {
		t.Error("whitespace-only value should be rejected")
	}
}

func TestSplitCSVAndGetStr(t *testing.T) {
	got := splitCSV("a, b ,, c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("splitCSV unexpected: %#v", got)
	}
	if splitCSV("") != nil {
		t.Error("splitCSV(\"\") should be nil")
	}
	state := map[string]any{"name": "x", "n": 5}
	if getStr(state, "name") != "x" {
		t.Error("getStr should return the string value")
	}
	if getStr(state, "n") != "" {
		t.Error("getStr on non-string should return empty")
	}
	if getStr(state, "missing") != "" {
		t.Error("getStr on missing key should return empty")
	}
}

func TestNewWizardFieldsPerKind(t *testing.T) {
	cases := []struct {
		kind  wizardKind
		title string
		n     int
	}{
		{wizSteering, "Create steering", 5},
		{wizSpec, "Spec wizard", 5},
		{wizSkill, "Create skill", 3},
		{wizAgent, "Create agent", 5},
		{wizMCP, "Add MCP server", 7},
		{wizPreset, "Install MCP preset", 1},
	}
	for _, c := range cases {
		w := newWizard(c.kind, templater.FS, t.TempDir())
		if w.title != c.title {
			t.Errorf("kind %d title = %q, want %q", c.kind, w.title, c.title)
		}
		if len(w.fields) != c.n {
			t.Errorf("kind %d field count = %d, want %d", c.kind, len(w.fields), c.n)
		}
	}
}

// TestWizardSteeringAlwaysSkipsConditionalFields walks the steering wizard with
// inclusion=always and asserts the pattern + description steps are skipped.
func TestWizardSteeringAlwaysSkipsConditionalFields(t *testing.T) {
	w := newWizard(wizSteering, templater.FS, t.TempDir())

	// Step 0: name (text).
	w.fields[0].input.SetValue("api-conv")
	if err := w.commit(); err != nil {
		t.Fatalf("commit name: %v", err)
	}
	if done, _ := w.advance(); done {
		t.Fatal("should not be done after name")
	}
	if w.cursor != 1 {
		t.Fatalf("expected inclusion at cursor 1, got %d", w.cursor)
	}

	// Step 1: inclusion (select) → pick "always" (index 0).
	w.fields[1].selIdx = 0
	if err := w.commit(); err != nil {
		t.Fatalf("commit inclusion: %v", err)
	}
	done, _ := w.advance()
	if done {
		t.Fatal("should land on the title step, not finish")
	}
	if w.cursor != 4 {
		t.Fatalf("inclusion=always should skip patterns+description and land on title (cursor 4), got %d", w.cursor)
	}
	if _, ok := w.state["patterns"]; ok {
		t.Error("patterns must not be committed when skipped")
	}
	if _, ok := w.state["description"]; ok {
		t.Error("description must not be committed when skipped")
	}

	// Step 4: optional title — leave empty, commit, advance → done.
	if err := w.commit(); err != nil {
		t.Fatalf("commit title: %v", err)
	}
	if done, _ := w.advance(); !done {
		t.Fatal("should be done after the last field")
	}
	if w.state["inclusion"] != "always" || w.state["name"] != "api-conv" {
		t.Errorf("unexpected final state: %#v", w.state)
	}
}

// TestWizardFileMatchRequiresPattern confirms the conditional pattern field is
// reached for inclusion=fileMatch and that its validator rejects empty input.
func TestWizardFileMatchRequiresPattern(t *testing.T) {
	w := newWizard(wizSteering, templater.FS, t.TempDir())
	w.fields[0].input.SetValue("api-conv")
	_ = w.commit()
	_, _ = w.advance()
	// inclusion = fileMatch (index 1).
	w.fields[1].selIdx = 1
	_ = w.commit()
	if done, _ := w.advance(); done {
		t.Fatal("fileMatch should require the patterns step")
	}
	if w.cursor != 2 {
		t.Fatalf("expected patterns at cursor 2, got %d", w.cursor)
	}
	// Empty patterns must fail validation.
	w.fields[2].input.SetValue("")
	if err := w.commit(); err == nil {
		t.Error("empty patterns should fail validation for fileMatch")
	}
}

func TestWizardRetreat(t *testing.T) {
	w := newWizard(wizSkill, templater.FS, t.TempDir())
	w.fields[0].input.SetValue("my-skill")
	_ = w.commit()
	_, _ = w.advance()
	if w.cursor != 1 {
		t.Fatalf("setup: expected cursor 1, got %d", w.cursor)
	}
	w.retreat()
	if w.cursor != 0 {
		t.Errorf("retreat should return to cursor 0, got %d", w.cursor)
	}
	// retreat at the start is a no-op.
	w.retreat()
	if w.cursor != 0 {
		t.Errorf("retreat at start should stay at 0, got %d", w.cursor)
	}
}

func TestWizardSubmitSkill(t *testing.T) {
	dir := freshTUIWorkspace(t)
	w := wizardModel{kind: wizSkill, templates: templater.FS, root: dir, state: map[string]any{
		"name":        "spec-tasks",
		"description": "Generate tasks.md annotations.",
		"title":       "",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("skill submit failed: %#v", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/skills/spec-tasks/SKILL.md")); err != nil {
		t.Errorf("SKILL.md not created: %v", err)
	}
}

// TestWizardSubmitSpecInitFlow asserts the wizard's spec-init submit carries the
// selected development flow into the shared SpecInit op (Req 1.4).
func TestWizardSubmitSpecInitFlow(t *testing.T) {
	dir := freshTUIWorkspace(t)
	w := wizardModel{kind: wizSpec, templates: templater.FS, root: dir, state: map[string]any{
		"operation": "init",
		"feature":   "wizard-flow",
		"flow":      "unit",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("spec init submit failed: %#v", msg)
	}
	b, err := os.ReadFile(filepath.Join(dir, "specs", "wizard-flow", "spec.json"))
	if err != nil {
		t.Fatalf("spec.json not created: %v", err)
	}
	if !strings.Contains(string(b), `"development_flow": "unit"`) {
		t.Errorf("wizard did not persist flow=unit:\n%s", b)
	}
}

func TestWizardSubmitAgentDefaultsTools(t *testing.T) {
	dir := freshTUIWorkspace(t)
	// Empty tools slice in state must fall back to least-privilege Read+Grep.
	w := wizardModel{kind: wizAgent, templates: templater.FS, root: dir, state: map[string]any{
		"name":        "custom-reviewer",
		"description": "Read-only reviewer",
		"tools":       []string{},
		"model":       "none",
		"title":       "",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("agent submit failed: %#v", msg)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude/agents/custom-reviewer.md"))
	if err != nil {
		t.Fatalf("agent file not created: %v", err)
	}
	if !strings.Contains(string(data), "Read") || !strings.Contains(string(data), "Grep") {
		t.Errorf("expected default Read+Grep tools in:\n%s", data)
	}
}

func TestWizardSubmitSteeringFileMatch(t *testing.T) {
	dir := freshTUIWorkspace(t)
	w := wizardModel{kind: wizSteering, templates: templater.FS, root: dir, state: map[string]any{
		"name":      "api-conv",
		"inclusion": "fileMatch",
		"patterns":  "src/api/**/*, **/*Controller.*",
		"title":     "",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("steering submit failed: %#v", msg)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude/steering/api-conv.md"))
	if err != nil {
		t.Fatalf("steering file not created: %v", err)
	}
	if !strings.Contains(string(data), "inclusion: fileMatch") {
		t.Errorf("expected fileMatch frontmatter in:\n%s", data)
	}
}

// TestWizardMCPTransportSkips checks that the stdio transport reaches the
// command step and skips the remote-only url step (and vice versa).
func TestWizardMCPTransportSkips(t *testing.T) {
	w := newWizard(wizMCP, templater.FS, t.TempDir())
	w.fields[0].input.SetValue("filesystem")
	_ = w.commit()
	_, _ = w.advance()
	// transport select (index 1): pick "sse" (index 1) → remote.
	w.fields[1].selIdx = 1
	_ = w.commit()
	done, _ := w.advance()
	if done {
		t.Fatal("remote transport should still need the url step")
	}
	// command (2) and args (3) are stdio-only and must be skipped → land on url (4).
	if w.cursor != 4 {
		t.Fatalf("sse transport should land on the url step (cursor 4), got %d", w.cursor)
	}
}

func TestWizardSubmitMCPStdio(t *testing.T) {
	dir := freshTUIWorkspace(t)
	w := wizardModel{kind: wizMCP, templates: templater.FS, root: dir, state: map[string]any{
		"name":      "filesystem",
		"transport": "stdio",
		"command":   "npx",
		"args":      "-y, @scope/server, .",
		"env":       "TOKEN=abc",
		"disabled":  "no",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("mcp stdio submit failed: %#v", msg)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"filesystem", "\"command\": \"npx\"", "@scope/server", "TOKEN"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("mcp.json missing %q:\n%s", want, data)
		}
	}
}

func TestWizardSubmitMCPRemote(t *testing.T) {
	dir := freshTUIWorkspace(t)
	w := wizardModel{kind: wizMCP, templates: templater.FS, root: dir, state: map[string]any{
		"name":      "linear",
		"transport": "sse",
		"url":       "https://mcp.linear.app/sse",
		"disabled":  "yes",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("mcp remote submit failed: %#v", msg)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	for _, want := range []string{"linear", "\"type\": \"sse\"", "\"disabled\": true"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("mcp.json missing %q:\n%s", want, data)
		}
	}
}

func TestWizardSubmitMCPPreset(t *testing.T) {
	dir := freshTUIWorkspace(t)
	w := wizardModel{kind: wizPreset, templates: templater.FS, root: dir, state: map[string]any{
		"preset": "context7",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("mcp preset submit failed: %#v", msg)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	for _, want := range []string{"context7", "\"url\"", "\"type\": \"http\""} {
		if !strings.Contains(string(data), want) {
			t.Errorf("mcp.json missing %q:\n%s", want, data)
		}
	}
}

func TestWizardSubmitSpecInit(t *testing.T) {
	dir := freshTUIWorkspace(t)
	// spec init goes through cli.Run which resolves the workspace from cwd, so
	// run from inside the workspace.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	w := wizardModel{kind: wizSpec, templates: templater.FS, root: dir, state: map[string]any{
		"operation": "init",
		"feature":   "photo-albums",
	}}
	msg := w.submit()()
	rm, ok := msg.(resultMsg)
	if !ok || rm.isErr {
		t.Fatalf("spec init submit failed: %#v", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "specs/photo-albums/spec.json")); err != nil {
		t.Errorf("spec.json not created: %v", err)
	}
}

func TestWizardSubmitSpecGenerateAndApprove(t *testing.T) {
	dir := freshTUIWorkspace(t)
	if code := cli.Run([]string{"spec", "init", "feat", "--root", dir}, templater.FS); code != 0 {
		t.Fatalf("spec init: exit %d", code)
	}

	gen := wizardModel{kind: wizSpec, templates: templater.FS, root: dir, state: map[string]any{
		"operation": "generate", "feature": "feat", "artifact": "requirements",
	}}
	if rm, ok := gen.submit()().(resultMsg); !ok || rm.isErr {
		t.Fatalf("generate submit failed: %#v", rm)
	}

	// Replace the scaffold with EARS-valid content so approval passes its
	// validation gate without --force (the wizard never forces).
	valid := "### Requirement 1: Thing\n\n**Acceptance Criteria**\n1. WHEN a user acts THEN the system SHALL respond.\n"
	if err := os.WriteFile(filepath.Join(dir, "specs/feat/requirements.md"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}

	app := wizardModel{kind: wizSpec, templates: templater.FS, root: dir, state: map[string]any{
		"operation": "approve", "feature": "feat", "phase": "requirements",
	}}
	if rm, ok := app.submit()().(resultMsg); !ok || rm.isErr {
		t.Fatalf("approve submit failed: %#v", rm)
	}
}
