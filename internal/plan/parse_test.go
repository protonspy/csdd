package plan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const goodPlan = `---
name: photo-sharing
status: draft
---

# Photo Sharing — Plan

Some prose the parser ignores.

## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload-pipeline | Ingest and store photos | — | M1 |  | stack:go [[storage-design]] |
| 2 | thumbnailer | Generate thumbnails | upload-pipeline | M1 | P | stack:libvips |
| 3 | share-links | Signed share URLs | upload-pipeline, thumbnailer | M2 | yes | [[share-security]] [[url-signing]] |

## Quality Gates

- verify: make check
- e2e: go test ./e2e/...

## Executor Notes

Run gofmt before committing.
Never touch generated files.

## Something Unknown

Ignored entirely.
`

func TestParseFrontmatterAndSections(t *testing.T) {
	doc := Parse(goodPlan)
	if doc.Name != "photo-sharing" {
		t.Errorf("Name = %q, want photo-sharing", doc.Name)
	}
	if doc.Status != StatusDraft {
		t.Errorf("Status = %q, want draft", doc.Status)
	}
	if len(doc.grammar) != 0 {
		t.Errorf("expected no grammar issues, got %v", doc.grammar)
	}
	wantNotes := "Run gofmt before committing.\nNever touch generated files."
	if doc.ExecutorNotes != wantNotes {
		t.Errorf("ExecutorNotes = %q, want %q", doc.ExecutorNotes, wantNotes)
	}
}

func TestParseFeats(t *testing.T) {
	doc := Parse(goodPlan)
	if len(doc.Feats) != 3 {
		t.Fatalf("got %d feats, want 3", len(doc.Feats))
	}

	f0 := doc.Feats[0]
	if f0.Num != "1" || f0.Slug != "upload-pipeline" || f0.Objective != "Ingest and store photos" {
		t.Errorf("feat 0 = %+v", f0)
	}
	if len(f0.Depends) != 0 {
		t.Errorf("feat 0 Depends = %v, want none (— placeholder)", f0.Depends)
	}
	if f0.Parallel {
		t.Errorf("feat 0 should not be parallel")
	}
	if !reflect.DeepEqual(f0.StackRefs, []string{"go"}) {
		t.Errorf("feat 0 StackRefs = %v, want [go]", f0.StackRefs)
	}
	if !reflect.DeepEqual(f0.WikiRefs, []string{"storage-design"}) {
		t.Errorf("feat 0 WikiRefs = %v, want [storage-design]", f0.WikiRefs)
	}

	f1 := doc.Feats[1]
	if !f1.Parallel {
		t.Errorf("feat 1 (P)=P should be parallel")
	}
	if !reflect.DeepEqual(f1.Depends, []string{"upload-pipeline"}) {
		t.Errorf("feat 1 Depends = %v", f1.Depends)
	}

	f2 := doc.Feats[2]
	if !reflect.DeepEqual(f2.Depends, []string{"upload-pipeline", "thumbnailer"}) {
		t.Errorf("feat 2 Depends = %v, want two", f2.Depends)
	}
	if !f2.Parallel {
		t.Errorf("feat 2 (P)=yes should be parallel")
	}
	if !reflect.DeepEqual(f2.WikiRefs, []string{"share-security", "url-signing"}) {
		t.Errorf("feat 2 WikiRefs = %v", f2.WikiRefs)
	}
	if f2.Line != 16 {
		t.Errorf("feat 2 Line = %d, want 16", f2.Line)
	}
}

func TestParseGates(t *testing.T) {
	doc := Parse(goodPlan)
	want := []Gate{
		{Label: "verify", Command: "make check", Line: 20},
		{Label: "e2e", Command: "go test ./e2e/...", Line: 21},
	}
	if len(doc.Gates) != len(want) {
		t.Fatalf("got %d gates, want %d: %+v", len(doc.Gates), len(want), doc.Gates)
	}
	for i := range want {
		if doc.Gates[i].Label != want[i].Label || doc.Gates[i].Command != want[i].Command {
			t.Errorf("gate %d = %+v, want %+v", i, doc.Gates[i], want[i])
		}
	}
}

func TestParseWikiRefAlias(t *testing.T) {
	wiki, stack, adr, raw := tokenizeRefs("[[page|Alias]] [[other#Section]] stack:chi adr:pick-x bare-token")
	if !reflect.DeepEqual(wiki, []string{"page", "other"}) {
		t.Errorf("wiki = %v, want [page other]", wiki)
	}
	if !reflect.DeepEqual(stack, []string{"chi"}) {
		t.Errorf("stack = %v, want [chi]", stack)
	}
	if !reflect.DeepEqual(adr, []string{"pick-x"}) {
		t.Errorf("adr = %v, want [pick-x]", adr)
	}
	if len(raw) != 5 {
		t.Errorf("raw = %v, want 5 tokens", raw)
	}
}

// A bare "adr:" with no slug is captured as an empty-string ref so validate can
// report it malformed (R2.2), rather than being silently dropped.
func TestParseADRRefEmptySlugCaptured(t *testing.T) {
	_, _, adr, _ := tokenizeRefs("adr: adr:good-one")
	if !reflect.DeepEqual(adr, []string{"", "good-one"}) {
		t.Errorf("adr = %q, want [\"\" \"good-one\"]", adr)
	}
}

func TestParseMalformedFeatRow(t *testing.T) {
	// A row missing the Refs cell (only 6 of 7 columns) is a grammar issue but
	// the feat is still captured.
	src := `## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | lonely | too few cells | — | M1 | |
`
	doc := Parse(src)
	if len(doc.Feats) != 1 {
		t.Fatalf("expected the truncated feat to still be captured, got %d", len(doc.Feats))
	}
	if len(doc.grammar) == 0 {
		t.Errorf("expected a grammar issue for the short row")
	}
}

func TestParseMissingColumn(t *testing.T) {
	src := `## Feats

| # | Feat | Objective | Depends | Milestone | Refs |
|---|------|-----------|---------|-----------|------|
| 1 | thing | do it | — | M1 | |
`
	doc := Parse(src)
	var found bool
	for _, g := range doc.grammar {
		if g.msg == "Feats table is missing the '(p)' column" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a missing-(p)-column grammar issue, got %v", doc.grammar)
	}
}

func TestParseEmpty(t *testing.T) {
	doc := Parse("")
	if doc.Name != "" || len(doc.Feats) != 0 || len(doc.Gates) != 0 {
		t.Errorf("empty content should yield an empty doc, got %+v", doc)
	}
}

func TestHashPlanDeterministicAndSensitive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plan.md"), goodPlan)
	writeFile(t, filepath.Join(dir, "seeds", "upload-pipeline", "requirements.md"), "# seed\n")

	h1, err := HashPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hash not deterministic: %s vs %s", h1, h2)
	}

	// Editing a seed changes the hash.
	writeFile(t, filepath.Join(dir, "seeds", "upload-pipeline", "requirements.md"), "# seed edited\n")
	h3, err := HashPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Errorf("hash should change when a seed changes")
	}

	// A CRLF re-checkout of plan.md must NOT look like drift (normalized hashing).
	writeFile(t, filepath.Join(dir, "seeds", "upload-pipeline", "requirements.md"), "# seed\n")
	writeFile(t, filepath.Join(dir, "plan.md"), replaceLF(goodPlan))
	h4, err := HashPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h4 != h1 {
		t.Errorf("CRLF plan.md must hash identically to LF; got %s vs %s", h4, h1)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// replaceLF converts LF to CRLF to exercise line-ending-insensitive hashing.
func replaceLF(s string) string {
	out := make([]byte, 0, len(s)+len(s)/20)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, '\r', '\n')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
