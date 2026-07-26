package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStackContractAndLints(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"docs/stack.md": `# Stack
## Decided
| Domain | Choice | Version | Why | Refs |
|---|---|---|---|---|
| Router | chi | v5 | lightweight | [notes](docs/wiki/pages/chi.md) |
| Cache | redis |  | speed |  |

## Rules
The contract is law.

## Open questions
- none
`,
		"go.mod": `module example.com/p

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.0
	github.com/lib/pq v1.10.0 // indirect
)

require github.com/spf13/cobra v1.8.0
`,
	}
	for rel, c := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ex := []Extractor{&stackExtractor{}, &wikiExtractor{}}
	g, err := BuildWith(dir, ex)
	if err != nil {
		t.Fatal(err)
	}

	// Contract rows → tech nodes (R17.2).
	chi := nodeByID(g, techID("chi"))
	if chi == nil || chi.Attrs["from_contract"] != true {
		t.Fatalf("chi contract node missing/incorrect: %+v", chi)
	}
	// Manifest dep chi → uses_tech from the go.mod code_ref (R17.2).
	if !hasEdge(g, codeRefID("go.mod"), RelUsesTech, techID("chi")) {
		t.Errorf("missing uses_tech go.mod → chi")
	}

	report := Analyze(g, dir)
	kinds := map[string][]string{}
	for _, f := range report.Findings {
		kinds[f.Kind] = append(kinds[f.Kind], f.Label)
	}

	// cobra is a direct dep not in the contract → undeclared (R17.3).
	if !containsLabel(kinds[kindUndeclaredTech], "github.com/spf13/cobra") {
		t.Errorf("expected undeclared cobra; got %v", kinds[kindUndeclaredTech])
	}
	// pq is `// indirect` and skipped, so it must NOT be flagged undeclared.
	if containsLabel(kinds[kindUndeclaredTech], "github.com/lib/pq") {
		t.Errorf("indirect dep pq should be skipped")
	}
	// redis is declared but unused → phantom; and missing Version+Refs → unrefined.
	if len(kinds[kindPhantomTech]) == 0 {
		t.Errorf("expected phantom tech (redis)")
	}
	if len(kinds[kindUnrefinedTech]) == 0 {
		t.Errorf("expected unrefined tech (redis)")
	}
	// chi is declared AND used with version+refs → clean (no findings about it).
	for _, k := range []string{kindUndeclaredTech, kindPhantomTech, kindUnrefinedTech} {
		if containsLabel(kinds[k], "chi") {
			t.Errorf("chi should be clean; flagged as %s", k)
		}
	}
	// Contract present → no no_tech_contract finding.
	if len(kinds[kindNoTechContract]) != 0 {
		t.Errorf("stack.md present; no_tech_contract must not fire")
	}
}

// TestScopedNpmPackagesDistinct guards against the react vs @monaco-editor/react
// collision that made package.json indexing non-deterministic: scoped packages
// must key distinctly from a same-named bare package, and repeated builds must be
// byte-identical.
func TestScopedNpmPackagesDistinct(t *testing.T) {
	if techID("react") == techID("@monaco-editor/react") {
		t.Fatalf("scoped and bare package must not share a tech ID")
	}
	dir := t.TempDir()
	pkg := `{"dependencies":{"react":"^18","@monaco-editor/react":"^4","react-dom":"^18"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := []Extractor{&stackExtractor{}}
	var first string
	for i := 0; i < 8; i++ {
		g, err := BuildWith(dir, ex)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := Marshal(g)
		if i == 0 {
			first = string(data)
			// All three packages become distinct tech nodes.
			for _, id := range []string{techID("react"), techID("@monaco-editor/react"), techID("react-dom")} {
				if nodeByID(g, id) == nil {
					t.Errorf("missing tech node %s", id)
				}
			}
			continue
		}
		if string(data) != first {
			t.Fatalf("package.json indexing not deterministic across builds")
		}
	}
}

func containsLabel(list []string, want string) bool {
	for _, l := range list {
		if l == want {
			return true
		}
	}
	return false
}

// TestParsePyprojectExtrasDoNotCloseTheArray is regression cover for a silent
// truncation. A PEP 508 extra spells its brackets inside the quoted requirement,
// and treating that `]` as the end of the dependencies array dropped every
// dependency declared below it — 50-odd libraries in a real backend manifest,
// each of which then had its docs/stack.md row reported as phantom tech.
func TestParsePyprojectExtrasDoNotCloseTheArray(t *testing.T) {
	manifest := []byte(`[project]
name = "backend"
dependencies = [
    "celery[librabbitmq,gevent]>=5.6.1",
    "gevent>=25.4.1",
    "fastapi>=0.128.0",
    "audioop-lts",
    "bcrypt>=4.2.0",  # Direct bcrypt usage (no passlib)
]

[tool.ruff]
line-length = 100
`)
	got := parsePyproject(manifest)
	want := []string{"celery", "gevent", "fastapi", "audioop-lts", "bcrypt"}
	if len(got) != len(want) {
		t.Fatalf("parsePyproject returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parsePyproject returned %v, want %v", got, want)
		}
	}
}

// TestParsePyprojectStopsAtTheRealArrayEnd proves the fix did not make the scan
// run on: content after the closing bracket must not be read as dependencies.
func TestParsePyprojectStopsAtTheRealArrayEnd(t *testing.T) {
	manifest := []byte(`[project]
dependencies = ["fastapi>=0.1"]

[tool.uv]
constraint-dependencies = ["not-a-dependency==9"]
`)
	got := parsePyproject(manifest)
	for _, g := range got {
		if g == "not-a-dependency" {
			t.Errorf("parsePyproject read past the array end: %v", got)
		}
	}
	if len(got) != 1 || got[0] != "fastapi" {
		t.Errorf("parsePyproject = %v, want [fastapi]", got)
	}
}

// TestStripQuotedSpans asserts the property the bracket matcher depends on —
// delimiters inside a string disappear, structural ones survive — rather than an
// exact run of spaces, which would break on any unrelated edit to the fixture.
func TestStripQuotedSpans(t *testing.T) {
	hidden := []string{
		`"celery[a,b]>=5.6.1",`,
		`'x]y'`,
		`"a" "b]c"`,
	}
	for _, in := range hidden {
		if got := stripQuotedSpans(in); strings.ContainsAny(got, "[]") {
			t.Errorf("stripQuotedSpans(%q) = %q; brackets inside quotes must not survive", in, got)
		}
	}
	kept := []string{`dependencies = [`, `]`, `'x]y' ]`}
	for _, in := range kept {
		got := stripQuotedSpans(in)
		if !strings.ContainsAny(got, "[]") {
			t.Errorf("stripQuotedSpans(%q) = %q; structural brackets must survive", in, got)
		}
		if len(got) != len(in) {
			t.Errorf("stripQuotedSpans(%q) changed length %d -> %d; offsets must be preserved", in, len(in), len(got))
		}
	}
}

// TestStackContractExternalBlock covers the escape hatch for decisions no manifest
// declares. Without it the phantom lint reported managed services and models
// forever, and the author's only remedies were deleting a true decision or
// switching the gate off — which is what a real workspace did.
func TestStackContractExternalBlock(t *testing.T) {
	doc := []byte(`# Tech contract

## Decided

| Domain | Choice | Version | Why | Refs |
|---|---|---|---|---|
| Backend · HTTP | fastapi | 0.128.0 | routes | [x](wiki/pages/x.md) |

### External services

| Domain | Choice | Version | Why | Refs |
|---|---|---|---|---|
| Database | PostgreSQL | 18 | rows | [y](wiki/pages/y.md) |
| Messaging | Telegram Bot API | 10.1 | chat | [z](wiki/pages/z.md) |

## Rules

1. Not a table.
`)
	frags := stackContract(Source{Path: "docs/stack.md", Content: doc})
	got := map[string]bool{}
	for _, f := range frags {
		for _, n := range f.Nodes {
			got[n.Label] = asBool(n.Attrs["external"])
		}
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 contract rows, got %d: %v", len(got), got)
	}
	if got["fastapi"] {
		t.Error("a row above the external heading must stay diffed against the manifests")
	}
	for _, label := range []string{"PostgreSQL", "Telegram Bot API"} {
		if !got[label] {
			t.Errorf("%q sits under the external heading and must be marked external", label)
		}
	}
}

// TestIsExternalBlockHeading pins the recognized spellings. A heading the parser
// does not recognize leaves its rows linted, so the set is closed and documented
// rather than guessed at.
func TestIsExternalBlockHeading(t *testing.T) {
	for _, yes := range []string{
		"External services", "external", "Services", "Infrastructure",
		"Managed services", "Hosted APIs", "  external SERVICES  ",
	} {
		if !isExternalBlockHeading(yes) {
			t.Errorf("%q should open the external block", yes)
		}
	}
	for _, no := range []string{"Frontend", "Backend", "Notes", "Open questions", ""} {
		if isExternalBlockHeading(no) {
			t.Errorf("%q must not open the external block", no)
		}
	}
}

// TestParsePyprojectDependencyGroups covers PEP 735 groups and PEP 621 extras.
// uv writes dev tooling into [dependency-groups] by default, and reading only the
// PEP 621 array left those packages invisible — so their contract rows reported as
// phantom tech for dependencies the project really declares.
func TestParsePyprojectDependencyGroups(t *testing.T) {
	manifest := []byte(`[project]
dependencies = ["fastapi>=0.128.0"]

[project.optional-dependencies]
media = ["pillow>=11"]

[dependency-groups]
dev = [
    "rich>=14.2.0",
    "pytest-cov>=7.0.0",
    {include-group = "test"},
]
test = ["pytest>=8"]

[tool.ruff]
line-length = 100
`)
	got := map[string]bool{}
	for _, d := range parsePyproject(manifest) {
		got[d] = true
	}
	for _, want := range []string{"fastapi", "pillow", "rich", "pytest-cov", "pytest"} {
		if !got[want] {
			t.Errorf("parsePyproject missed %q; got %v", want, got)
		}
	}
	// An include-group reference names a group, not a package.
	if got["test"] {
		t.Errorf("include-group reference must not be read as a dependency; got %v", got)
	}
	// And the scan must still stop at the table boundary.
	if got["line-length"] || got["100"] {
		t.Errorf("parsePyproject read past [dependency-groups]; got %v", got)
	}
}
