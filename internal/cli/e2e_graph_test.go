package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EGoldenPath drives the full knowledge-base flow through the CLI on a
// fixture workspace: init → build → analyze → query, and wiki lint — asserting
// the golden path holds end-to-end and the graph.json is byte-stable across
// rebuilds (task 15.1).
func TestE2EGoldenPath(t *testing.T) {
	dir := freshWorkspace(t)
	seedSpec(t, dir)

	// build → graph.json exists.
	if code, _, e := run(t, "graph", "build", "--root", dir); code != 0 {
		t.Fatalf("graph build failed: %s", e)
	}
	graphPath := filepath.Join(dir, "docs", "graph", "graph.json")
	first, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatalf("graph.json missing: %v", err)
	}

	// A second build with an unchanged corpus is byte-identical (§5.9, R7.2).
	if code, _, e := run(t, "graph", "build", "--root", dir, "--full"); code != 0 {
		t.Fatalf("second build failed: %s", e)
	}
	second, _ := os.ReadFile(graphPath)
	if string(first) != string(second) {
		t.Fatalf("graph.json not byte-stable across rebuilds")
	}

	// Incremental (default) also matches the full rebuild byte-for-byte.
	if code, _, e := run(t, "graph", "build", "--root", dir); code != 0 {
		t.Fatalf("incremental build failed: %s", e)
	}
	third, _ := os.ReadFile(graphPath)
	if string(first) != string(third) {
		t.Fatalf("incremental graph.json differs from full rebuild")
	}

	// analyze surfaces the seeded gap (criterion 1.2 untested); --strict gates.
	if code, out, _ := run(t, "graph", "analyze", "--root", dir); code != 0 || !strings.Contains(out, "criterion") {
		t.Fatalf("analyze: code=%d out=%s", code, out)
	}
	if code, _, _ := run(t, "graph", "analyze", "--strict", "--root", dir); code == 0 {
		t.Fatalf("analyze --strict should gate non-zero on findings")
	}

	// query resolves the design component.
	if code, out, _ := run(t, "graph", "query", "Svc", "--root", dir); code != 0 || !strings.Contains(out, "Svc") {
		t.Fatalf("query: code=%d out=%s", code, out)
	}

	// wiki lint on the clean init scaffold passes; a dropped raw source fails it.
	if code, _, _ := run(t, "wiki", "lint", "--root", dir); code != 0 {
		t.Fatalf("clean wiki lint should pass")
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "raw", "src.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "wiki", "lint", "--root", dir); code == 0 {
		t.Fatalf("unprocessed raw source should fail wiki lint")
	}
}
