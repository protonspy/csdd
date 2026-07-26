// Package codewiki parses and lints the repo-derived wiki document the
// `codewiki` skill compiles from a source checkout dropped under docs/raw/.
//
// The document is authored prose — this package never writes it. It exists
// because the failure mode of an LLM-authored code wiki is not bad writing, it
// is *confident citation of code that does not exist*: a path that was never in
// the tree, a line range past the end of the file, a Structure tree that drifted
// from the sections below it. Those are mechanically checkable, so they are
// checked here rather than trusted, mirroring how internal/glossary lints the
// ubiquitous-language contract it likewise never authors.
//
// The format is an interchange shape rather than a private one: a document
// compiled locally and one produced by an external repo-wiki generator are the
// same artifact, lintable by the same rules and ingestible by the same `wiki`
// skill.
package codewiki

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/protonspy/csdd/internal/textutil"
)

// Finding kinds. They double as the JSON discriminator, so they are stable
// names, not prose.
const (
	KindHeader     = "header"     // provenance header missing or malformed
	KindStructure  = "structure"  // Structure tree and the SECTION blocks disagree
	KindSlug       = "slug"       // duplicate slug, or one that is not derived from its title
	KindCitation   = "citation"   // a [path:start-end]() that does not resolve in the repo
	KindSourceFile = "sourcefile" // a file in a "Relevant source files" block that is not in the repo
	KindCoverage   = "coverage"   // a section that cites nothing
	KindRepo       = "repo"       // the checkout could not be resolved; citation checks were skipped
)

// Finding is one lint result, carried with its 1-based line in the document.
// Informational marks a finding that reports a limit of the check rather than a
// fault in the document — it is listed, but it does not fail the gate.
type Finding struct {
	Kind          string `json:"kind"`
	Message       string `json:"message"`
	File          string `json:"file,omitempty"`
	Line          int    `json:"line,omitempty"`
	Informational bool   `json:"informational,omitempty"`
}

// Header is the provenance comment on line 1. It names the tool that compiled
// the document, the upstream repository, and — for a locally compiled one —
// `src:`, the workspace-relative checkout the citations are relative to.
//
// Src is what makes the citation check possible without a flag: a document
// carries the path to the tree it was read from. Externally generated documents
// have no such field, which is why --repo exists.
type Header struct {
	Tool      string `json:"tool"`
	Repo      string `json:"repo"`
	Src       string `json:"src,omitempty"`
	Generated string `json:"generated,omitempty"`
	Count     int    `json:"count,omitempty"`
	Present   bool   `json:"present"`
}

// OutlineEntry is one row of the `## Structure` tree: a dotted number and the
// title it labels.
type OutlineEntry struct {
	Number string
	Title  string
	Line   int
}

// Section is one `<<< SECTION: N Title [slug] >>>` block and everything under it
// up to the next delimiter.
type Section struct {
	Number    string
	Title     string
	Slug      string
	Line      int
	Citations []Citation
	Files     []SourceFile
}

// Citation is a `[path:start-end]()` reference — a markdown link with an empty
// target, which is the format's citation form. The empty parens are load-bearing:
// they are what separates a citation from a mermaid node label like
// `["main() function<br/>[main.go:165]"]`, which carries the same bracketed
// path:line text but is not a link and must not be linted as one.
//
// WholeFile marks a citation that names a file and no lines at all
// (`[pkg/registry/registry.go]()`). It is still a claim that the file exists,
// which is the failure mode worth catching; there is simply no range to check.
// A comma-separated citation (`[beat.py:34,505]()`) becomes one Citation per
// range, since each range is independently right or wrong.
type Citation struct {
	Path      string
	Start     int
	End       int // 0 when the citation names a single line
	WholeFile bool
	Line      int
	Raw       string
}

// SourceFile is an entry of a "Relevant source files" <details> block —
// `- [path](path)`. It has no line range, so only existence is checkable.
type SourceFile struct {
	Path string
	Line int
}

// Doc is a parsed codewiki document.
type Doc struct {
	Header   Header
	Outline  []OutlineEntry
	Sections []Section
}

var (
	headerRe   = regexp.MustCompile(`^<!--(.*)-->\s*$`)
	sectionRe  = regexp.MustCompile(`^<<<\s*SECTION:\s*(.+?)\s*\[([^\]]*)\]\s*>>>\s*$`)
	numTitleRe = regexp.MustCompile(`^(\d+(?:\.\d+)*)\s+(.+)$`)
	// Citations come in several forms in the wild, often in one document:
	// [path:1-9](), the code-spanned [`path:1-9`](), the double-bracketed
	// [[path:1-9]](), a comma-separated [path:34,505-510](), and a bare
	// [path]() naming no lines. Only the empty target is structural — that is
	// what makes a citation a citation and a mermaid label just a label — so the
	// brackets and backticks around it are accepted in any observed combination.
	//
	// A link with an empty target need not be a citation at all: these documents
	// also cross-reference their own sections that way ([2.1](), [5.1 Worker
	// Architecture]()). Excluding whitespace and colons from the path, then
	// demanding the rangeless form still look like a path, is what keeps those
	// out — see looksLikePath.
	citationRe = regexp.MustCompile("\\[\\[?`?([^\\]\\s\"'<>`:]+?)(?::(\\d+(?:-\\d+)?(?:, ?\\d+(?:-\\d+)?)*))?`?\\]\\]?\\(\\)")
	fileLinkRe = regexp.MustCompile(`^\s*[-*]\s*\[([^\]]+)\]\(([^)]*)\)\s*$`)
	countRe    = regexp.MustCompile(`^(\d+)\s+(?:pages?|sections?)$`)
	srcRe      = regexp.MustCompile(`^src:\s*(.+)$`)
	repoSlugRe = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)
	tsRe       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`)
	nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)
	treeGlyphs = strings.NewReplacer("├", " ", "└", " ", "│", " ", "─", " ")
)

// Parse reads the document text into the model. It never fails: an unparseable
// document is an empty model, and the missing pieces surface as findings from
// Lint rather than as an error the caller has to translate.
func Parse(text string) *Doc {
	lines := strings.Split(textutil.NormalizeNewlines(text), "\n")
	doc := &Doc{}
	doc.Header = parseHeader(lines)
	doc.Outline = parseOutline(lines)
	doc.Sections = parseSections(lines)
	return doc
}

// parseHeader reads the provenance comment from the first non-blank line. Fields
// are pipe-separated and identified by shape, not position, so a document that
// omits `src:` (every externally generated document does) still parses.
func parseHeader(lines []string) Header {
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := headerRe.FindStringSubmatch(line)
		if m == nil {
			return Header{}
		}
		h := Header{Present: true}
		for i, f := range strings.Split(m[1], "|") {
			f = strings.TrimSpace(f)
			switch {
			case i == 0:
				h.Tool = f
			case repoSlugRe.MatchString(f) && h.Repo == "":
				h.Repo = f
			case srcRe.MatchString(f):
				h.Src = strings.TrimSpace(srcRe.FindStringSubmatch(f)[1])
			case tsRe.MatchString(f):
				h.Generated = f
			case countRe.MatchString(f):
				h.Count, _ = strconv.Atoi(countRe.FindStringSubmatch(f)[1])
			}
		}
		return h
	}
	return Header{}
}

// parseOutline reads the `## Structure` tree — the table of contents a reader
// (human or LLM) navigates by before paying for any section body.
func parseOutline(lines []string) []OutlineEntry {
	var out []OutlineEntry
	inTree := false
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "#") {
			inTree = strings.EqualFold(strings.TrimSpace(strings.TrimLeft(trimmed, "# ")), "Structure")
			continue
		}
		if !inTree || trimmed == "" {
			continue
		}
		// A SECTION delimiter means the tree ended without a heading after it.
		if sectionRe.MatchString(trimmed) {
			break
		}
		entry := strings.TrimSpace(treeGlyphs.Replace(trimmed))
		if m := numTitleRe.FindStringSubmatch(entry); m != nil {
			out = append(out, OutlineEntry{Number: m[1], Title: m[2], Line: i + 1})
		}
	}
	return out
}

// parseSections splits the document on the `<<< SECTION: ... >>>` delimiters and
// harvests each block's citations and source-file list.
func parseSections(lines []string) []Section {
	var out []Section
	cur := -1
	for i, raw := range lines {
		if m := sectionRe.FindStringSubmatch(strings.TrimSpace(raw)); m != nil {
			s := Section{Slug: m[2], Line: i + 1}
			if nt := numTitleRe.FindStringSubmatch(m[1]); nt != nil {
				s.Number, s.Title = nt[1], nt[2]
			} else {
				s.Title = m[1]
			}
			out = append(out, s)
			cur = len(out) - 1
			continue
		}
		if cur < 0 {
			continue
		}
		for _, c := range citationRe.FindAllStringSubmatch(raw, -1) {
			out[cur].Citations = append(out[cur].Citations, citationsFrom(c[1], c[2], i+1, c[0])...)
		}
		// A "Relevant source files" entry is a list item whose link text and
		// target are the same path; anything else on a list line is prose. The
		// text half may be code-spanned, so compare unwrapped.
		if m := fileLinkRe.FindStringSubmatch(raw); m != nil {
			if text, target := unspan(m[1]), unspan(m[2]); text == target && target != "" {
				out[cur].Files = append(out[cur].Files, SourceFile{Path: target, Line: i + 1})
			}
		}
	}
	return out
}

// unspan strips a markdown code span's backticks, so `path/to/file.go` and
// path/to/file.go compare equal.
func unspan(s string) string { return strings.Trim(strings.TrimSpace(s), "`") }

// citationsFrom expands one matched link into the citations it asserts: one per
// comma-separated range, or a single whole-file citation when it names no lines.
func citationsFrom(p, spec string, line int, raw string) []Citation {
	if spec == "" {
		if !looksLikePath(p) {
			return nil // a cross-reference to another section, not a citation
		}
		return []Citation{{Path: p, WholeFile: true, Line: line, Raw: raw}}
	}
	var out []Citation
	for _, part := range strings.Split(spec, ",") {
		c := Citation{Path: p, Line: line, Raw: raw}
		lo, hi, found := strings.Cut(strings.TrimSpace(part), "-")
		c.Start, _ = strconv.Atoi(lo)
		if found {
			c.End, _ = strconv.Atoi(hi)
		}
		out = append(out, c)
	}
	return out
}

// looksLikePath separates a rangeless citation from a section cross-reference:
// "pkg/registry/registry.go" is a file, "2.1" and "3.4" are page numbers. A slash
// settles it; otherwise the extension has to begin with a letter, which is what
// rules the numbering out.
func looksLikePath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	ext := path.Ext(s)
	if len(ext) < 2 {
		return false
	}
	c := ext[1]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// Slugify derives a section's slug from its full "N.M Title" heading, which is
// how the delimiter's bracketed identifier is expected to read: "3.1 CLI Layer"
// becomes "3-1-cli-layer".
func Slugify(heading string) string {
	s := nonAlnumRe.ReplaceAllString(strings.ToLower(heading), "-")
	return strings.Trim(s, "-")
}

// Lint parses the document at docPath and reports every mechanical fault. repo
// is the checkout the citations are relative to; when it is empty the citation
// and source-file checks are skipped and that omission is itself reported (as
// informational), so a clean run never silently means "half the checks ran".
func Lint(docPath, repo string) ([]Finding, error) {
	raw, err := os.ReadFile(docPath) //nolint:gosec // operator-supplied path, by design
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", docPath, err)
	}
	doc := Parse(string(raw))
	var findings []Finding
	findings = append(findings, lintHeader(doc)...)
	findings = append(findings, lintStructure(doc)...)
	findings = append(findings, lintSlugs(doc)...)
	findings = append(findings, lintCoverage(doc)...)
	findings = append(findings, lintReferences(doc, repo)...)
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Line < findings[j].Line })
	return findings, nil
}

func lintHeader(doc *Doc) []Finding {
	h := doc.Header
	if !h.Present {
		return []Finding{{
			Kind:    KindHeader,
			Line:    1,
			Message: "no provenance header — line 1 must be `<!-- csdd-codewiki v1 | owner/repo | src: docs/raw/<checkout> | <RFC3339> | N sections -->`",
		}}
	}
	var out []Finding
	if h.Repo == "" {
		out = append(out, Finding{Kind: KindHeader, Line: 1, Message: "header names no upstream repository (expected an `owner/repo` field)"})
	}
	if h.Generated == "" {
		out = append(out, Finding{Kind: KindHeader, Line: 1, Message: "header carries no generation timestamp (expected an RFC3339 field)"})
	}
	if h.Count > 0 && h.Count != len(doc.Sections) {
		out = append(out, Finding{
			Kind:    KindHeader,
			Line:    1,
			Message: fmt.Sprintf("header declares %d sections, document has %d", h.Count, len(doc.Sections)),
		})
	}
	return out
}

// lintStructure holds the Structure tree and the SECTION blocks to each other.
// Either one drifting alone is the failure that makes the document untrustworthy
// to navigate: a reader picks a section off the tree that no longer exists, or
// pays for a body no index announced.
func lintStructure(doc *Doc) []Finding {
	var out []Finding
	inOutline := map[string]OutlineEntry{}
	for _, e := range doc.Outline {
		if prev, dup := inOutline[e.Number]; dup {
			out = append(out, Finding{
				Kind:    KindStructure,
				Line:    e.Line,
				Message: fmt.Sprintf("Structure lists %q twice (first at line %d)", e.Number, prev.Line),
			})
			continue
		}
		inOutline[e.Number] = e
	}
	inDoc := map[string]Section{}
	for _, s := range doc.Sections {
		if s.Number == "" {
			out = append(out, Finding{
				Kind:    KindStructure,
				Line:    s.Line,
				Message: fmt.Sprintf("section %q has no leading number (expected `<<< SECTION: N Title [slug] >>>`)", s.Title),
			})
			continue
		}
		if prev, dup := inDoc[s.Number]; dup {
			out = append(out, Finding{
				Kind:    KindStructure,
				Line:    s.Line,
				Message: fmt.Sprintf("section %q appears twice (first at line %d)", s.Number, prev.Line),
			})
			continue
		}
		inDoc[s.Number] = s
		e, ok := inOutline[s.Number]
		if !ok {
			out = append(out, Finding{
				Kind:    KindStructure,
				Line:    s.Line,
				Message: fmt.Sprintf("section %s %q is not listed in the Structure tree", s.Number, s.Title),
			})
			continue
		}
		if e.Title != s.Title {
			out = append(out, Finding{
				Kind:    KindStructure,
				Line:    s.Line,
				Message: fmt.Sprintf("section %s is titled %q here and %q in the Structure tree", s.Number, s.Title, e.Title),
			})
		}
	}
	for _, e := range doc.Outline {
		if _, ok := inDoc[e.Number]; !ok {
			out = append(out, Finding{
				Kind:    KindStructure,
				Line:    e.Line,
				Message: fmt.Sprintf("Structure lists %s %q, but no section carries that number", e.Number, e.Title),
			})
			continue
		}
		// A subsection whose parent is missing leaves the tree unrooted.
		if i := strings.LastIndex(e.Number, "."); i > 0 {
			parent := e.Number[:i]
			if _, ok := inDoc[parent]; !ok {
				out = append(out, Finding{
					Kind:    KindStructure,
					Line:    e.Line,
					Message: fmt.Sprintf("section %s has no parent section %s", e.Number, parent),
				})
			}
		}
	}
	return out
}

// lintSlugs enforces that the bracketed identifier is unique and derivable from
// the heading. The slug is the document's addressing scheme — a stale one is a
// link that points at the wrong body, which is worse than one that 404s.
func lintSlugs(doc *Doc) []Finding {
	var out []Finding
	seen := map[string]int{}
	for _, s := range doc.Sections {
		if s.Slug == "" {
			out = append(out, Finding{Kind: KindSlug, Line: s.Line, Message: fmt.Sprintf("section %s %q has an empty slug", s.Number, s.Title)})
			continue
		}
		if first, dup := seen[s.Slug]; dup {
			out = append(out, Finding{
				Kind:    KindSlug,
				Line:    s.Line,
				Message: fmt.Sprintf("slug %q is used twice (first at line %d)", s.Slug, first),
			})
			continue
		}
		seen[s.Slug] = s.Line
		heading := strings.TrimSpace(s.Number + " " + s.Title)
		if want := Slugify(heading); want != s.Slug {
			out = append(out, Finding{
				Kind:    KindSlug,
				Line:    s.Line,
				Message: fmt.Sprintf("slug %q does not match its heading (expected %q)", s.Slug, want),
			})
		}
	}
	return out
}

// lintCoverage flags a section that cites nothing. Prose with no citation is the
// one thing this format exists to prevent: unfalsifiable claims about code.
func lintCoverage(doc *Doc) []Finding {
	var out []Finding
	for _, s := range doc.Sections {
		if len(s.Citations) == 0 {
			out = append(out, Finding{
				Kind:    KindCoverage,
				Line:    s.Line,
				Message: fmt.Sprintf("section %s %q cites no source (expected at least one `[path:start-end]()`)", s.Number, s.Title),
			})
		}
	}
	return out
}

// lintReferences resolves every citation and source-file entry against the
// checkout. This is the check the format is built around and the one an LLM
// cannot self-verify by re-reading its own output.
func lintReferences(doc *Doc, repo string) []Finding {
	if repo == "" {
		return []Finding{{
			Kind:          KindRepo,
			Line:          1,
			Informational: true,
			Message:       "no checkout resolved — citation and source-file checks were skipped (pass --repo DIR, or add `src: <dir>` to the header)",
		}}
	}
	var out []Finding
	seen := map[string]fileMeta{}
	for _, s := range doc.Sections {
		for _, f := range s.Files {
			if _, err := resolveInRepo(repo, f.Path); err != nil {
				out = append(out, Finding{Kind: KindSourceFile, Line: f.Line, File: f.Path, Message: err.Error()})
			}
		}
		for _, c := range s.Citations {
			out = append(out, lintCitation(repo, c, seen)...)
		}
	}
	return out
}

// fileMeta is the measured shape of a cited file, cached so a document that
// cites the same file eighty times reads it once.
type fileMeta struct {
	lines    int // -1 when the file is not text
	trailing bool
}

func lintCitation(repo string, c Citation, seen map[string]fileMeta) []Finding {
	if c.WholeFile {
		if _, err := resolveInRepo(repo, c.Path); err != nil {
			return []Finding{{Kind: KindCitation, Line: c.Line, File: c.Path, Message: err.Error()}}
		}
		return nil
	}
	end := c.End
	if end == 0 {
		end = c.Start
	}
	if c.Start < 1 {
		return []Finding{{Kind: KindCitation, Line: c.Line, File: c.Path, Message: fmt.Sprintf("%s starts at line %d (lines are 1-based)", c.Raw, c.Start)}}
	}
	if end < c.Start {
		return []Finding{{Kind: KindCitation, Line: c.Line, File: c.Path, Message: fmt.Sprintf("%s ends before it starts", c.Raw)}}
	}
	abs, err := resolveInRepo(repo, c.Path)
	if err != nil {
		return []Finding{{Kind: KindCitation, Line: c.Line, File: c.Path, Message: err.Error()}}
	}
	meta, cached := seen[c.Path]
	if !cached {
		meta = measure(abs)
		seen[c.Path] = meta
	}
	n := meta.lines
	if n < 0 { // not a text file — existence is all that can be checked
		return nil
	}
	// A file that ends in a newline has an addressable empty line after it, and
	// any split("\n")-based counter numbers it. Measured against a real corpus
	// that convention accounts for the overwhelming majority of end == n+1
	// citations, so tolerating it is what keeps the genuine overruns — a range
	// citing 60 lines of a 43-line manifest — visible instead of buried under
	// forty off-by-ones.
	if end == n+1 && meta.trailing {
		return nil
	}
	if end > n {
		return []Finding{{
			Kind:    KindCitation,
			Line:    c.Line,
			File:    c.Path,
			Message: fmt.Sprintf("%s — %s has %d line(s)", c.Raw, c.Path, n),
		}}
	}
	return nil
}

// resolveInRepo maps a document-relative citation path onto the checkout,
// refusing anything that climbs out of it. A citation is a claim about *this*
// repository; one that resolves through `..` is either a mistake or a way to
// read a file the document has no business naming.
func resolveInRepo(repo, rel string) (string, error) {
	clean := path.Clean(strings.TrimSpace(rel))
	if clean == "" || clean == "." {
		return "", fmt.Errorf("empty path")
	}
	if path.IsAbs(clean) || filepath.IsAbs(filepath.FromSlash(clean)) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q escapes the checkout", rel)
	}
	abs := filepath.Join(repo, filepath.FromSlash(clean))
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("no such file in the checkout: %s", rel)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a file", rel)
	}
	return abs, nil
}

// measure counts the lines of a text file and notes whether it ends in a
// newline. lines is -1 when the file is binary — an image cited as a source file
// is legitimate, but a line range into it is not something this lint can judge.
func measure(abs string) fileMeta {
	data, err := os.ReadFile(abs) //nolint:gosec // path already constrained to the checkout
	if err != nil {
		return fileMeta{lines: -1}
	}
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return fileMeta{lines: -1}
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	trailing := len(data) > 0 && bytes.HasSuffix(data, []byte("\n"))
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && !trailing {
		n++
	}
	return fileMeta{lines: n, trailing: trailing}
}

// ResolveRepo decides which checkout a document's citations are relative to:
// an explicit --repo wins, then the header's `src:` field (resolved against the
// workspace root), and finally a sibling directory named after the document —
// docs/raw/acme-widget.md next to docs/raw/widget/. Returns ""
// when none of them lands on a directory, which Lint reports rather than guesses
// around.
func ResolveRepo(root, docPath, override string, h Header) string {
	if override != "" {
		if isDir(override) {
			return override
		}
		if abs := filepath.Join(root, filepath.FromSlash(override)); isDir(abs) {
			return abs
		}
		return ""
	}
	if h.Src != "" {
		if abs := filepath.Join(root, filepath.FromSlash(h.Src)); isDir(abs) {
			return abs
		}
		if isDir(h.Src) {
			return h.Src
		}
	}
	if h.Repo != "" {
		if _, name, ok := strings.Cut(h.Repo, "/"); ok && name != "" {
			if abs := filepath.Join(filepath.Dir(docPath), name); isDir(abs) {
				return abs
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(docPath), filepath.Ext(docPath))
	if abs := filepath.Join(filepath.Dir(docPath), base); isDir(abs) {
		return abs
	}
	return ""
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// Discover returns every .md directly under dir that carries a provenance
// header, so `csdd codewiki lint` with no argument can gate the whole dropzone.
func Discover(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(p) //nolint:gosec // enumerated from the dropzone
		if err != nil {
			continue
		}
		if h := parseHeader(strings.Split(textutil.NormalizeNewlines(string(raw)), "\n")); h.Present && h.Repo != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Faults reports whether any finding is a real fault (i.e. not informational) —
// the signal the CLI turns into a non-zero exit.
func Faults(findings []Finding) int {
	n := 0
	for _, f := range findings {
		if !f.Informational {
			n++
		}
	}
	return n
}
