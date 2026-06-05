package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
)

// allowedRoots are the only paths the file viewer may surface or read. Anything
// outside them (the rest of the filesystem) is off-limits; this is the single
// allowlist that the tree walker and ReadFile both enforce.
var allowedRoots = []struct {
	rel string
	dir bool
}{
	{paths.SpecsSeg, true},   // specs/
	{paths.ClaudeDir, true},  // .claude/
	{paths.EntryFile, false}, // CLAUDE.md
	{paths.MCPFile, false},   // .mcp.json
}

// skipDirs are never descended into (noise / heavy / irrelevant to the workspace).
var skipDirs = map[string]bool{".git": true, "node_modules": true}

// TreeNode is one node of the workspace file tree the explorer renders. Path is
// workspace-relative and slash-separated.
type TreeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Dir      bool       `json:"dir"`
	Children []TreeNode `json:"children,omitempty"`
}

// Tree returns the top-level nodes of the workspace file tree (specs/, .claude/,
// CLAUDE.md, .mcp.json), each directory walked recursively.
func Tree(root string) []TreeNode {
	var nodes []TreeNode
	for _, r := range allowedRoots {
		abs := filepath.Join(root, r.rel)
		fi, err := os.Stat(abs)
		if err != nil {
			continue
		}
		switch {
		case r.dir && fi.IsDir():
			nodes = append(nodes, walkDir(root, abs))
		case !r.dir && !fi.IsDir():
			nodes = append(nodes, TreeNode{Name: r.rel, Path: rel(root, abs)})
		}
	}
	return nodes
}

func walkDir(root, dir string) TreeNode {
	node := TreeNode{Name: filepath.Base(dir), Path: rel(root, dir), Dir: true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return node
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir() // directories first
		}
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		if e.IsDir() && skipDirs[e.Name()] {
			continue
		}
		abs := filepath.Join(dir, e.Name())
		if e.IsDir() {
			node.Children = append(node.Children, walkDir(root, abs))
		} else {
			node.Children = append(node.Children, TreeNode{Name: e.Name(), Path: rel(root, abs)})
		}
	}
	return node
}

// FileContent is a single file's text plus a Monaco language id for highlighting.
type FileContent struct {
	Path string `json:"path"`
	Lang string `json:"lang"`
	Text string `json:"text"`
}

// ReadFile reads a workspace-relative file for the viewer. relPath is resolved
// and confirmed to live under one of the allowed roots before any disk access,
// so path-traversal inputs (../, absolute paths, paths outside the workspace)
// are rejected.
func ReadFile(root, relPath string) (FileContent, error) {
	abs, err := resolveInWorkspace(root, relPath)
	if err != nil {
		return FileContent{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileContent{}, err
	}
	return FileContent{Path: filepath.ToSlash(relPath), Lang: langFor(abs), Text: string(data)}, nil
}

func resolveInWorkspace(root, relPath string) (string, error) {
	if relPath == "" {
		return "", errors.New("empty path")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// Clean against "/" so any leading ../ that would escape the workspace is
	// collapsed away before the join, neutralising traversal attempts.
	cleaned := filepath.Clean(string(filepath.Separator) + filepath.FromSlash(relPath))
	cleaned = strings.TrimPrefix(cleaned, string(filepath.Separator))
	target := filepath.Join(rootAbs, cleaned)
	if !underAllowedRoot(rootAbs, target) {
		return "", fmt.Errorf("path not allowed: %s", relPath)
	}
	return target, nil
}

func underAllowedRoot(rootAbs, target string) bool {
	for _, r := range allowedRoots {
		base := filepath.Join(rootAbs, r.rel)
		if r.dir {
			if target == base || strings.HasPrefix(target, base+string(filepath.Separator)) {
				return true
			}
		} else if target == base {
			return true
		}
	}
	return false
}

// langFor maps a file extension to a Monaco language id (best-effort).
func langFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "markdown"
	case ".json":
		return "json"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".go":
		return "go"
	case ".sh", ".bash":
		return "shell"
	case ".yml", ".yaml":
		return "yaml"
	case ".toml":
		return "ini"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	default:
		return "plaintext"
	}
}
