package session

import (
	"path/filepath"
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
		"go.mod":                      "module x\n", // outside the allowlist
	})
}

func TestTreeRoots(t *testing.T) {
	root := treeWorkspace(t)
	nodes := Tree(root)
	names := map[string]bool{}
	for _, n := range nodes {
		names[n.Name] = true
	}
	for _, want := range []string{"specs", ".claude", "CLAUDE.md", ".mcp.json"} {
		if !names[want] {
			t.Errorf("tree missing top-level node %q (got %v)", want, names)
		}
	}
	if names["go.mod"] {
		t.Errorf("go.mod must not appear in the tree (outside allowlist)")
	}
	// .git must be skipped inside .claude.
	var claude TreeNode
	for _, n := range nodes {
		if n.Name == ".claude" {
			claude = n
		}
	}
	for _, c := range claude.Children {
		if c.Name == ".git" {
			t.Errorf(".git should be skipped in the tree")
		}
	}
}

func TestReadFileAllowed(t *testing.T) {
	root := treeWorkspace(t)
	fc, err := ReadFile(root, "specs/f/tasks.md")
	if err != nil {
		t.Fatal(err)
	}
	if fc.Lang != "markdown" {
		t.Errorf("lang = %q, want markdown", fc.Lang)
	}
	if fc.Text == "" {
		t.Errorf("expected file content")
	}

	if fc, err := ReadFile(root, ".mcp.json"); err != nil || fc.Lang != "json" {
		t.Errorf("reading .mcp.json: err=%v lang=%q", err, fc.Lang)
	}
}

func TestReadFileBlocksTraversal(t *testing.T) {
	root := treeWorkspace(t)
	bad := []string{
		"../../etc/passwd",
		"specs/f/../../../etc/passwd",
		"go.mod",                      // exists at root but not under an allowed root
		filepath.Join(root, "go.mod"), // absolute path
		"",
	}
	for _, p := range bad {
		if _, err := ReadFile(root, p); err == nil {
			t.Errorf("ReadFile(%q) should have been rejected", p)
		}
	}
}
