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

func briefFor(t *testing.T, root, slug, feat string) string {
	t.Helper()
	doc, err := Load(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := doc.Feat(feat)
	if !ok {
		t.Fatalf("feat %q not in plan", feat)
	}
	out, err := FeatBrief(root, doc, f)
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

	out := briefFor(t, root, "p", "upload")

	// Determinism: same plan + feat → byte-identical brief.
	if out2 := briefFor(t, root, "p", "upload"); out != out2 {
		t.Errorf("brief is not byte-deterministic")
	}

	// Stack row listed as a short ref only (the Decided row for go); version/why
	// are NOT inlined — the session fetches them via the graph when it needs them.
	if !strings.Contains(out, "stack:go") {
		t.Errorf("brief should list the governing stack ref: %s", out)
	}
	if strings.Contains(out, "1.22") {
		t.Errorf("brief must NOT inline the stack row version/why (the gate enforces compliance now): %s", out)
	}
	// Wiki ref as path only — NOT the frontmatter description and NOT the page body.
	if !strings.Contains(out, "docs/wiki/pages/storage-design.md") {
		t.Errorf("brief should cite the wiki page path")
	}
	if strings.Contains(out, "How photos are stored") {
		t.Errorf("brief must NOT inline the wiki frontmatter description: %s", out)
	}
	if strings.Contains(out, "SECRET_BODY_TOKEN") {
		t.Errorf("brief must NOT inline the wiki page body (token leaked)")
	}
	// The plan's own verification contract, and the Executor Notes verbatim.
	if !strings.Contains(out, "make check") {
		t.Errorf("brief should carry the plan's Quality Gates")
	}
	if !strings.Contains(out, "Run gofmt before committing") {
		t.Errorf("brief should carry the Executor Notes verbatim")
	}
	// Seeds listed.
	if !strings.Contains(out, "docs/plans/p/seeds/upload/requirements.md") {
		t.Errorf("brief should list the feat's seeds")
	}
}

func TestBriefUnknownFeat(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	doc, _ := Load(root, "p")
	if _, err := FeatBrief(root, doc, Feat{Slug: "ghost"}); err == nil {
		t.Errorf("FeatBrief should error for a feat not in the plan")
	}
}

// TestBriefCarriesNoProcess is the boundary the brief now holds: it describes the
// FEAT, never the development process.
//
// The process lives in the worktree's CLAUDE.md and the `plan-dev` skill, both of
// which the session reads every turn anyway. Restating it here was not merely
// duplicate — the copies drifted, and the brief's authority block argued against
// STOP rules that the plan-session CLAUDE.md replacing the interactive one no longer
// contains. Anything on this list reappearing means the two are diverging again.
func TestBriefCarriesNoProcess(t *testing.T) {
	out := briefFor(t, setupWorkspace(t, "p", briefPlan), "p", "upload")
	for _, banned := range []string{
		"YOU are the approver",      // authority — CLAUDE.md's job
		"csdd spec approve",         // the phase-gate workflow
		"csdd spec init",            // the cycle
		"implementer",               // delegation
		"spec-author",               //  …
		"/csdd-commit",              // git
		"do NOT create a branch",    //  …
		"Forbidden actions",         // the hard rules
		"Verdict protocol",          // the verdict contract
		"An honest `continue`",      // the attempt economics
		"Before you declare `done`", // the feat-exit checklist
	} {
		if strings.Contains(out, banned) {
			t.Errorf("the brief carries process again (%q); that belongs in the plan-session CLAUDE.md:\n%s", banned, out)
		}
	}
	// What it does carry instead: one pointer at where the process lives, and the
	// verdict shape the runner parses.
	if !strings.Contains(out, "plan-dev") || !strings.Contains(out, "CLAUDE.md") {
		t.Errorf("the brief should point at the process rather than restate it:\n%s", out)
	}
	if !strings.Contains(out, `{"status":"done"}`) {
		t.Errorf("the brief should still name the verdict the runner parses:\n%s", out)
	}
}

// TestBriefRendersTheContextPack: a verified pack is the discovered half of the
// brief — where the feat lives, what constrains it, what is already there.
func TestBriefRendersTheContextPack(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	writeFile(t, filepath.Join(root, "docs", "wiki", "pages", "storage-design.md"), "---\ntitle: t\n---\n")
	if err := SavePack(root, "p", "upload", &EnrichPack{
		Touches:   []PackTouch{{Path: "docs/wiki/pages/storage-design.md", Why: "describes the old uploader"}},
		Governors: []PackGovernor{{ID: "stack:go", Constraint: "no new runtime deps", Declared: true}},
		Exists:    []string{"the bucket client already exists"},
		Missing:   []string{"no checksum on write"},
		Traps:     []string{"the fixture server rejects chunked uploads"},
		Flow:      PackFlow{Choice: "unit", Why: "render/CRUD only"},
	}); err != nil {
		t.Fatal(err)
	}
	out := briefFor(t, root, "p", "upload")
	for _, want := range []string{
		"Where this feat lives",
		"describes the old uploader",
		"no new runtime deps",
		"Already there (do not redo)",
		"the bucket client already exists",
		"no checksum on write",
		"the fixture server rejects chunked uploads",
		"Suggested flow: `unit`",
		"not an order", // the flow stays the session's decision
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the brief should render the pack's %q:\n%s", want, out)
		}
	}
	// A pack is still deterministic input: same disk state, same brief (R7.3).
	if out2 := briefFor(t, root, "p", "upload"); out != out2 {
		t.Errorf("a brief with a pack is not byte-deterministic")
	}
}

// TestBriefWithoutPackIsPlanOnly: enrichment is an optimization, never a
// dependency. With no pack on disk the brief is the plan's own content and nothing
// else — which is exactly what a run with `--enrich-model none` gets.
func TestBriefWithoutPackIsPlanOnly(t *testing.T) {
	out := briefFor(t, setupWorkspace(t, "p", briefPlan), "p", "upload")
	for _, absent := range []string{"Where this feat lives", "Already there", "Suggested flow"} {
		if strings.Contains(out, absent) {
			t.Errorf("brief should have no %q section without a pack:\n%s", absent, out)
		}
	}
	if !strings.Contains(out, "Ingest and store photos") {
		t.Errorf("brief should still carry the feat's objective:\n%s", out)
	}
}
