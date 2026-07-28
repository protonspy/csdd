package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestADRDisciplineInstalledSkills asserts the M2 template surface: the triple
// gate and grill live in the prd skill, the light gate in quick-prd, the adr:
// token in the plan template, and the decision moment in the managed CLAUDE.md
// section.
func TestADRDisciplineInstalledSkills(t *testing.T) {
	dir := freshWorkspace(t)

	prd := readFile(t, filepath.Join(dir, ".claude", "skills", "prd", "SKILL.md"))
	for _, want := range []string{
		"Hard to reverse", "Surprising without context", "real trade-off", // triple gate
		"One decision per exchange", "Recommendation first", "Dependency order", // grill rules
		"Tier 1", "Tier 2", // two-tier Draft
		"the moment it resolves", // inline ADR write
		"[DEFERRED-BY-HUMAN]",    // Present sweep
		"superseded-by",          // Revise funnel supersession
		"adr:<slug>",             // Decompose citation
	} {
		if !strings.Contains(prd, want) {
			t.Errorf("prd SKILL.md missing decision-grill element %q", want)
		}
	}

	quick := readFile(t, filepath.Join(dir, ".claude", "skills", "quick-prd", "SKILL.md"))
	for _, want := range []string{"Hard to reverse", "docs/adr/", "recorded decisions"} {
		if !strings.Contains(quick, want) {
			t.Errorf("quick-prd SKILL.md missing light-gate element %q", want)
		}
	}

	// The plan template Refs comment ships via the scaffold, not an installed file.
	if code, _, e := run(t, "plan", "init", "reftmpl", "--root", dir); code != 0 {
		t.Fatalf("plan init: %s", e)
	}
	planTmpl := readFile(t, filepath.Join(dir, "docs", "plans", "reftmpl", "plan.md"))
	if !strings.Contains(planTmpl, "adr:<slug>") {
		t.Errorf("plan template Refs comment should document adr:<slug>")
	}

	claudemd := readFile(t, filepath.Join(dir, "CLAUDE.md"))
	for _, want := range []string{"docs/adr/", "triple gate", "Never decide silently", "superseded"} {
		if !strings.Contains(claudemd, want) {
			t.Errorf("CLAUDE.md managed section missing decision moment %q", want)
		}
	}
}

const adrCitePlan = `---
name: photos
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest photos | — | M1 | | adr:pick-store |

## Quality Gates

- verify: make check
`

// TestADRValidateE2E drives the decision-ref lint through the real CLI: a plan
// citing a present ADR validates clean; broken, ambiguous, and cites-superseded
// variants each fail with exit 2; and the brief lists the cited record as a short
// ref (it no longer inlines the body — the gate enforces citation instead).
func TestADRValidateE2E(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "plan", "init", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan init: %s", e)
	}
	mustWrite(t, filepath.Join(dir, "docs", "plans", "photos", "plan.md"), adrCitePlan)
	adrPath := func(name string) string { return filepath.Join(dir, "docs", "adr", name) }

	// Clean citation.
	mustWrite(t, adrPath("0001-pick-store.md"), "# Store under docs/adr\n\nBODY_TOKEN: append-only.\n")
	if code, _, e := run(t, "plan", "validate", "photos", "--root", dir); code != 0 {
		t.Fatalf("plan citing a present ADR should validate clean: %s", e)
	}

	// The brief lists the governing ADR as a short ref and directs fetching the
	// body via the graph; it must NOT inline the ADR title or body. The context pass
	// is off so the test never spawns a `claude` — it now runs by default.
	code, out, _ := run(t, "plan", "brief", "photos", "--feat", "upload", "--root", dir, "--enrich-model", "none")
	if code != 0 {
		t.Fatalf("plan brief failed: %d", code)
	}
	if !strings.Contains(out, "adr:pick-store") {
		t.Errorf("brief should list the governing ADR ref:\n%s", out)
	}
	if !strings.Contains(out, "csdd graph explain adr:") {
		t.Errorf("brief should direct the session to fetch the ADR via graph explain:\n%s", out)
	}
	if strings.Contains(out, "BODY_TOKEN") {
		t.Errorf("brief must NOT inline the ADR body (the gate enforces citation now):\n%s", out)
	}

	// Broken citation: rewrite the ADR to a different slug.
	mustWrite(t, adrPath("0001-pick-store.md"), "# renamed\n\nbody\n") // still slug pick-store
	mustWrite(t, filepath.Join(dir, "docs", "plans", "photos", "plan.md"),
		strings.Replace(adrCitePlan, "adr:pick-store", "adr:ghost", 1))
	if code, _, _ := run(t, "plan", "validate", "photos", "--root", dir); code != 2 {
		t.Errorf("broken decision ref should fail validate (exit 2), got %d", code)
	}

	// Ambiguous citation: two records share the pick-store slug. Findings print to
	// stderr, so assert against errOut.
	mustWrite(t, filepath.Join(dir, "docs", "plans", "photos", "plan.md"), adrCitePlan)
	mustWrite(t, adrPath("0001-pick-store.md"), "# one\n\nbody\n")
	mustWrite(t, adrPath("0005-pick-store.md"), "# five\n\nbody\n")
	if code, _, errOut := run(t, "plan", "validate", "photos", "--root", dir); code != 2 || !strings.Contains(errOut, "ambiguous decision ref") {
		t.Errorf("ambiguous decision ref should fail validate; code=%d err=%s", code, errOut)
	}

	// Cites-superseded: drop the duplicate so the slug resolves uniquely to 0001,
	// then mark 0001 superseded by a successor.
	if err := os.Remove(adrPath("0005-pick-store.md")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, adrPath("0001-pick-store.md"),
		"---\nstatus: superseded\nsuperseded-by: 0002\n---\n# old\n\nbody\n")
	mustWrite(t, adrPath("0002-pick-store-v2.md"), "# new\n\nbody\n")
	code, _, errOut := run(t, "plan", "validate", "photos", "--root", dir)
	if code != 2 || !strings.Contains(errOut, "cites superseded") {
		t.Errorf("citing a superseded ADR should fail validate with a cites-superseded finding; code=%d err=%s", code, errOut)
	}
}
