package graph

import (
	"os"
	"path/filepath"
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
