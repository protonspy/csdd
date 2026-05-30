package templater

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// TestEmbeddedGuideMatchesSource guards against drift between the human-edited
// source guide (docs/kiro_sdd.md) and the embedded copy that `csdd init`
// scaffolds. It skips gracefully when the repo source isn't reachable (e.g.
// when the package is built from the module cache rather than the repo tree).
func TestEmbeddedGuideMatchesSource(t *testing.T) {
	const sourcePath = "../../docs/kiro_sdd.md"
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Skipf("source guide not reachable (%v); skipping parity check", err)
	}
	embedded, err := Static(FS, "templates/guides/kiro-sdd.md.tmpl")
	if err != nil {
		t.Fatalf("embedded guide missing: %v", err)
	}
	if string(source) != embedded {
		t.Error("docs/kiro_sdd.md and the embedded guide have drifted — re-copy the source into internal/templater/templates/guides/kiro-sdd.md.tmpl")
	}
}

func TestStatic(t *testing.T) {
	out, err := Static(FS, "templates/root/kiroignore.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ".env") {
		t.Errorf("kiroignore template should mention .env")
	}
}

func TestRenderSkill(t *testing.T) {
	out, err := Render(FS, "templates/skill/SKILL.md.tmpl", map[string]string{
		"Name":        "demo-skill",
		"Description": "Demo skill.",
		"Title":       "Demo Skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "name: demo-skill") {
		t.Error("Skill template missing name frontmatter")
	}
	if !strings.Contains(out, "# Demo Skill") {
		t.Error("Skill template missing title")
	}
}

func TestRenderSteeringCustomAuto(t *testing.T) {
	out, err := Render(FS, "templates/steering/custom.md.tmpl", map[string]any{
		"Inclusion":   "auto",
		"AutoName":    "observability",
		"Description": "Logging trigger",
		"Title":       "Observability",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "inclusion: auto") {
		t.Error("frontmatter inclusion missing")
	}
	if !strings.Contains(out, "name: observability") {
		t.Error("auto inclusion should emit name field")
	}
	if !strings.Contains(out, "description: Logging trigger") {
		t.Error("auto inclusion should emit description field")
	}
}

func TestRenderSteeringCustomFileMatch(t *testing.T) {
	out, err := Render(FS, "templates/steering/custom.md.tmpl", map[string]any{
		"Inclusion": "fileMatch",
		"Patterns":  []string{"a", "b"},
		"Title":     "API",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `fileMatchPattern: ["a", "b"]`) {
		t.Errorf("fileMatch frontmatter missing patterns: %s", out)
	}
}

func TestRenderAgentWithAndWithoutModel(t *testing.T) {
	withModel, err := Render(FS, "templates/agent/agent.md.tmpl", map[string]string{
		"Name": "rev", "Description": "d", "Tools": "Read", "Model": "sonnet", "Title": "Rev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withModel, "model: sonnet") {
		t.Error("agent template should include model line when set")
	}
	without, err := Render(FS, "templates/agent/agent.md.tmpl", map[string]string{
		"Name": "rev", "Description": "d", "Tools": "Read", "Model": "", "Title": "Rev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without, "model:") {
		t.Error("agent template should omit model line when empty")
	}
}

func TestRenderMissingTemplate(t *testing.T) {
	if _, err := Render(FS, "templates/nope.tmpl", nil); err == nil {
		t.Error("expected error for missing template")
	}
	if _, err := Static(FS, "templates/nope.tmpl"); err == nil {
		t.Error("expected error for missing static template")
	}
}

func TestRuleFiles(t *testing.T) {
	rules, err := RuleFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	required := []string{
		"ears-format.md",
		"requirements-review-gate.md",
		"design-principles.md",
		"tasks-generation.md",
		"steering-principles.md",
	}
	for _, name := range required {
		if _, ok := rules[name]; !ok {
			t.Errorf("rules missing %q", name)
		}
	}
}

func TestWorkflowTemplateFiles(t *testing.T) {
	files, err := WorkflowTemplateFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	required := []string{
		"specs/requirements.md",
		"specs/design.md",
		"specs/tasks.md",
		"specs/init.json",
		"steering/product.md",
		"steering/api-conventions.md",
		"steering-custom/custom.md",
	}
	for _, name := range required {
		if _, ok := files[name]; !ok {
			t.Errorf("workflow templates missing %q", name)
		}
	}
	// The on-disk steering-custom reference must be clean, fill-in markdown:
	// it must NOT leak Go text/template directives from the render template.
	if custom := files["steering-custom/custom.md"]; strings.Contains(custom, "{{") {
		t.Errorf("steering-custom/custom.md leaks template directives:\n%s", custom)
	}
}

// TestRenderMalformedTemplate covers the template.Parse error branch.
// An unclosed `{{ }}` action is the simplest way to make text/template error.
func TestRenderMalformedTemplate(t *testing.T) {
	memFS := fstest.MapFS{
		"bad.tmpl": &fstest.MapFile{Data: []byte("{{ not valid")},
	}
	if _, err := Render(memFS, "bad.tmpl", nil); err == nil {
		t.Error("Render should fail on a malformed template")
	}
}

// TestRenderExecutionError covers the template.Execute error branch.
// Referencing a method on a nil pointer triggers an execution error.
func TestRenderExecutionError(t *testing.T) {
	memFS := fstest.MapFS{
		// .Value is not present on a nil data context, but Go templates ignore
		// missing fields by default. Use a function call against a missing
		// method to force an execution error.
		"exec.tmpl": &fstest.MapFile{Data: []byte("{{ .NoSuchMethod }}")},
	}
	type empty struct{}
	if _, err := Render(memFS, "exec.tmpl", empty{}); err == nil {
		t.Error("Render should fail when template references missing field")
	}
}

func TestRuleFilesMissingDir(t *testing.T) {
	memFS := fstest.MapFS{
		"templates/root/KIRO.md.tmpl": &fstest.MapFile{Data: []byte("x")},
		// No templates/rules/ directory at all.
	}
	if _, err := RuleFiles(memFS); err == nil {
		t.Error("RuleFiles should fail when templates/rules/ is missing")
	}
}

func TestRuleFilesSkipsSubdirectories(t *testing.T) {
	memFS := fstest.MapFS{
		"templates/rules/x.md.tmpl":          &fstest.MapFile{Data: []byte("body")},
		"templates/rules/sub/nested.md.tmpl": &fstest.MapFile{Data: []byte("hide")},
	}
	rules, err := RuleFiles(memFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rules["x.md"]; !ok {
		t.Error("RuleFiles missed top-level rule")
	}
	if _, ok := rules["nested.md"]; ok {
		t.Error("RuleFiles should not descend into subdirectories")
	}
}
