package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGlossarySkillAndCommandInstalled asserts the M2 template surface: the
// glossary skill (four moves, tombstone, lazy inline write, graph-query-first,
// contract purity, gate-positive hand-off), the /glossary command, the flow
// hooks in prd/quick-prd/wiki, and the knowledge-section + CLAUDE.md moment.
func TestGlossarySkillAndCommandInstalled(t *testing.T) {
	dir := freshWorkspace(t)

	// The skill installs and passes the skill validator.
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "glossary", "SKILL.md")); err != nil {
		t.Fatalf("glossary skill not installed: %v", err)
	}
	if code, _, errOut := run(t, "skill", "validate", "glossary", "--root", dir); code != 0 {
		t.Errorf("glossary skill failed validation (code=%d): %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "commands", "glossary.md")); err != nil {
		t.Errorf("/glossary command not installed: %v", err)
	}

	gl := readFile(t, filepath.Join(dir, ".claude", "skills", "glossary", "SKILL.md"))
	for _, want := range []string{
		"Challenge", "Sharpen", "Stress-test", "Cross-reference", // four moves
		"tombstone",           // the tombstone rule
		"immediately",         // inline write
		"lazily",              // lazy creation
		"csdd graph query",    // graph-query-before-asking
		"zero implementation", // contract purity
		"decision grill",      // gate-positive hand-off
	} {
		if !strings.Contains(gl, want) {
			t.Errorf("glossary skill missing %q", want)
		}
	}

	// Flow hooks — invocation only.
	prd := readFile(t, filepath.Join(dir, ".claude", "skills", "prd", "SKILL.md"))
	if !strings.Contains(prd, "glossary skill") || !strings.Contains(prd, "canonical terms") {
		t.Errorf("prd skill missing glossary Draft/Decompose hook")
	}
	quick := readFile(t, filepath.Join(dir, ".claude", "skills", "quick-prd", "SKILL.md"))
	if !strings.Contains(quick, "glossary skill") {
		t.Errorf("quick-prd skill missing glossary hook")
	}
	wiki := readFile(t, filepath.Join(dir, ".claude", "skills", "wiki", "SKILL.md"))
	if !strings.Contains(wiki, "glossary") || !strings.Contains(wiki, "canonical terms") {
		t.Errorf("wiki skill missing glossary Ingest hook")
	}

	// Satellites.
	claudemd := readFile(t, filepath.Join(dir, "CLAUDE.md"))
	for _, want := range []string{"docs/glossary.md", "canonical term", "tombstone"} {
		if !strings.Contains(claudemd, want) {
			t.Errorf("CLAUDE.md managed section missing glossary moment %q", want)
		}
	}
}

const glossaryPlan = `---
name: shop
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | client-sync | sync the buyer records | — | M1 | | |

## Quality Gates

- verify: make check
`

const dogfoodGlossary = `## Language

**Customer**: A person or organization that buys.
_Avoid_: client, account

**Feat**: One row of a plan's Feats table.
_Avoid_: feature
`

// TestGlossaryLintE2E drives the identifier lint end to end: an avoided feat slug
// fails plan validate; an avoided wiki page name surfaces in wiki lint; term nodes
// answer graph query; and the orphan-term informational fires.
func TestGlossaryLintE2E(t *testing.T) {
	dir := freshWorkspace(t)
	mustWrite(t, filepath.Join(dir, "docs", "glossary.md"), dogfoodGlossary)

	// 1. An avoided feat slug ("client-sync" — client is avoided) fails validate.
	if code, _, e := run(t, "plan", "init", "shop", "--root", dir); code != 0 {
		t.Fatalf("plan init: %s", e)
	}
	mustWrite(t, filepath.Join(dir, "docs", "plans", "shop", "plan.md"), glossaryPlan)
	code, _, errOut := run(t, "plan", "validate", "shop", "--root", dir)
	if code != 2 || !strings.Contains(errOut, "avoided term 'client'") {
		t.Errorf("avoided feat slug should fail validate; code=%d err=%s", code, errOut)
	}

	// 2. An avoided wiki page name surfaces in wiki lint (corpus-filtered).
	mustWrite(t, filepath.Join(dir, "docs", "wiki", "pages", "account-portal.md"), "# Account Portal\n")
	code, out, _ := run(t, "wiki", "lint", "--root", dir)
	if code != 2 || !strings.Contains(out, "avoided term 'account'") {
		t.Errorf("avoided wiki page name should surface in wiki lint; code=%d out=%s", code, out)
	}

	// 3. Term nodes answer graph query.
	if code, _, e := run(t, "graph", "build", "--root", dir); code != 0 {
		t.Fatalf("graph build: %s", e)
	}
	code, out, _ = run(t, "graph", "query", "Customer", "--root", dir)
	if code != 0 || !strings.Contains(strings.ToLower(out), "customer") {
		t.Errorf("graph query should find the Customer term node; code=%d out=%s", code, out)
	}

	// 4. The orphan-term informational fires (Feat is unused here).
	code, out, _ = run(t, "graph", "analyze", "--root", dir)
	if !strings.Contains(out, "orphan term") {
		t.Errorf("expected an orphan_term finding in analyze; out=%s", out)
	}
}
