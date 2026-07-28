package plan

import (
	"path/filepath"
	"strings"
	"testing"
)

// setupWorkspace lays down a workspace with a stack contract and one wiki page,
// then writes the given plan.md under docs/plans/<slug>/. It returns the root.
func setupWorkspace(t *testing.T, slug, planMD string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "stack.md"), `# Tech contract

## Decided

| Domain | Choice | Version | Why | Refs |
|---|---|---|---|---|
| Language | Go | 1.22 | speed | — |
| HTTP router | chi | v5 | small | — |

## Rules
`)
	writeFile(t, filepath.Join(root, "docs", "wiki", "pages", "storage-design.md"), "# Storage Design\n")
	writeFile(t, filepath.Join(root, "docs", "plans", slug, "plan.md"), planMD)
	return root
}

func msgsContain(issues []issueStrings, sub string) bool {
	for _, i := range issues {
		if strings.Contains(i.s, sub) {
			return true
		}
	}
	return false
}

type issueStrings struct{ s string }

func validateStrings(t *testing.T, root, slug string) []issueStrings {
	t.Helper()
	doc, err := Load(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	var out []issueStrings
	for _, i := range ValidatePlan(doc, root) {
		out = append(out, issueStrings{i.String()})
	}
	return out
}

func TestValidateClean(t *testing.T) {
	root := setupWorkspace(t, "photos", `---
name: photos
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest photos | — | M1 | | stack:go [[storage-design]] |
| 2 | thumbs | Thumbnails | upload | M1 | P | stack:chi |

## Quality Gates

- verify: make check
`)
	issues := validateStrings(t, root, "photos")
	if len(issues) != 0 {
		t.Fatalf("expected clean plan, got issues: %v", issues)
	}
}

func TestValidateUnknownDepAndCycle(t *testing.T) {
	root := setupWorkspace(t, "p", `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | b | M1 | | |
| 2 | b | B | a | M1 | | |
| 3 | c | C | ghost | M1 | | |

## Quality Gates

- verify: make check
`)
	issues := validateStrings(t, root, "p")
	if !msgsContain(issues, "depends on unknown feat 'ghost'") {
		t.Errorf("missing unknown-dep finding: %v", issues)
	}
	if !msgsContain(issues, "feat dependency cycle") {
		t.Errorf("missing cycle finding: %v", issues)
	}
}

func TestValidateRefsAndGates(t *testing.T) {
	root := setupWorkspace(t, "p", `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | stack:redis [[missing-page]] |
`)
	issues := validateStrings(t, root, "p")
	if !msgsContain(issues, "undeclared tech 'stack:redis'") {
		t.Errorf("missing undeclared-tech finding: %v", issues)
	}
	if !msgsContain(issues, "broken plan ref '[[missing-page]]'") {
		t.Errorf("missing broken-ref finding: %v", issues)
	}
	if !msgsContain(issues, "no quality gates declared") {
		t.Errorf("missing empty-gates finding: %v", issues)
	}
}

func TestValidateDupAndInvalidSlugAndRange(t *testing.T) {
	root := setupWorkspace(t, "p", `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | good | A | — | M1 | | |
| 2 | good | dup | — | M1 | | |
| 3 | Bad_Slug | invalid | — | M1 | | |
| 4 | ranged | R | 1-3 | M1 | | |

## Quality Gates

- verify: make check
`)
	issues := validateStrings(t, root, "p")
	if !msgsContain(issues, "duplicate feat slug 'good'") {
		t.Errorf("missing duplicate finding: %v", issues)
	}
	if !msgsContain(issues, "invalid feat slug 'Bad_Slug'") {
		t.Errorf("missing invalid-slug finding: %v", issues)
	}
	if !msgsContain(issues, "range shorthand '1-3'") {
		t.Errorf("missing range finding: %v", issues)
	}
}

func TestValidateSeedEARS(t *testing.T) {
	root := setupWorkspace(t, "p", `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | |

## Quality Gates

- verify: make check
`)
	// A seed requirements.md with a numbered criterion that lacks EARS structure.
	writeFile(t, filepath.Join(root, "docs", "plans", "p", "seeds", "a", "requirements.md"),
		"### Requirement 1: Thing\n\n**Acceptance Criteria**\n\n1. The user clicks the button and it should work.\n")
	issues := validateStrings(t, root, "p")
	if !msgsContain(issues, "docs/plans/p/seeds/a/requirements.md") {
		t.Errorf("seed EARS finding should be path-qualified: %v", issues)
	}
}

func TestValidateDeterministic(t *testing.T) {
	root := setupWorkspace(t, "p", `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | ghost | M1 | | stack:redis |
| 2 | b | B | zzz | M1 | | [[nope]] |
`)
	a := validateStrings(t, root, "p")
	b := validateStrings(t, root, "p")
	if len(a) != len(b) {
		t.Fatalf("nondeterministic count %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].s != b[i].s {
			t.Errorf("nondeterministic order at %d: %q vs %q", i, a[i].s, b[i].s)
		}
	}
}

// TestValidateDesignConformance pins the mechanical backstop that lets the brief
// drop ADR bodies: a feat bound to adr:<slug> must have its authored design.md
// cite that governor. Silent when design.md is absent (plan validated before any
// spec is authored) and clears once the design references the slug.
func TestValidateDesignConformance(t *testing.T) {
	const planMD = `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | adr:auth |

## Quality Gates

- verify: make check
`
	root := setupWorkspace(t, "p", planMD)
	writeADRs(t, root, map[string]string{
		"0001-auth.md": "# Use OAuth for authentication\n\nContext. Decision. Why.\n",
	})

	// design.md authored without citing the governing ADR → finding.
	writeFile(t, filepath.Join(root, "specs", "a", "design.md"),
		"# Design\n\nNo mention of the decision.\n")
	issues := validateStrings(t, root, "p")
	if !msgsContain(issues, "does not cite governing ADR 'adr:auth'") {
		t.Errorf("missing ADR-conformance finding: %v", issues)
	}

	// Once the design cites the slug, the finding clears.
	writeFile(t, filepath.Join(root, "specs", "a", "design.md"),
		"# Design\n\nHonors adr:auth for the login boundary.\n")
	if got := validateStrings(t, root, "p"); msgsContain(got, "does not cite governing ADR") {
		t.Errorf("citing the ADR should clear the finding: %v", got)
	}

	// No design.md authored yet → silent (plan validated before spec authoring).
	root2 := setupWorkspace(t, "p", planMD)
	writeADRs(t, root2, map[string]string{
		"0001-auth.md": "# Use OAuth for authentication\n\nContext. Decision. Why.\n",
	})
	if got := validateStrings(t, root2, "p"); msgsContain(got, "does not cite governing ADR") {
		t.Errorf("absent design.md must be silent: %v", got)
	}
}
