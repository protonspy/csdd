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

// csddRoots are the csdd-managed entries. They are grouped separately in the
// file explorer and are the paths watched for live updates.
var csddRoots = []struct {
	rel string
	dir bool
}{
	{paths.SpecsSeg, true},   // specs/
	{paths.ClaudeDir, true},  // .claude/
	{paths.EntryFile, false}, // CLAUDE.md
	{paths.MCPFile, false},   // .mcp.json
}

// skipDirs are never descended into — VCS/dependency/cache noise that would
// bloat the tree and isn't meaningful for analysing the project.
var skipDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"__pycache__":   true,
	".venv":         true,
	"venv":          true,
	".mypy_cache":   true,
	".pytest_cache": true,
	".ruff_cache":   true,
}

// maxViewBytes caps the size of a file served to the viewer; larger files (e.g.
// model weights / datasets) are not loaded into memory.
const maxViewBytes = 2 << 20 // 2 MiB

// TreeNode is one node of the file tree. Path is workspace-relative (slash).
type TreeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Dir      bool       `json:"dir"`
	Children []TreeNode `json:"children,omitempty"`
}

// WorkspaceTree splits the explorer into the csdd-managed artifacts and the rest
// of the project, so the two are visually separate.
type WorkspaceTree struct {
	Csdd    []TreeNode `json:"csdd"`
	Project []TreeNode `json:"project"`
}

// Tree returns the full project file tree, with the csdd-managed entries
// (specs/, .claude/, CLAUDE.md, .mcp.json) grouped separately from everything
// else in the project root.
func Tree(root string) WorkspaceTree {
	csdd := []TreeNode{}
	managed := map[string]bool{}
	for _, r := range csddRoots {
		managed[r.rel] = true
		abs := filepath.Join(root, r.rel)
		fi, err := os.Stat(abs)
		if err != nil {
			continue
		}
		switch {
		case r.dir && fi.IsDir():
			csdd = append(csdd, walkDir(root, abs))
		case !r.dir && !fi.IsDir():
			csdd = append(csdd, TreeNode{Name: r.rel, Path: rel(root, abs)})
		}
	}

	project := []TreeNode{}
	if entries, err := os.ReadDir(root); err == nil {
		sortEntries(entries)
		for _, e := range entries {
			name := e.Name()
			if managed[name] || skipDirs[name] {
				continue
			}
			abs := filepath.Join(root, name)
			if e.IsDir() {
				project = append(project, walkDir(root, abs))
			} else {
				project = append(project, TreeNode{Name: name, Path: rel(root, abs)})
			}
		}
	}

	return WorkspaceTree{Csdd: csdd, Project: project}
}

func walkDir(root, dir string) TreeNode {
	node := TreeNode{Name: filepath.Base(dir), Path: rel(root, dir), Dir: true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return node
	}
	sortEntries(entries)
	for _, e := range entries {
		if e.IsDir() && skipDirs[e.Name()] {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue // don't follow or list symlinks (escape / loop risk)
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

func sortEntries(entries []os.DirEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir() // directories first
		}
		return entries[i].Name() < entries[j].Name()
	})
}

// FileContent is a single file's text plus a Monaco language id for highlighting.
type FileContent struct {
	Path string `json:"path"`
	Lang string `json:"lang"`
	Text string `json:"text"`
}

// ReadFile reads any project file for the viewer. relPath is resolved and
// confirmed to stay under the project root, so traversal inputs (../, absolute
// paths, anything outside the workspace) are rejected. Directories, oversized
// files, and binaries are handled gracefully rather than dumped.
func ReadFile(root, relPath string) (FileContent, error) {
	abs, err := resolveInWorkspace(root, relPath)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, fmt.Errorf("is a directory: %s", relPath)
	}
	if isSecretFile(filepath.Base(abs)) {
		return FileContent{
			Path: filepath.ToSlash(relPath),
			Lang: "plaintext",
			Text: "(hidden — looks like a secret/credentials file)",
		}, nil
	}
	if info.Size() > maxViewBytes {
		return FileContent{
			Path: filepath.ToSlash(relPath),
			Lang: "plaintext",
			Text: fmt.Sprintf("(file too large to preview: %d bytes)", info.Size()),
		}, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileContent{}, err
	}
	if isBinary(data) {
		return FileContent{
			Path: filepath.ToSlash(relPath),
			Lang: "plaintext",
			Text: "(binary file — not shown)",
		}, nil
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
	if target != rootAbs && !strings.HasPrefix(target, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path not allowed: %s", relPath)
	}
	// Symlink-safe containment: resolve symlinks and re-check, so a symlink
	// inside the workspace can't disclose a file outside it — important once the
	// dashboard is reachable publicly via --tunnel.
	rootReal := rootAbs
	if rr, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = rr
	}
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("path not allowed: %s", relPath)
	}
	if targetReal != rootReal && !strings.HasPrefix(targetReal, rootReal+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", relPath)
	}
	return target, nil
}

// isSecretFile reports whether a filename looks like it holds credentials. The
// viewer refuses to serve these even with auth, so a leaked token (or a public
// tunnel) can't dump them. Names only — directories are skipped via skipDirs.
func isSecretFile(name string) bool {
	n := strings.ToLower(name)
	switch {
	case n == ".env" || strings.HasPrefix(n, ".env."),
		n == ".npmrc" || n == ".netrc" || n == "credentials",
		strings.HasPrefix(n, "id_rsa") || strings.HasPrefix(n, "id_ed25519") || strings.HasPrefix(n, "id_dsa"):
		return true
	}
	switch filepath.Ext(n) {
	case ".pem", ".key", ".tfstate", ".pfx", ".p12", ".keystore":
		return true
	}
	return false
}

// isBinary reports whether data looks binary (contains a NUL byte in its head).
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
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
	case ".jsx":
		return "javascript"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".sh", ".bash":
		return "shell"
	case ".yml", ".yaml":
		return "yaml"
	case ".toml":
		return "ini"
	case ".sql":
		return "sql"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".scss":
		return "scss"
	case ".xml":
		return "xml"
	case ".dockerfile":
		return "dockerfile"
	default:
		return "plaintext"
	}
}
