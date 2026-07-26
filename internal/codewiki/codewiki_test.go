package codewiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// soundDoc is a minimal but complete codewiki document over the fixture repo
// built by fixtureRepo. Tests mutate copies of it to produce each fault.
const soundDoc = `<!-- csdd-codewiki v1 | acme/widget | src: docs/raw/widget | 2026-07-26T06:23:44Z | 4 sections -->

# acme/widget — Codewiki

## Structure

├── 1 Overview
├── 2 Architecture
│   └── 2.1 Storage Layer
└── 3 Glossary

## Contents

<<< SECTION: 1 Overview [1-overview] >>>

# Overview

<details>
<summary>Relevant source files</summary>

- [main.go](main.go)
- [logo.png](logo.png)

</details>

Widget is a thing [main.go:1-5]().

**Sources:** [main.go:1-5]()

<<< SECTION: 2 Architecture [2-architecture] >>>

# Architecture

` + "```mermaid" + `
graph TD
    A["main() entry<br/>[main.go:3]"] --> B["store.Put<br/>[store.go:2]"]
` + "```" + `

Sources: [store.go:1-4]()

<<< SECTION: 2.1 Storage Layer [2-1-storage-layer] >>>

# Storage Layer

The store writes [store.go:2]().

<<< SECTION: 3 Glossary [3-glossary] >>>

# Glossary

**Widget** — the thing. [main.go:5]()
`

// fixtureRepo lays down the checkout the document above cites: main.go with 5
// lines and a trailing newline, store.go with 4 and none, and a binary file.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "docs", "raw", "widget")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() {\n\tprintln(1)\n}\n") // 5 lines, trailing newline
	write("store.go", "package main\n\nfunc Put() {}\n// end")           // 4 lines, no trailing newline
	write("logo.png", "\x89PNG\r\n\x1a\n\x00\x00binary")
	return dir
}

// writeDoc drops a document into the fixture workspace's dropzone and returns
// its path.
func writeDoc(t *testing.T, root, body string) string {
	t.Helper()
	p := filepath.Join(root, "docs", "raw", "acme-widget.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func lintDoc(t *testing.T, body string) ([]Finding, string) {
	t.Helper()
	root := fixtureRepo(t)
	doc := writeDoc(t, root, body)
	repo := ResolveRepo(root, doc, "", Parse(body).Header)
	findings, err := Lint(doc, repo)
	if err != nil {
		t.Fatal(err)
	}
	return findings, root
}

// kinds collapses findings to their kinds for terse assertions.
func kinds(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

func hasKind(findings []Finding, kind string) bool {
	for _, f := range findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func findingWith(t *testing.T, findings []Finding, kind, substr string) Finding {
	t.Helper()
	for _, f := range findings {
		if f.Kind == kind && strings.Contains(f.Message, substr) {
			return f
		}
	}
	t.Fatalf("no %s finding containing %q; got %v", kind, substr, findings)
	return Finding{}
}

func TestSoundDocumentHasNoFindings(t *testing.T) {
	findings, _ := lintDoc(t, soundDoc)
	if len(findings) != 0 {
		t.Fatalf("a sound document must lint clean, got %v", findings)
	}
	if Faults(findings) != 0 {
		t.Errorf("Faults() = %d, want 0", Faults(findings))
	}
}

func TestParseHeader(t *testing.T) {
	h := Parse(soundDoc).Header
	if !h.Present {
		t.Fatal("header not detected")
	}
	if h.Tool != "csdd-codewiki v1" || h.Repo != "acme/widget" || h.Src != "docs/raw/widget" || h.Count != 4 {
		t.Errorf("header parsed wrong: %+v", h)
	}
	// An externally generated document carries no src: field and may say "pages"
	// rather than "sections". It must still parse, or the lint is useless on
	// anything this workspace did not author — which is why header fields are
	// identified by shape rather than by position.
	ext := Parse("<!-- somegen v0.2.3 | acme/gadget | 2026-07-26T06:23:44Z | 24 pages -->\n").Header
	if !ext.Present || ext.Repo != "acme/gadget" || ext.Count != 24 || ext.Src != "" {
		t.Errorf("external header parsed wrong: %+v", ext)
	}
}

func TestParseSectionsAndCitations(t *testing.T) {
	doc := Parse(soundDoc)
	if len(doc.Sections) != 4 {
		t.Fatalf("got %d sections, want 4", len(doc.Sections))
	}
	if len(doc.Outline) != 4 {
		t.Fatalf("got %d outline entries, want 4", len(doc.Outline))
	}
	sub := doc.Sections[2]
	if sub.Number != "2.1" || sub.Title != "Storage Layer" || sub.Slug != "2-1-storage-layer" {
		t.Errorf("subsection parsed wrong: %+v", sub)
	}
	// The Architecture section's mermaid labels carry [main.go:3] and
	// [store.go:2] without the empty parens. Only the real citation counts.
	arch := doc.Sections[1]
	if len(arch.Citations) != 1 || arch.Citations[0].Path != "store.go" {
		t.Errorf("mermaid node labels must not parse as citations, got %+v", arch.Citations)
	}
	// The <details> block lists both a source file and an asset.
	if len(doc.Sections[0].Files) != 2 {
		t.Errorf("source files parsed wrong: %+v", doc.Sections[0].Files)
	}
}

// Real exports mix citation dialects freely, sometimes within one paragraph.
// Every one of them is a checkable claim about the code; a dialect the parser
// drops silently becomes a section that appears to cite nothing.
func TestCitationDialects(t *testing.T) {
	cite := func(body string) []Citation {
		doc := Parse("<<< SECTION: 1 X [1-x] >>>\n" + body + "\n")
		if len(doc.Sections) != 1 {
			t.Fatalf("fixture parsed into %d sections", len(doc.Sections))
		}
		return doc.Sections[0].Citations
	}
	type want struct {
		path       string
		start, end int
		whole      bool
	}
	for body, wants := range map[string][]want{
		"plain [main.go:1-5]().":            {{path: "main.go", start: 1, end: 5}},
		"single line [main.go:7]().":        {{path: "main.go", start: 7}},
		"code span [`a/b.go:1-5`]().":       {{path: "a/b.go", start: 1, end: 5}},
		"double bracket [[a/b.go:1-5]]().":  {{path: "a/b.go", start: 1, end: 5}},
		"whole file [pkg/registry.go]().":   {{path: "pkg/registry.go", whole: true}},
		"commas [beat.py:34,505]().":        {{path: "beat.py", start: 34}, {path: "beat.py", start: 505}},
		"comma space [tox.ini:6, 51-60]().": {{path: "tox.ini", start: 6}, {path: "tox.ini", start: 51, end: 60}},
	} {
		got := cite(body)
		if len(got) != len(wants) {
			t.Errorf("%q parsed %d citations, want %d (%+v)", body, len(got), len(wants), got)
			continue
		}
		for i, w := range wants {
			if got[i].Path != w.path || got[i].Start != w.start || got[i].End != w.end || got[i].WholeFile != w.whole {
				t.Errorf("%q citation %d = %+v, want %+v", body, i, got[i], w)
			}
		}
	}

	// Section cross-references share the empty-target form but assert nothing
	// about code, so they must never be counted — a document whose only "cites"
	// are links to its own pages cites nothing.
	for _, body := range []string{
		"see [2.1]().",
		"see [3.4]() and [5.11]().",
		"see [Event System (8.3)]().",
		"see [5.1 Worker Architecture and Bootsteps]().",
		"see [User Interface]() for more.",
	} {
		if got := cite(body); len(got) != 0 {
			t.Errorf("%q must not parse as a citation, got %+v", body, got)
		}
	}
}

func TestWholeFileCitationIsExistenceChecked(t *testing.T) {
	// It names no range, so only the file's existence is a checkable claim.
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[store.go]()", 1))
	if hasKind(findings, KindCitation) {
		t.Errorf("a whole-file citation to an existing file must pass, got %v", findings)
	}
	findings, _ = lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[stoer.go]()", 1))
	findingWith(t, findings, KindCitation, "no such file in the checkout: stoer.go")
}

func TestEachRangeOfAMultiRangeCitationIsChecked(t *testing.T) {
	// store.go has 4 lines: the first range fits, the second does not.
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[store.go:1-2, 9-12]()", 1))
	findingWith(t, findings, KindCitation, "store.go has 4 line(s)")
	if n := len(findings); n != 1 {
		t.Errorf("only the bad range should fault, got %d findings: %v", n, findings)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"3.1 CLI Layer":                  "3-1-cli-layer",
		"7 Configuration and Automation": "7-configuration-and-automation",
		"  Spaced  ":                     "spaced",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMissingHeaderIsReported(t *testing.T) {
	body := strings.SplitN(soundDoc, "\n", 2)[1]
	findings, _ := lintDoc(t, body)
	f := findingWith(t, findings, KindHeader, "no provenance header")
	if f.Line != 1 {
		t.Errorf("header finding must anchor to line 1, got %d", f.Line)
	}
	// A document with no header names neither its repo nor its src:, so there is
	// nothing left to resolve the checkout from. The reference checks are then
	// skipped — and say so, rather than passing quietly.
	if !hasKind(findings, KindRepo) {
		t.Errorf("an unresolvable checkout must be reported, got %v", kinds(findings))
	}
}

func TestHeaderSectionCountMustMatch(t *testing.T) {
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "| 4 sections", "| 9 sections", 1))
	findingWith(t, findings, KindHeader, "declares 9 sections, document has 4")
	// The count covers every section, subsections included — that is what a
	// external generator means by "N pages".
	if n := len(Parse(soundDoc).Sections); n != 4 {
		t.Errorf("subsections must count toward the section total, got %d", n)
	}
}

func TestStructureAndSectionsMustAgree(t *testing.T) {
	// A section listed in the tree but never written.
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "└── 3 Glossary", "├── 3 Glossary\n└── 4 Appendix", 1))
	findingWith(t, findings, KindStructure, `Structure lists 4 "Appendix"`)

	// A section written but never listed.
	findings, _ = lintDoc(t, soundDoc+"\n<<< SECTION: 4 Appendix [4-appendix] >>>\n\nExtra [main.go:1]().\n")
	findingWith(t, findings, KindStructure, "is not listed in the Structure tree")

	// Titles that drifted apart between tree and body.
	findings, _ = lintDoc(t, strings.Replace(soundDoc, "├── 2 Architecture", "├── 2 Design", 1))
	findingWith(t, findings, KindStructure, `titled "Architecture" here and "Design" in the Structure tree`)
}

func TestOrphanSubsectionIsReported(t *testing.T) {
	body := strings.Replace(soundDoc, "│   └── 2.1 Storage Layer", "│   └── 5.1 Storage Layer", 1)
	body = strings.Replace(body, "<<< SECTION: 2.1 Storage Layer [2-1-storage-layer] >>>", "<<< SECTION: 5.1 Storage Layer [5-1-storage-layer] >>>", 1)
	findings, _ := lintDoc(t, body)
	findingWith(t, findings, KindStructure, "section 5.1 has no parent section 5")
}

func TestSlugFaults(t *testing.T) {
	// A slug that no longer derives from its heading points at the wrong body.
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[2-1-storage-layer]", "[2-1-storage]", 1))
	findingWith(t, findings, KindSlug, `does not match its heading (expected "2-1-storage-layer")`)

	// Two sections addressed by the same slug.
	body := strings.Replace(soundDoc, "<<< SECTION: 3 Glossary [3-glossary] >>>", "<<< SECTION: 3 Glossary [1-overview] >>>", 1)
	findings, _ = lintDoc(t, body)
	findingWith(t, findings, KindSlug, `slug "1-overview" is used twice`)
}

func TestSectionWithNoCitationIsReported(t *testing.T) {
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "The store writes [store.go:2]().", "The store writes things.", 1))
	findingWith(t, findings, KindCoverage, `section 2.1 "Storage Layer" cites no source`)
}

func TestCitationMustResolveInTheCheckout(t *testing.T) {
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[stoer.go:1-4]()", 1))
	f := findingWith(t, findings, KindCitation, "no such file in the checkout: stoer.go")
	if f.File != "stoer.go" {
		t.Errorf("finding must carry the cited path, got %q", f.File)
	}
}

func TestCitationMustStayInsideTheCheckout(t *testing.T) {
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[../../../etc/passwd:1-4]()", 1))
	findingWith(t, findings, KindCitation, "escapes the checkout")
}

func TestCitationRangeMustFitTheFile(t *testing.T) {
	// store.go has 4 lines and no trailing newline: 1-5 is a real overrun.
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[store.go:1-5]()", 1))
	findingWith(t, findings, KindCitation, "store.go has 4 line(s)")

	// main.go has 5 lines and a trailing newline, so the empty line 6 that
	// split("\n")-based counters number is tolerated — but line 7 is not.
	findings, _ = lintDoc(t, strings.Replace(soundDoc, "[main.go:1-5]()", "[main.go:1-6]()", 1))
	if hasKind(findings, KindCitation) {
		t.Errorf("end == lines+1 on a newline-terminated file must be tolerated, got %v", findings)
	}
	findings, _ = lintDoc(t, strings.Replace(soundDoc, "[main.go:1-5]()", "[main.go:1-7]()", 1))
	findingWith(t, findings, KindCitation, "main.go has 5 line(s)")
}

func TestDegenerateCitationRanges(t *testing.T) {
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[store.go:4-2]()", 1))
	findingWith(t, findings, KindCitation, "ends before it starts")

	findings, _ = lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[store.go:0-4]()", 1))
	findingWith(t, findings, KindCitation, "lines are 1-based")
}

func TestBinaryFileIsExistenceCheckedOnly(t *testing.T) {
	// A line range into a PNG is not something this lint can judge, so it must
	// pass rather than produce a nonsense line count.
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "[store.go:1-4]()", "[logo.png:1-9999]()", 1))
	if hasKind(findings, KindCitation) {
		t.Errorf("binary citations must be existence-checked only, got %v", findings)
	}
}

func TestMissingSourceFileIsReported(t *testing.T) {
	findings, _ := lintDoc(t, strings.Replace(soundDoc, "- [main.go](main.go)", "- [gone.go](gone.go)", 1))
	findingWith(t, findings, KindSourceFile, "no such file in the checkout: gone.go")
}

func TestUnresolvedCheckoutSkipsReferenceChecksInformationally(t *testing.T) {
	findings, err := Lint(writeDoc(t, fixtureRepo(t), soundDoc), "")
	if err != nil {
		t.Fatal(err)
	}
	f := findingWith(t, findings, KindRepo, "citation and source-file checks were skipped")
	if !f.Informational {
		t.Error("a skipped-check notice must be informational, not a fault")
	}
	if Faults(findings) != 0 {
		t.Errorf("informational findings must not fail the gate, Faults() = %d", Faults(findings))
	}
}

func TestResolveRepo(t *testing.T) {
	root := fixtureRepo(t)
	doc := writeDoc(t, root, soundDoc)
	want := filepath.Join(root, "docs", "raw", "widget")

	if got := ResolveRepo(root, doc, "", Parse(soundDoc).Header); got != want {
		t.Errorf("src: header should resolve the checkout, got %q", got)
	}
	// An explicit --repo wins over the header.
	if got := ResolveRepo(root, doc, want, Header{Src: "nowhere"}); got != want {
		t.Errorf("explicit --repo must win, got %q", got)
	}
	// A --repo that is not a directory resolves to nothing rather than to a
	// silently different tree.
	if got := ResolveRepo(root, doc, "no/such/dir", Header{Src: "docs/raw/widget"}); got != "" {
		t.Errorf("a bad --repo must not fall through to the header, got %q", got)
	}
	// With no src:, the repo slug's name is tried as a sibling of the document.
	if err := os.Rename(want, filepath.Join(root, "docs", "raw", "gadget")); err != nil {
		t.Fatal(err)
	}
	got := ResolveRepo(root, doc, "", Header{Repo: "acme/gadget"})
	if got != filepath.Join(root, "docs", "raw", "gadget") {
		t.Errorf("sibling-by-repo-name resolution failed, got %q", got)
	}
	// Last resort: a directory sharing the document's own basename.
	if err := os.Rename(filepath.Join(root, "docs", "raw", "gadget"), filepath.Join(root, "docs", "raw", "acme-widget")); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(root, "docs", "raw", "acme-widget")
	if got := ResolveRepo(root, doc, "", Header{}); got != sibling {
		t.Errorf("sibling-by-document-name resolution failed, got %q", got)
	}
	// With every candidate gone, resolution yields nothing rather than a guess.
	if err := os.Rename(sibling, filepath.Join(root, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	if got := ResolveRepo(root, doc, "", Header{}); got != "" {
		t.Errorf("nothing to resolve from must yield empty, got %q", got)
	}
}

func TestDiscoverFindsOnlyProvenanceCarryingDocs(t *testing.T) {
	root := fixtureRepo(t)
	raw := filepath.Join(root, "docs", "raw")
	writeDoc(t, root, soundDoc)
	for name, body := range map[string]string{
		"README.md":    "# Just a readme\n",
		"notes.txt":    "<!-- csdd-codewiki v1 | a/b | 2026-01-01T00:00:00Z -->\n",
		"partial.md":   "<!-- some other comment -->\n",
		"other-doc.md": "<!-- csdd-codewiki v1 | acme/other | 2026-01-01T00:00:00Z | 1 sections -->\n",
	} {
		if err := os.WriteFile(filepath.Join(raw, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	found, err := Discover(raw)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range found {
		names = append(names, filepath.Base(p))
	}
	want := []string{"acme-widget.md", "other-doc.md"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("Discover() = %v, want %v", names, want)
	}
}

func TestLintReportsFindingsInDocumentOrder(t *testing.T) {
	body := strings.Replace(soundDoc, "[store.go:1-4]()", "[gone.go:1-4]()", 1)
	body = strings.Replace(body, "The store writes [store.go:2]().", "The store writes things.", 1)
	findings, _ := lintDoc(t, body)
	if len(findings) < 2 {
		t.Fatalf("expected at least two findings, got %v", findings)
	}
	for i := 1; i < len(findings); i++ {
		if findings[i].Line < findings[i-1].Line {
			t.Errorf("findings out of document order: %v", findings)
		}
	}
}

func TestLintOnMissingFileErrors(t *testing.T) {
	if _, err := Lint(filepath.Join(t.TempDir(), "nope.md"), ""); err == nil {
		t.Error("linting a nonexistent document must error")
	}
}
