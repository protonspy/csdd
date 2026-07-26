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
}

func TestBriefSelfChecks(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	out := briefFor(t, root, "p", "upload")
	// The self-checks the session runs before declaring done: spec validate, graph
	// analyze, and the plan's own Quality Gates.
	if !strings.Contains(out, "csdd spec validate upload") {
		t.Errorf("brief should tell the session to run spec validate itself")
	}
	if !strings.Contains(out, "graph analyze --strict") {
		t.Errorf("brief should list the graph traceability self-check")
	}
	if !strings.Contains(out, "make check") {
		t.Errorf("brief should inject the plan Quality Gates as self-checks")
	}
	// The verdict protocol is done|continue only.
	if !strings.Contains(out, "`done`") || !strings.Contains(out, "`continue`") {
		t.Errorf("brief should teach the done|continue verdict protocol")
	}
}

// TestBriefApprovalAuthority: the brief must grant the autonomous session authority
// to approve its own spec phases, so CLAUDE.md's interactive "a human authorizes"
// STOP rules do not stall the loop at approval.
func TestBriefApprovalAuthority(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	out := briefFor(t, root, "p", "upload")
	if !strings.Contains(out, "YOU are the approver") {
		t.Errorf("brief should assert the session's approval authority: %s", out)
	}
	if !strings.Contains(out, "csdd plan approve p") {
		t.Errorf("brief should point at the plan-level human gate already given")
	}
	if !strings.Contains(out, "csdd spec approve upload") {
		t.Errorf("brief should tell the session to self-approve its spec phases")
	}
	if !strings.Contains(out, "plan-dev") {
		t.Errorf("brief should point the session at the plan-dev skill")
	}
}

// TestBriefDelegatesImplementation: the mission must route task implementation to
// the `implementer` sub-agent (the orchestrator decides, the fast sub-agent
// executes), not have the orchestrating session hand-write task code inline.
func TestBriefDelegatesImplementation(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	out := briefFor(t, root, "p", "upload")
	if !strings.Contains(out, "implementer") {
		t.Errorf("brief should delegate task implementation to the implementer sub-agent:\n%s", out)
	}
	if !strings.Contains(out, "DELEGATING") && !strings.Contains(out, "delegat") {
		t.Errorf("brief should tell the session to delegate implementation, not hand-write it inline")
	}
}

func TestBriefUnknownFeat(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	doc, _ := Load(root, "p")
	if _, err := FeatBrief(root, doc, Feat{Slug: "ghost"}); err == nil {
		t.Errorf("FeatBrief should error for a feat not in the plan")
	}
}

// TestBriefIsFlowAware guards the brief against re-hardcoding one development flow.
// It used to say the implementer works "test-first" and to name `tdd-cycle` as the
// only fallback, which silently overrode a spec that declared `unit`: the flow field
// existed, the validator now enforces the task shape it implies, and the brief told
// the session to ignore both.
func TestBriefIsFlowAware(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	out := briefFor(t, root, "p", "upload")
	for _, want := range []string{
		"development_flow", // the brief points at the spec's declared flow
		"tdd-cycle",        // …and names the cycle skill for each side of it
		"unit-cycle",
		"--flow", // authoring picks the flow deliberately
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief should mention %q so the session honours the spec's flow:\n%s", want, out)
		}
	}
	// The old wording made test-first unconditional. Any reintroduction of a bare
	// "test-first" instruction re-breaks the unit flow.
	if strings.Contains(out, "test-first") {
		t.Errorf("brief hardcodes 'test-first', which overrides a spec declaring development_flow 'unit'")
	}
}

// TestBriefDelegatesTheCommandGate guards the split the feat-exit block encodes:
// the session reads state itself, and delegates the command gate.
//
// Running the plan's Quality Gates inline lands the whole suite, lint and
// typecheck output in the orchestrator's context — which is then re-read on every
// remaining turn of the feat. That is 58% of a measured session's wall clock
// spent in API calls, and reading exit codes does not need the orchestrator's
// model.
func TestBriefDelegatesTheCommandGate(t *testing.T) {
	root := setupWorkspace(t, "p", briefPlan)
	out := briefFor(t, root, "p", "upload")
	for _, want := range []string{
		"quality-gate",  // the sub-agent that owns the command gate
		"code-reviewer", // dispatched with it, so the two overlap
		"--run --lang",  // and it records the Tier-3 evidence through csdd
		"csdd spec validate",
		"graph analyze --strict",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("feat-exit block should mention %q:\n%s", want, out)
		}
	}
	// The evidence artifact is what the verdict gate reads; a delegated gate that
	// only reports in prose leaves it stale and the `done` is refused.
	if !strings.Contains(out, "test-report") {
		t.Error("the delegated gate must still record test-report.json through csdd")
	}
}

// TestBriefDispatchesTheTaskNotTheSpec keeps the implementer dispatch narrow.
// Seven implementers each re-reading an 800-line spec pays the authoring cost
// once per sub-agent, in contexts that re-read it on every one of their turns.
func TestBriefDispatchesTheTaskNotTheSpec(t *testing.T) {
	out := briefFor(t, setupWorkspace(t, "p", briefPlan), "p", "upload")
	for _, want := range []string{"_Boundary:", "acceptance criteria", "design.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("the implementer dispatch should name %q as what to hand over:\n%s", want, out)
		}
	}
}
