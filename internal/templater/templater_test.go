package templater

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestStatic(t *testing.T) {
	out, err := Static(FS, "templates/root/CLAUDE.md.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "csdd") {
		t.Errorf("CLAUDE.md template should mention csdd")
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

// TestShippedArtifactsPresent guards the artifact set `csdd init` scaffolds. A
// name missing here means init silently stopped shipping it.
//
// The BMAD-style wf:product/discovery and wf:development trees this test used to
// cover were retired: they were a second methodology running beside csdd's own
// requirements -> design -> tasks, producing artifacts (architecture.md,
// sprint-status.yaml, retrospective.md) that nothing downstream read.
func TestShippedArtifactsPresent(t *testing.T) {
	skills, err := SkillFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	requiredSkills := []string{
		// the per-task discipline, one skill per development_flow
		"tdd-cycle/SKILL.md",
		"unit-cycle/SKILL.md",
		"verify-change/SKILL.md",
		"safe-refactor/SKILL.md",
		"pr-review/SKILL.md",
		// the autonomous loop and its on-ramps
		"plan-dev/SKILL.md",
		"prd/SKILL.md",
		"quick-prd/SKILL.md",
		"quick-prd/assets/prd-template.md",
		"spec-brainstorm/SKILL.md",
		// the knowledge base
		"graph/SKILL.md",
		"wiki/SKILL.md",
		"glossary/SKILL.md",
		"stack/SKILL.md",
		// frontend QA — Playwright e2e (the e2e arm of the tdd-e2e flow)
		"frontend-e2e-qa/SKILL.md",
	}
	for _, name := range requiredSkills {
		if _, ok := skills[name]; !ok {
			t.Errorf("shipped skills missing %q", name)
		}
	}

	agents, err := AgentFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"implementer.md", "spec-author.md", "code-reviewer.md", "security-reviewer.md", "quality-gate.md",
	} {
		if _, ok := agents[name]; !ok {
			t.Errorf("shipped agents missing %q", name)
		}
	}
	// The retired cluster must stay retired: shipping one again without its
	// references is how a half-removed methodology comes back.
	for _, gone := range []string{"dev-architecture/SKILL.md", "discovery-prd/SKILL.md"} {
		if _, ok := skills[gone]; ok {
			t.Errorf("retired skill %q is shipping again", gone)
		}
	}
	for _, gone := range []string{"wf-development.md", "wf-product-discovery.md", "test-designer.md"} {
		if _, ok := agents[gone]; ok {
			t.Errorf("retired agent %q is shipping again", gone)
		}
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
		"templates/root/CLAUDE.md.tmpl": &fstest.MapFile{Data: []byte("x")},
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

// TestImplementerAgentShipped covers requirements 1.1, 1.3, 1.4, 1.5: the generic
// implementer agent ships alongside (not instead of) the existing agents, with
// least-privilege tools and well-formed frontmatter.
func TestImplementerAgentShipped(t *testing.T) {
	agents, err := AgentFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := agents["implementer.md"]
	if !ok {
		t.Fatal("AgentFiles is missing implementer.md")
	}
	for _, sibling := range []string{"code-reviewer.md", "security-reviewer.md", "quality-gate.md"} {
		if _, ok := agents[sibling]; !ok {
			t.Errorf("implementer ships without its sibling agent %q", sibling)
		}
	}
	for _, want := range []string{"name: implementer", "tools: Read, Grep, Glob, Edit, Write, Bash"} {
		if !strings.Contains(body, want) {
			t.Errorf("implementer.md frontmatter missing %q", want)
		}
	}
	if !strings.Contains(body, "description:") {
		t.Error("implementer.md missing a description")
	}
}

// TestSpecAuthorAgentShipped covers the spec-author agent: it ships alongside the
// other agents, drafts (never approves) one spec phase on the cheap sonnet model,
// scaffolds via `csdd spec generate`, and defers approval to the orchestrator.
func TestSpecAuthorAgentShipped(t *testing.T) {
	agents, err := AgentFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := agents["spec-author.md"]
	if !ok {
		t.Fatal("AgentFiles is missing spec-author.md")
	}
	// Least-privilege tools: it writes spec artifacts, so Edit/Write/Bash are in,
	// but it must not approve or implement.
	for _, want := range []string{
		"name: spec-author",
		"tools: Read, Grep, Glob, Edit, Write, Bash",
		"model: sonnet",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("spec-author.md frontmatter missing %q", want)
		}
	}
	if !strings.Contains(body, "description:") {
		t.Error("spec-author.md missing a description")
	}
}

// TestSpecAuthorAgentEncodesDiscipline pins the authoring discipline and the
// hand-off to the orchestrator: scaffold via generate, consult the graph, write
// to EARS/traceability, validate, and never approve.
func TestSpecAuthorAgentEncodesDiscipline(t *testing.T) {
	agents, err := AgentFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	body := agents["spec-author.md"]
	for _, want := range []string{
		"csdd spec generate",        // scaffold, not hand-write
		"graph",                     // consult the knowledge graph
		"EARS",                      // requirements contract
		"Requirements Traceability", // design contract
		"csdd spec validate",        // self-validate before reporting
		"do not approve",            // the orchestrator owns approval
	} {
		if !strings.Contains(body, want) {
			t.Errorf("spec-author.md should reference %q", want)
		}
	}
	// It names the approve command only to forbid it — never to instruct calling it.
	for _, bad := range []string{"csdd spec approve <feat>", "spec approve <feat>"} {
		if strings.Contains(body, bad) {
			t.Errorf("spec-author.md must not instruct approving its own phase: %q", bad)
		}
	}
}
func TestImplementerAgentEncodesDiscipline(t *testing.T) {
	agents, err := AgentFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	body := agents["implementer.md"]
	for _, want := range []string{
		"tdd-cycle",            // 2.1 RED→GREEN→REFACTOR under tdd/tdd-e2e
		"unit-cycle",           // 2.1 implement→cover→gate under unit
		"Scope discipline",     // 2.2 one task, in-boundary, no scope creep
		"one task",             // 2.2 binds the discipline, not a bare keyword
		"verify-change",        // 2.3 run the gate
		"test-report",          // 2.3 record evidence
		"Implementation Notes", // 2.5 notes
		"[x]",                  // 2.4 mark the task done
		"Specialize",           // 4.3 specialization section
		"language-agnostic",    // 4.1 no single language as a hard requirement
	} {
		if !strings.Contains(body, want) {
			t.Errorf("implementer.md should reference %q", want)
		}
	}
	if !strings.Contains(strings.ToLower(body), "steering") {
		t.Error("implementer.md should defer language/framework specifics to steering (4.1/4.2)")
	}
}

// TestTddCycleMarksTaskDone covers requirements 3.1, 3.2, 3.3, 5.3: the shipped
// tdd-cycle now instructs marking the completed task [x] on green, and keeps its
// existing steps (no regression).
func TestTddCycleMarksTaskDone(t *testing.T) {
	skills, err := SkillFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := skills["tdd-cycle/SKILL.md"]
	if !ok {
		t.Fatal("SkillFiles is missing tdd-cycle/SKILL.md")
	}
	for _, anchor := range []string{
		"RED — write the failing test",
		"GREEN — minimal implementation",
		// The evidence step is now the single Tier-2 task-exit gate; it used to be
		// "Record the spec evidence", one of two full-suite runs per task.
		"Tier 2 — the task-exit gate",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("tdd-cycle lost existing step %q", anchor)
		}
	}
	// 3.1: the workflow step exists; 3.3: the Completion-Criteria box exists;
	// 3.2: both pin "only" the completed task. Assert each anchor distinctly so a
	// regression that drops one (but keeps the other) still fails.
	if !strings.Contains(body, "Mark the task done") {
		t.Error("tdd-cycle should have a 'Mark the task done' workflow step (3.1)")
	}
	if !strings.Contains(body, "[x]") || !strings.Contains(body, "tasks.md") {
		t.Error("tdd-cycle should instruct marking the completed task [x] in tasks.md (3.1)")
	}
	if !strings.Contains(body, "only that task") {
		t.Error("tdd-cycle Completion Criteria should mark only the completed task (3.2/3.3)")
	}
}

// TestSetupCommandsShipped covers requirements 1.1, 1.4, 5.1: both setup commands
// ship alongside (not instead of) the existing csdd-commit command.
func TestSetupCommandsShipped(t *testing.T) {
	cmds, err := CommandFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"csdd-setup-init.md", "csdd-setup-update.md", "csdd-commit.md"} {
		if _, ok := cmds[want]; !ok {
			t.Errorf("CommandFiles is missing %q", want)
		}
	}
}

// TestSetupCommandsFrontmatter covers requirements 1.3, 4.1: each command has a
// description and an allowed-tools list scoped to the csdd CLI (least privilege).
func TestSetupCommandsFrontmatter(t *testing.T) {
	cmds, err := CommandFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"csdd-setup-init.md", "csdd-setup-update.md"} {
		body := cmds[name]
		if !strings.Contains(body, "description:") {
			t.Errorf("%s missing a description", name)
		}
		if !strings.Contains(body, "allowed-tools:") {
			t.Errorf("%s missing allowed-tools", name)
		}
		if !strings.Contains(body, "Bash(csdd") {
			t.Errorf("%s allowed-tools should scope Bash to the csdd CLI (least privilege)", name)
		}
		// Least privilege (4.1): no unscoped, blanket Bash token.
		for _, line := range strings.Split(body, "\n") {
			if !strings.HasPrefix(line, "allowed-tools:") {
				continue
			}
			for _, tok := range strings.Split(strings.TrimPrefix(line, "allowed-tools:"), ",") {
				if strings.TrimSpace(tok) == "Bash" {
					t.Errorf("%s grants unscoped Bash; scope it like Bash(csdd:*)", name)
				}
			}
		}
	}
}

// TestSetupCommandsEncodeFlow covers requirements 2.x and 3.x: the command bodies
// encode the intended flow and drive the csdd CLI.
func TestSetupCommandsEncodeFlow(t *testing.T) {
	cmds, err := CommandFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	initBody := cmds["csdd-setup-init.md"]
	for _, want := range []string{
		"csdd init",     // 2.2 init if needed / 2.7 layer on top
		"csdd steering", // 2.3 steering via CLI
		"csdd agent",    // 2.4 derive specialized agent
		"implementer",   // 2.4 derive from the shipped implementer
		"review",        // 2.6 report needs-review
	} {
		if !strings.Contains(initBody, want) {
			t.Errorf("csdd-setup-init should reference %q", want)
		}
	}
	if !strings.Contains(strings.ToLower(initBody), "detect") {
		t.Error("csdd-setup-init should detect the project stack (2.1)")
	}
	updBody := cmds["csdd-setup-update.md"]
	for _, want := range []string{
		"csdd update", // 3.2 preserve edits via csdd update
		"targeted",    // 3.1 targeted adjustments, not a full rebuild
	} {
		if !strings.Contains(updBody, want) {
			t.Errorf("csdd-setup-update should reference %q", want)
		}
	}
}

// TestSpecTemplatesCiteTheirRules keeps every artifact template pointing at the
// rules that govern its content.
//
// The rules only reach the model when something cites them — CLAUDE.md carries no
// @-imports and steering/ can be empty — so an uncited rule is a rule that never
// runs. requirements.md cited ears-format and tasks.md cited tasks-generation,
// but design.md, the largest artifact in a real corpus at ~470 lines against
// requirements' ~139, cited nothing at all. The review-gate counterparts were
// missing on all three.
func TestSpecTemplatesCiteTheirRules(t *testing.T) {
	specs, err := WorkflowTemplateFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"specs/requirements.md": {"ears-format.md", "requirements-review-gate.md"},
		"specs/design.md":       {"design-principles.md", "design-review-gate.md"},
		"specs/tasks.md":        {"tasks-generation.md", "tasks-parallel-analysis.md"},
	}
	for file, rules := range want {
		body, ok := specs[file]
		if !ok {
			t.Errorf("spec templates missing %q", file)
			continue
		}
		for _, rule := range rules {
			if !strings.Contains(body, rule) {
				t.Errorf("%s should cite %s — an uncited rule never reaches the model", file, rule)
			}
		}
	}
}
