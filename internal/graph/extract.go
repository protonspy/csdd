package graph

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/textutil"
)

// Source is one file of a corpus, already read and root-relative. Content is the
// raw bytes; for opaque corpora (docs/raw/) Content is left empty because the
// file is indexed by path only.
type Source struct {
	Path    string // workspace-root-relative, forward slashes
	Content []byte
}

// Fragment is one extractor's partial contribution: nodes plus edges whose
// endpoints may still be symbolic (e.g. an annotation "1.1") until Assemble
// reconciles them against the assembled node set.
type Fragment struct {
	Nodes []Node
	Edges []Edge
}

// Extractor is the single seam through which every corpus enters the graph.
// Nothing downstream of extraction branches on which corpus a fragment came
// from (design principle 6) — adding a new corpus is adding a new Extractor.
type Extractor interface {
	// Matches reports whether this extractor handles the given root-relative path.
	Matches(path string) bool
	// Extract parses one source into fragments. Deterministic; performs no I/O.
	Extract(src Source) ([]Fragment, error)
}

// defaultExtractors returns the corpus extractors in dispatch order. Order
// matters where paths overlap: docs/stack.md and the dependency manifests are
// claimed by the stack extractor before the wiki extractor would treat stack.md
// as a page.
func defaultExtractors() []Extractor {
	return []Extractor{
		&specExtractor{},
		&claudeExtractor{},
		&stackExtractor{},
		&wikiExtractor{},
		&goExtractor{},
	}
}

// walkSkipDirs are directory names never descended into during corpus discovery:
// VCS internals, dependency dumps, csdd's own state dir, build staging, and the
// vendored graphify study material.
var walkSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".csdd":        true,
	"dist":         true,
}

// collectSources walks the workspace once and returns every file some extractor
// claims, read and root-relative, in deterministic (lexical) order. Files under
// docs/raw/ are returned with empty Content — they are indexed opaquely, so
// their (possibly large, possibly binary) bytes are never read.
func collectSources(root string, extractors []Extractor) ([]Source, error) {
	var out []Source
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			// A transient stat error on one entry must not abort the whole build.
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && walkSkipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		rel := filepath.ToSlash(mustRel(root, p))
		if !anyMatch(extractors, rel) {
			return nil
		}
		if isRawSourcePath(rel) {
			// Opaque: path only, content never parsed.
			out = append(out, Source{Path: rel})
			return nil
		}
		content, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		out = append(out, Source{Path: rel, Content: content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func mustRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}

func anyMatch(extractors []Extractor, path string) bool {
	for _, e := range extractors {
		if e.Matches(path) {
			return true
		}
	}
	return false
}

// dispatch runs the first matching extractor for a source. Exactly one extractor
// handles each file (Matches order is the tie-break), so a fragment's origin is
// unambiguous.
func dispatch(extractors []Extractor, src Source) ([]Fragment, error) {
	for _, e := range extractors {
		if e.Matches(src.Path) {
			return e.Extract(src)
		}
	}
	return nil, nil
}

// isRawSourcePath reports whether a path is an immutable raw source under
// docs/raw/ (indexed opaquely as a raw_source node).
func isRawSourcePath(path string) bool {
	return strings.HasPrefix(path, "docs/raw/")
}

// --- shared parsing helpers -------------------------------------------------

// normLines splits content into line-ending-normalized lines.
func normLines(content []byte) []string {
	return strings.Split(textutil.NormalizeNewlines(string(content)), "\n")
}

// rtrim strips trailing spaces and tabs (keeping leading indentation, which some
// parsers use to detect block boundaries).
func rtrim(s string) string { return strings.TrimRight(s, " \t") }

// reHTMLComment matches an HTML/markdown comment, including multi-line ones.
var reHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// stripHTMLComments blanks out `<!-- … -->` regions so example links and
// placeholders in commented-out scaffold content are never parsed as real edges.
// Newlines inside a comment are preserved so line-oriented parsing stays aligned.
func stripHTMLComments(s string) string {
	return reHTMLComment.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat("\n", strings.Count(m, "\n"))
	})
}

// loc formats a 0-based line index as the "L<n>" source_location convention.
func loc(lineIndex int) string {
	return "L" + itoa(lineIndex+1)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// specNameFromPath extracts <name> from "specs/<name>/...", or "" if the path is
// not inside a spec folder.
func specNameFromPath(path string) string {
	const prefix = "specs/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return ""
}

// reIDList matches a comma-separated annotation ID list, individual tokens split
// by the caller. Kept liberal on the token content so malformed tokens (ranges)
// survive to become pending references rather than being silently discarded.
var reWhitespace = regexp.MustCompile(`\s+`)

// splitList splits a comma-separated annotation value into trimmed, non-empty
// tokens. Range shorthand ("3.1-3.5") is preserved verbatim as a single token so
// Assemble surfaces it as an unresolved reference (§5.6) rather than expanding
// it.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		t := strings.TrimSpace(part)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// splitCells splits a markdown table row into trimmed cell values (leading and
// trailing pipe removed).
func splitCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// isTableSeparator reports whether a table row is the `|---|---|` divider.
func isTableSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

// collapseSpaces normalizes internal whitespace runs to single spaces.
func collapseSpaces(s string) string {
	return strings.TrimSpace(reWhitespace.ReplaceAllString(s, " "))
}
