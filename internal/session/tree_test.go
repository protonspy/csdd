package session

import (
	"testing"
)

func treeWorkspace(t *testing.T) string {
	t.Helper()
	return writeWorkspace(t, map[string]string{
		"specs/f/tasks.md":            "## Phase 1: Foundation\n- [ ] 1. x\n",
		"specs/f/spec.json":           `{"phase":"x"}`,
		".claude/steering/product.md": "# Product\n",
		".claude/.git/config":         "[core]\n", // must be skipped
		"CLAUDE.md":                   "# Claude\n",
		".mcp.json":                   `{"mcpServers":{}}`,
		"go.mod":                      "module x\n",        // project file (not csdd)
		"src/main.go":                 "package main\n",    // project file
		"node_modules/dep/index.js":   "module.exports={}", // must be skipped
	})
}

func TestTreeGroups(t *testing.T) {
	wt := Tree(treeWorkspace(t))

	csdd := names(wt.Csdd)
	for _, want := range []string{"specs", ".claude", "CLAUDE.md", ".mcp.json"} {
		if !csdd[want] {
			t.Errorf("csdd group missing %q (got %v)", want, csdd)
		}
	}

	proj := names(wt.Project)
	for _, want := range []string{"go.mod", "src"} {
		if !proj[want] {
			t.Errorf("project group missing %q (got %v)", want, proj)
		}
	}
	// csdd roots must not be duplicated into the project group.
	for _, n := range []string{"specs", ".claude", "CLAUDE.md", ".mcp.json"} {
		if proj[n] {
			t.Errorf("%q should be in csdd group, not project", n)
		}
	}
	// node_modules must be skipped entirely.
	if proj["node_modules"] {
		t.Errorf("node_modules must be skipped from the tree")
	}
	// .git must be skipped inside .claude.
	for _, c := range childOf(wt.Csdd, ".claude") {
		if c.Name == ".git" {
			t.Errorf(".git should be skipped in the tree")
		}
	}
}

func TestReadFileAllowed(t *testing.T) {
	root := treeWorkspace(t)
	if fc, err := ReadFile(root, "specs/f/tasks.md"); err != nil || fc.Lang != "markdown" || fc.Text == "" {
		t.Errorf("reading tasks.md: err=%v lang=%q", err, fc.Lang)
	}
	// Project files are now browsable (whole-project explorer).
	if fc, err := ReadFile(root, "src/main.go"); err != nil || fc.Lang != "go" {
		t.Errorf("reading project file src/main.go: err=%v lang=%q", err, fc.Lang)
	}
}

func TestReadFileRejectsEscapeAndDirs(t *testing.T) {
	root := treeWorkspace(t)
	for _, p := range []string{"../../etc/passwd", "/etc/passwd", "", "specs"} {
		if _, err := ReadFile(root, p); err == nil {
			t.Errorf("ReadFile(%q) should have been rejected", p)
		}
	}
}

func names(nodes []TreeNode) map[string]bool {
	out := map[string]bool{}
	for _, n := range nodes {
		out[n.Name] = true
	}
	return out
}

func childOf(nodes []TreeNode, name string) []TreeNode {
	for _, n := range nodes {
		if n.Name == name {
			return n.Children
		}
	}
	return nil
}
