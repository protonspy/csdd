package plan

import (
	"path/filepath"
	"strings"
	"testing"
)

const briefPlan = `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest and store photos | — | M1 | | stack:go [[storage-design]] |

## Quality Gates

- verify: make check
- e2e: go test ./e2e/...

## Executor Notes

Run gofmt before committing. Never touch generated files.
`

func briefFor(t *testing.T, root, slug string, step Step) string {
	t.Helper()
	doc, err := Load(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Brief(root, doc, step)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBriefContentAndDeterminism(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	// A wiki page with a frontmatter description (its body must NOT be inlined).
	writeFile(t, filepath.Join(root, "docs", "wiki", "pages", "storage-design.md"),
		"---\ntitle: Storage Design\ndescription: How photos are stored and addressed.\n---\n# Storage Design\nSECRET_BODY_TOKEN should not appear in the brief.\n")
	// A seed for the feat.
	writeFile(t, filepath.Join(root, "docs", "plans", "p", "seeds", "upload", "requirements.md"), "# seed reqs\n")

	step := Step{Feat: "upload", Step: StepSpecRequirements, Reason: "start requirements"}
	out := briefFor(t, root, "p", step)

	// Determinism: same plan + step → byte-identical brief.
	if out2 := briefFor(t, root, "p", step); out != out2 {
		t.Errorf("brief is not byte-deterministic")
	}

	// Stack row inlined in full (the Decided row for Go).
	if !strings.Contains(out, "Go") || !strings.Contains(out, "1.22") {
		t.Errorf("brief should inline the full stack row: %s", out)
	}
	// Wiki ref as path + description, NOT the page body.
	if !strings.Contains(out, "docs/wiki/pages/storage-design.md") {
		t.Errorf("brief should cite the wiki page path")
	}
	if !strings.Contains(out, "How photos are stored") {
		t.Errorf("brief should include the wiki frontmatter description")
	}
	if strings.Contains(out, "SECRET_BODY_TOKEN") {
		t.Errorf("brief must NOT inline the wiki page body (token leaked)")
	}
	// Forbidden actions and Executor Notes verbatim.
	if !strings.Contains(out, "Do NOT edit plan.md") {
		t.Errorf("brief should carry the forbidden-actions contract")
	}
	if !strings.Contains(out, "Run gofmt before committing") {
		t.Errorf("brief should carry the Executor Notes verbatim")
	}
	// Seeds listed.
	if !strings.Contains(out, "docs/plans/p/seeds/upload/requirements.md") {
		t.Errorf("brief should list the feat's seeds")
	}
	// Step gate: the spec validator for this feat.
	if !strings.Contains(out, "csdd spec validate upload") {
		t.Errorf("requirements step should name the spec-validate gate")
	}
}

func TestBriefTaskStepGates(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	step := Step{Feat: "upload", Step: "task 1.2", Reason: "implement task 1.2"}
	out := briefFor(t, root, "p", step)
	// Implementation task runs the plan's Quality Gates plus graph analyze.
	if !strings.Contains(out, "make check") {
		t.Errorf("task step should list the plan Quality Gates")
	}
	if !strings.Contains(out, "graph analyze --strict") {
		t.Errorf("task step should list the graph traceability gate")
	}
}

func TestBriefUnknownFeat(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	doc, _ := Load(root, "p")
	if _, err := Brief(root, doc, Step{Feat: "ghost", Step: StepSpecRequirements}); err == nil {
		t.Errorf("Brief should error for a feat not in the plan")
	}
}
