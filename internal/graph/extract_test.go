package graph

import "testing"

// TestParseIgnoreDirPattern pins the narrow subset the parser accepts. Anything
// it is not certain about must contribute no rule, because a mis-read pattern
// silently drops a subtree from the index.
func TestParseIgnoreDirPattern(t *testing.T) {
	accepted := map[string]ignorePattern{
		".venv/":            {path: ".venv"},
		".venv":             {path: ".venv"},
		"node_modules/":     {path: "node_modules"},
		"/dist/":            {path: "dist", anchored: true},
		"internal/web/dist": {path: "internal/web/dist"},
		"  .tox/  ":         {path: ".tox"},
	}
	for line, want := range accepted {
		got, ok := parseIgnoreDirPattern(line)
		if !ok || got != want {
			t.Errorf("parseIgnoreDirPattern(%q) = %+v,%v; want %+v,true", line, got, ok, want)
		}
	}
	for _, line := range []string{
		"", "   ", "# a comment", "!keep-me/", "*.pyc", "*.py[codz]",
		"**/build/", "dir?/", "/", "..", ".",
	} {
		if got, ok := parseIgnoreDirPattern(line); ok {
			t.Errorf("parseIgnoreDirPattern(%q) should contribute no rule, got %+v", line, got)
		}
	}
}

// TestIgnoreRulesNestedAndAnchored covers the shape that made this worth
// building: the virtualenv that flooded a real workspace's tech lint is declared
// in backend/.gitignore, not the root one.
func TestIgnoreRulesNestedAndAnchored(t *testing.T) {
	ir := newIgnoreRules()
	ir.byDir["."] = []ignorePattern{{path: "dist", anchored: true}, {path: "node_modules"}}
	ir.byDir["backend"] = []ignorePattern{{path: ".venv"}}

	skipped := []string{
		"dist",                  // anchored at the root
		"node_modules",          // unanchored, at the root
		"frontend/node_modules", // unanchored, deeper
		"backend/.venv",         // declared by the nested file
		"backend/svc/.venv",     // …and below it
	}
	for _, rel := range skipped {
		if !ir.skips(rel) {
			t.Errorf("expected %q to be skipped", rel)
		}
	}
	kept := []string{
		"src",
		"frontend/dist", // the root rule was anchored: only the root's own dist
		"backend",
		".venv/nope/deep", // a rule from backend/ must not reach outside it
		"other/.venv",     // …nor apply to a sibling subtree
	}
	for _, rel := range kept {
		if ir.skips(rel) {
			t.Errorf("expected %q to be indexed", rel)
		}
	}
}

// TestIgnoreRulesNeverSkipTheCsddCorpus is the guard that makes reading
// .gitignore safe at all. csdd's own repository gitignores /specs/ and /.claude/,
// and honouring that would empty the graph of the very corpus it indexes.
func TestIgnoreRulesNeverSkipTheCsddCorpus(t *testing.T) {
	ir := newIgnoreRules()
	ir.byDir["."] = []ignorePattern{
		{path: "specs", anchored: true},
		{path: ".claude", anchored: true},
		{path: "docs", anchored: true},
	}
	for _, rel := range []string{"specs", ".claude", "docs"} {
		if ir.skips(rel) {
			t.Errorf("%q is csdd-owned corpus and must be indexed even when gitignored", rel)
		}
	}
}

// TestResolveCitedPath covers the two failures that a verbatim citation caused.
//
// NormalizeID maps every non-word character to "_" and trims, so "../../adr/x.md"
// and "adr/x.md" collapsed to one node ID while being recorded as two artifacts:
// the build reported an id collision and dropped one, along with its edges. And a
// "../" path never stats from the workspace root, so a citation of a file that
// really exists was classified as a planned reference rather than a reuse.
func TestResolveCitedPath(t *testing.T) {
	cases := []struct{ from, cited, want string }{
		// The corpus case: a plan citing an ADR two levels up.
		{"docs/plans/agency/plan.md", "../../adr/0003-x.md", "docs/adr/0003-x.md"},
		{"docs/wiki/pages/p.md", "../../adr/0003-x.md", "docs/adr/0003-x.md"},
		{"docs/plans/agency/plan.md", "./notes.md", "docs/plans/agency/notes.md"},
		// Already workspace-relative by convention — untouched.
		{"docs/plans/agency/plan.md", "docs/adr/0003-x.md", "docs/adr/0003-x.md"},
		{"specs/f/tasks.md", "internal/cli/spec.go", "internal/cli/spec.go"},
		// Climbing above the workspace names nothing this graph can model, so the
		// citation is kept as written rather than rewritten into an invented path.
		{"README.md", "../outside/x.md", "../outside/x.md"},
		{"docs/x.md", "../../../far/x.md", "../../../far/x.md"},
	}
	for _, tc := range cases {
		if got := resolveCitedPath(tc.from, tc.cited); got != tc.want {
			t.Errorf("resolveCitedPath(%q, %q) = %q, want %q", tc.from, tc.cited, got, tc.want)
		}
	}
}

// TestResolveCitedPathCollapsesSpellings is the property that removes the
// collision: two documents citing the same file by different relative spellings
// must land on one node.
func TestResolveCitedPathCollapsesSpellings(t *testing.T) {
	a := resolveCitedPath("docs/plans/agency/plan.md", "../../adr/0003-x.md")
	b := resolveCitedPath("docs/wiki/index.md", "../adr/0003-x.md")
	c := resolveCitedPath("README.md", "docs/adr/0003-x.md")
	if a != b || b != c {
		t.Fatalf("same file, three spellings, three paths: %q / %q / %q", a, b, c)
	}
	if codeRefID(a) != codeRefID(b) || codeRefID(b) != codeRefID(c) {
		t.Errorf("resolved paths must share a node ID: %q / %q / %q", codeRefID(a), codeRefID(b), codeRefID(c))
	}
}

// TestCleanCitedTokenStripsTrailingProse covers citations written mid-sentence.
// Trimming only quotes left an outer comma in place, so the backtick never came
// off either and the token became a second node for a file that already had one —
// same ID, two labels, reported as a collision with one artifact dropped.
func TestCleanCitedTokenStripsTrailingProse(t *testing.T) {
	same := "mcp/src/utils/tool_logger.py"
	for _, in := range []string{
		"mcp/src/utils/tool_logger.py`,",
		"`mcp/src/utils/tool_logger.py`;",
		"(mcp/src/utils/tool_logger.py)",
		"\"mcp/src/utils/tool_logger.py\":",
		"[mcp/src/utils/tool_logger.py]",
	} {
		if got := cleanCitedToken(in); got != same {
			t.Errorf("cleanCitedToken(%q) = %q, want %q", in, got, same)
		}
	}
	// A path is still required: prose that merely ends in punctuation is not one.
	if got := cleanCitedToken("see the file,"); got != "" {
		t.Errorf("cleanCitedToken(prose) = %q, want empty", got)
	}
}

// TestTraceComponentName covers the Requirements Traceability column, which is
// prose in practice. Sixteen of twenty-one "unimplemented component" findings in
// a 28-spec workspace came from minting a component node per spelling found here.
func TestTraceComponentName(t *testing.T) {
	cases := map[string]string{
		"PurchaseService":         "PurchaseService",
		"RechargeLot (migration)": "RechargeLot", // an aside, not a different component
		"PagciClient (existing)":  "PagciClient",
		"  Agency  ":              "Agency",
		"Order_Service-v2":        "Order_Service-v2",
		"(all)":                   "", // means every component; names none
		"—":                       "",
		"-":                       "",
		"":                        "",
		"see the design":          "", // prose, not an identifier
		"A, B":                    "", // splitList already separates cells
	}
	for in, want := range cases {
		if got := traceComponentName(in); got != want {
			t.Errorf("traceComponentName(%q) = %q, want %q", in, got, want)
		}
	}
}
