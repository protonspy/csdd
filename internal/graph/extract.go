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
		&planExtractor{},
		&adrExtractor{},
		&glossaryExtractor{},
		&wikiExtractor{},
		&goExtractor{},
	}
}

// walkSkipDirs are directory names never descended into during corpus discovery:
// VCS internals, dependency dumps, tool caches, csdd's own state dir, and build
// staging.
//
// Everything indexed here becomes a node, and every third-party import inside it
// becomes a technology the tech contract is asked to account for. A Python
// project's backend/.venv/**/site-packages therefore produced a wall of
// undeclared_tech findings about libraries the project never chose — noise that
// got `graph analyze --strict` demoted to advisory in a real workspace, which
// means a gate the user pays for stops running.
//
// The list holds only names that are unambiguously not source: a bare `build` is
// deliberately absent, since projects do keep hand-written code there.
var walkSkipDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	".csdd":         true,
	"dist":          true,
	".venv":         true, // python virtualenvs — conventional names
	"venv":          true,
	"site-packages": true, // an installed tree reached by any other route
	"__pycache__":   true,
	".tox":          true,
	".nox":          true,
	".mypy_cache":   true,
	".pytest_cache": true,
	".ruff_cache":   true,
	"vendor":        true, // go vendored dependencies
	"target":        true, // cargo / maven build output
	".gradle":       true,
	".next":         true, // framework build caches
	".nuxt":         true,
	".svelte-kit":   true,
	".turbo":        true,
	"coverage":      true, // coverage HTML dumps
	"htmlcov":       true,
}

// collectSources walks the workspace once and returns every file some extractor
// claims, read and root-relative, in deterministic (lexical) order. Files under
// docs/raw/ are returned with empty Content — they are indexed opaquely, so
// their (possibly large, possibly binary) bytes are never read.
//
// A problem reading one claimed file, or walking one subtree, does not abort the
// build (a transient lock must not fail `graph build`), but it is never silently
// dropped either: it is returned as a warning the caller surfaces. Only a failure
// on the root itself aborts, since there is then nothing to index.
// csddOwnedDirs are the workspace's own corpus, always indexed no matter what
// .gitignore says about them.
//
// This guard is not hypothetical: csdd's own repository gitignores `/specs/` and
// `/.claude/`, because there they are generated workspace state rather than
// source. Honouring that would make `graph build` skip the entire spec corpus and
// every shipped artifact — the graph would go quiet instead of noisy, which is
// the worse failure of the two.
var csddOwnedDirs = map[string]bool{
	"specs":   true,
	"docs":    true,
	".claude": true,
}

// ignoreRules holds the directory-shaped .gitignore patterns declared by each
// directory of the tree, keyed by that directory's root-relative path ("." for
// the root).
//
// A project already states which directories are not source, and it states it
// per subtree: the Python virtualenv that flooded a real workspace's tech lint
// with findings about libraries nobody chose is declared in backend/.gitignore,
// not the root one. Reading them makes the skip list adapt to the project instead
// of guessing at every ecosystem's conventions — while walkSkipDirs stays as the
// floor, since a directory may be uncommitted for reasons that have nothing to do
// with being source, and a workspace need not be a git repository at all.
type ignoreRules struct {
	byDir map[string][]ignorePattern
}

// ignorePattern is one directory-shaped rule: name matched against a path
// relative to the directory that declared it, anchored when the rule began with
// "/".
type ignorePattern struct {
	path     string // slash-separated, no leading or trailing "/"
	anchored bool
}

func newIgnoreRules() *ignoreRules { return &ignoreRules{byDir: map[string][]ignorePattern{}} }

// load parses dirRel/.gitignore, if present. A missing or unreadable file simply
// contributes no rules.
func (ir *ignoreRules) load(root, dirRel string) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dirRel), ".gitignore"))
	if err != nil {
		return
	}
	var pats []ignorePattern
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if p, ok := parseIgnoreDirPattern(line); ok {
			pats = append(pats, p)
		}
	}
	if len(pats) > 0 {
		ir.byDir[dirRel] = pats
	}
}

// skips reports whether any ancestor's rules exclude the directory at rel.
func (ir *ignoreRules) skips(rel string) bool {
	if csddOwnedDirs[rel] {
		return false
	}
	// Walk the ancestors, matching rel against the rules each one declared.
	for dir := parentDir(rel); ; dir = parentDir(dir) {
		for _, p := range ir.byDir[dir] {
			if p.matches(dir, rel) {
				return true
			}
		}
		if dir == "." {
			return false
		}
	}
}

// matches reports whether rel (root-relative) is excluded by a rule declared in
// dir. An anchored rule must sit directly at the declaring directory; an
// unanchored one matches at any depth below it, which is git's own semantics for
// a pattern without a slash.
func (p ignorePattern) matches(dir, rel string) bool {
	sub := rel
	if dir != "." {
		if !strings.HasPrefix(rel, dir+"/") {
			return false
		}
		sub = rel[len(dir)+1:]
	}
	if p.anchored || strings.Contains(p.path, "/") {
		return sub == p.path
	}
	return sub == p.path || strings.HasSuffix(sub, "/"+p.path)
}

// parseIgnoreDirPattern reads one .gitignore line and returns a rule when the
// line unambiguously names a directory.
//
// The subset is deliberately narrow. Everything skipped here is content that
// stays indexed, which is a recoverable kind of wrong; a mis-parsed glob or
// negation that silently drops a subtree is not. So a line with wildcards, a
// negation, or anything else this parser is not certain about contributes no rule
// at all.
func parseIgnoreDirPattern(line string) (ignorePattern, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "!") {
		return ignorePattern{}, false
	}
	if strings.ContainsAny(s, "*?[]") {
		return ignorePattern{}, false
	}
	anchored := strings.HasPrefix(s, "/")
	s = strings.Trim(s, "/")
	if s == "" || s == "." || strings.Contains(s, "..") {
		return ignorePattern{}, false
	}
	// A trailing slash in the source line marked it as a directory; without one
	// the pattern may name a file, and matching it against directories is still
	// safe because a file and a directory never share a path.
	return ignorePattern{path: s, anchored: anchored}, true
}

// parentDir returns the slash-separated parent of rel, or "." at the top.
func parentDir(rel string) string {
	if i := strings.LastIndex(rel, "/"); i > 0 {
		return rel[:i]
	}
	return "."
}

func collectSources(root string, extractors []Extractor) ([]Source, []string, error) {
	var out []Source
	var warnings []string
	ignores := newIgnoreRules()
	ignores.load(root, ".")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err // a bad root has nothing to index — abort
			}
			// A stat/permission error on one entry: skip it, but report the gap.
			warnings = append(warnings, "could not access "+filepath.ToSlash(mustRel(root, p))+": "+err.Error())
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && walkSkipDirs[name] {
				return filepath.SkipDir
			}
			rel := filepath.ToSlash(mustRel(root, p))
			if p != root && ignores.skips(rel) {
				return filepath.SkipDir
			}
			// WalkDir hands a directory to us before its children, so loading the
			// rules here guarantees they are in place for everything below.
			ignores.load(root, rel)
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
			warnings = append(warnings, "could not read "+rel+" (claimed by an extractor, so its nodes are missing): "+rerr.Error())
			return nil
		}
		out = append(out, Source{Path: rel, Content: content})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	sort.Strings(warnings)
	return out, warnings, nil
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

// normLinesNoComments is normLines with `<!-- … -->` regions blanked first, so
// commented-out scaffold content (draft/deferred tasks, example annotations) is
// never parsed into real nodes and edges. stripHTMLComments preserves newlines,
// so line indices — and the "L<n>" source_location they feed — stay accurate.
func normLinesNoComments(content []byte) []string {
	return strings.Split(stripHTMLComments(textutil.NormalizeNewlines(string(content))), "\n")
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
