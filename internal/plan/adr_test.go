package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/validator"
)

// writeADRs lays down docs/adr/<name> files under root.
func writeADRs(t *testing.T, root string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "docs", "adr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanADRsParsesRecords(t *testing.T) {
	root := t.TempDir()
	writeADRs(t, root, map[string]string{
		"0001-store-under-docs-adr.md": "# Store decision records under docs/adr/\n\nContext. Decision. Why.\n",
		"0002-two-tier-interview.md": `---
status: superseded
superseded-by: 0003
---
# Two-tier interview

Batch facts, grill decisions.
`,
		"0003-two-tier-refined.md": "# The refined interview\n\nSupersedes 0002.\n",
	})
	s := ScanADRs(root)
	if !s.Present {
		t.Fatal("expected Present")
	}
	if len(s.All) != 3 {
		t.Fatalf("got %d records, want 3", len(s.All))
	}
	if len(s.issuesResolved()) != 0 {
		t.Errorf("expected no well-formedness issues, got %+v", s.issuesResolved())
	}

	a1, res := s.Resolve("store-under-docs-adr")
	if res != ADRResolved {
		t.Fatalf("0001 resolution = %v", res)
	}
	if a1.Number != 1 || a1.Status != ADRStatusAccepted {
		t.Errorf("0001 = %+v", a1)
	}
	if a1.Title != "Store decision records under docs/adr/" {
		t.Errorf("0001 title = %q", a1.Title)
	}
	if !strings.Contains(a1.Body, "Context. Decision. Why.") {
		t.Errorf("0001 body = %q", a1.Body)
	}

	a2, _ := s.Resolve("two-tier-interview")
	if a2.Status != ADRStatusSuperseded || a2.SupersededBy != 3 {
		t.Errorf("0002 = %+v", a2)
	}
}

func TestScanADRsAbsentIsSilent(t *testing.T) {
	s := ScanADRs(t.TempDir())
	if s.Present {
		t.Error("absent docs/adr should not be Present")
	}
	if len(s.All) != 0 || len(s.issuesResolved()) != 0 {
		t.Error("absent docs/adr should yield nothing")
	}
}

func TestScanADRsWellFormedness(t *testing.T) {
	root := t.TempDir()
	writeADRs(t, root, map[string]string{
		"0001-good.md":       "# Good\n\nbody\n",
		"0001-dup-number.md": "# Dup number\n\nbody\n",                             // duplicate number 0001
		"no-number.md":       "# Bad filename\n\nbody\n",                           // malformed filename
		"0007-no-title.md":   "just prose, no heading\n",                           // missing title
		"0008-dangles.md":    "---\nsuperseded-by: 0099\n---\n# Dangles\n\nbody\n", // dangling supersession
		"README.md":          "# Index\n\nnot an ADR\n",                            // ignored
	})
	s := ScanADRs(root)
	msgs := map[string]bool{}
	for _, i := range s.issuesResolved() {
		msgs[i.msg] = true
	}
	want := []string{
		"malformed ADR filename",
		"duplicate ADR number 0001",
		"ADR has no '# <title>' heading",
		"dangling supersession: superseded-by 0099",
	}
	for _, w := range want {
		found := false
		for m := range msgs {
			if strings.Contains(m, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing well-formedness issue containing %q; got %+v", w, s.issuesResolved())
		}
	}
	// README.md must never be flagged as malformed.
	for m := range msgs {
		if strings.Contains(m, "README") {
			t.Errorf("README.md should be ignored, not flagged: %q", m)
		}
	}
}

func TestScanADRsAmbiguousSlug(t *testing.T) {
	root := t.TempDir()
	writeADRs(t, root, map[string]string{
		"0002-shared.md": "# Two\n\nbody\n",
		"0005-shared.md": "# Five\n\nbody\n",
	})
	s := ScanADRs(root)
	if _, res := s.Resolve("shared"); res != ADRAmbiguous {
		t.Errorf("shared slug resolution = %v, want ambiguous", res)
	}
	if _, res := s.Resolve("nope"); res != ADRMissing {
		t.Errorf("unknown slug resolution = %v, want missing", res)
	}
}

// A plan citing ADRs is exercised end-to-end through ValidatePlan.
func adrPlanWorkspace(t *testing.T, refs string) (root string, doc *PlanDoc) {
	t.Helper()
	root = t.TempDir()
	writeADRs(t, root, map[string]string{
		"0001-accepted.md": "# Accepted decision\n\nWe chose X because Y.\n",
		"0002-old.md":      "---\nstatus: superseded\nsuperseded-by: 0003\n---\n# Old decision\n\nreplaced\n",
		"0003-new.md":      "# New decision\n\nthe successor\n",
		"0004-twin.md":     "# Twin A\n\nbody\n",
		"0009-twin-dup.md": "# Twin B\n\nbody\n", // NOTE: different slug, so not ambiguous
	})
	// Add a genuine ambiguity: two files sharing slug "dupe".
	writeADRs(t, root, map[string]string{
		"0006-dupe.md": "# Dupe six\n\nbody\n",
		"0007-dupe.md": "# Dupe seven\n\nbody\n",
	})
	src := "---\nname: demo\nstatus: draft\n---\n" +
		"## Feats\n\n" +
		"| # | Feat | Objective | Depends | Milestone | (P) | Refs |\n" +
		"|---|------|-----------|---------|-----------|-----|------|\n" +
		"| 1 | thing | do it | — | M1 | | " + refs + " |\n\n" +
		"## Quality Gates\n\n- verify: make check\n"
	doc = Parse(src)
	doc.Slug = "demo"
	return root, doc
}

func TestValidateADRRefs(t *testing.T) {
	cases := []struct {
		name    string
		refs    string
		wantSub string
	}{
		{"broken", "adr:ghost", "broken decision ref 'adr:ghost'"},
		{"ambiguous", "adr:dupe", "ambiguous decision ref 'adr:dupe'"},
		{"malformed", "adr:Not_Kebab", "malformed decision ref 'adr:Not_Kebab'"},
		{"empty", "adr:", "malformed decision ref 'adr:(empty)'"},
		{"cites-superseded", "adr:old", "cites superseded decision 'adr:old'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, doc := adrPlanWorkspace(t, tc.refs)
			issues := ValidatePlan(doc, root)
			found := false
			for _, i := range issues {
				if strings.Contains(i.Msg, tc.wantSub) {
					found = true
				}
			}
			if !found {
				t.Errorf("want an issue containing %q; got:\n%s", tc.wantSub, joinIssues(issues))
			}
		})
	}
}

func TestValidateADRCleanCitation(t *testing.T) {
	root, doc := adrPlanWorkspace(t, "adr:accepted adr:new")
	for _, i := range ValidatePlan(doc, root) {
		if strings.Contains(i.Msg, "decision ref") || strings.Contains(i.Msg, "superseded") {
			t.Errorf("clean citation raised a decision finding: %s", i.Msg)
		}
	}
}

const adrBriefPlan = `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | thing | Do the thing | — | M1 | | adr:pick-store adr:ghost-ref |

## Quality Gates

- verify: make check
`

func TestBriefInlinesADRs(t *testing.T) {
	root := setupWorkspace(t, "p", adrBriefPlan)
	writeADRs(t, root, map[string]string{
		"0001-pick-store.md": "# We store records under docs/adr\n\nDECISION_BODY_TOKEN: append-only, project-scoped.\n",
	})
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	feat, ok := doc.Feat("thing")
	if !ok {
		t.Fatal("feat thing not found")
	}
	out, err := FeatBrief(root, doc, feat)
	if err != nil {
		t.Fatal(err)
	}
	// Determinism.
	if out2, _ := FeatBrief(root, doc, feat); out != out2 {
		t.Errorf("ADR brief is not byte-deterministic")
	}
	// The resolved ADR is inlined in full (title + body).
	if !strings.Contains(out, "We store records under docs/adr") {
		t.Errorf("brief should inline the ADR title:\n%s", out)
	}
	if !strings.Contains(out, "DECISION_BODY_TOKEN") {
		t.Errorf("brief should inline the ADR body in full (unlike wiki):\n%s", out)
	}
	// A broken ADR ref is a WARNING, not a hard omission.
	if !strings.Contains(out, "adr:ghost-ref") || !strings.Contains(out, "WARNING") {
		t.Errorf("brief should warn on the unresolved ADR ref:\n%s", out)
	}
	// The mission tells the session to record any technology / hard-to-reverse
	// trade-off it makes (stack row + ADR), never adopt one silently.
	if !strings.Contains(out, "docs/stack.md Decided row") {
		t.Errorf("brief should carry the record-your-decisions mission line:\n%s", out)
	}
}

func joinIssues(issues []validator.Issue) string {
	var b strings.Builder
	for _, i := range issues {
		b.WriteString("  - ")
		b.WriteString(i.Msg)
		b.WriteByte('\n')
	}
	return b.String()
}
