package graph

import (
	"regexp"
	"strings"
)

// codeRefID is the canonical node ID for a cited path. The path itself is stored
// in the node's SourceFile so Assemble can stat it (reuses vs references) and M3
// can resolve it to a real code node.
func codeRefID(citedPath string) string { return MakeID("code_ref", citedPath) }

// emitCitedPath appends a code_ref node for citedPath plus a references edge from
// fromID. Assemble reclassifies the edge to `reuses` (EXTRACTED, 1.0) when the
// path exists on disk, or leaves it `references` (INFERRED, planned) when it does
// not — the deterministic on-disk check the extractor cannot perform (no I/O).
func emitCitedPath(src Source, line int, citedPath, fromID string, frag *Fragment) {
	cid := codeRefID(citedPath)
	frag.Nodes = append(frag.Nodes, Node{
		ID: cid, Label: citedPath, FileType: TypeCodeRef,
		SourceFile: citedPath, SourceLocation: "L1",
	})
	frag.Edges = append(frag.Edges, Edge{
		Source: fromID, Target: cid, Relation: RelReferences,
		Confidence: Inferred, ConfidenceScore: 0.85, SourceFile: src.Path, Ref: citedPath,
	})
}

// knownCodeExts are file extensions treated as citable code/artifact paths even
// when the token has no directory separator.
var knownCodeExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".java": true, ".rb": true, ".c": true,
	".h": true, ".cpp": true, ".md": true, ".json": true, ".yaml": true,
	".yml": true, ".toml": true, ".sh": true, ".sql": true,
}

// reInlineCode matches markdown inline code spans: `like this`.
var reInlineCode = regexp.MustCompile("`([^`]+)`")

// reMdLink matches markdown link targets: [text](target).
var reMdLink = regexp.MustCompile(`\]\(([^)]+)\)`)

// citedPathsInline extracts repo-relative paths cited in a markdown line, from
// inline code spans and link targets. Deduped, in first-seen order. URLs,
// anchors, and non-path tokens are rejected so the graph stays honest.
func citedPathsInline(line string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(tok string) {
		p := cleanCitedToken(tok)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, m := range reInlineCode.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	for _, m := range reMdLink.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	return out
}

// reTreeArt strips box-drawing characters and leading list markers from a File
// Structure Plan tree line.
var reTreeArt = regexp.MustCompile(`[│├└─\t]+`)

// citedPathsInTreeLine extracts a repo path from one line of a ```text tree
// block. It drops box-drawing art and a trailing "# comment", then validates the
// remainder as a real path. Placeholder tokens (containing < or >) are rejected.
func citedPathsInTreeLine(line string) []string {
	s := reTreeArt.ReplaceAllString(line, " ")
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Take the first whitespace-delimited token.
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	p := cleanCitedToken(s)
	if p == "" {
		return nil
	}
	return []string{p}
}

// cleanCitedToken normalizes and validates a candidate path token, returning ""
// when it is not a plausible repo-relative file path.
func cleanCitedToken(tok string) string {
	tok = strings.TrimSpace(tok)
	tok = strings.Trim(tok, "`'\"")
	// Strip a trailing slash (directory) — we index files, not dirs, but keep the
	// path for resolution; a bare directory has no extension and is rejected below.
	tok = strings.TrimRight(tok, "/")
	if tok == "" {
		return ""
	}
	if strings.ContainsAny(tok, "<>*?\" ") {
		return ""
	}
	if strings.Contains(tok, "://") || strings.HasPrefix(tok, "http") || strings.HasPrefix(tok, "#") || strings.HasPrefix(tok, "mailto:") {
		return ""
	}
	// Drop a leading slash so an absolute-looking cite resolves workspace-relative.
	tok = strings.TrimPrefix(tok, "/")
	if tok == "" {
		return ""
	}
	if !looksLikePath(tok) {
		return ""
	}
	return tok
}

// looksLikePath reports whether a token is a plausible repo-relative file path:
// it either has a directory separator or ends in a known code/artifact extension.
func looksLikePath(tok string) bool {
	if idx := strings.LastIndexByte(tok, '.'); idx >= 0 {
		if knownCodeExts[strings.ToLower(tok[idx:])] {
			return true
		}
	}
	// A path with a separator and at least one segment that looks file-ish.
	if strings.Contains(tok, "/") {
		last := tok[strings.LastIndexByte(tok, '/')+1:]
		if strings.Contains(last, ".") {
			return true
		}
	}
	return false
}
