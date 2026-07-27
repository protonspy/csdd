package plan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/glossary"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/textutil"
	"github.com/protonspy/csdd/internal/validator"
)

// reFeatSlug is the spec-directory-name grammar a feat slug must satisfy
// (kebab-case), identical to workspace.KebabCheck's rule but kept local so the
// finding message is plan-specific.
var reFeatSlug = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// reDependRange matches a numeric range ("1-3") used as dependency shorthand.
// Feat slugs are kebab-case and start with a letter, so a numeric range or a
// "a..b" span is never a valid slug — it is the shorthand R2.1 forbids.
var reDependRange = regexp.MustCompile(`\d+\s*[-–—]\s*\d+`)

// ValidatePlan runs every mechanical check R3 mandates over a parsed plan, plus
// the grammar issues Parse recorded. root is used to resolve stack:/[[wiki]] refs
// and to EARS-lint seeds. The returned issues are deterministically ordered so a
// double run is byte-stable (the approve gate and CI act on a stable list).
func ValidatePlan(doc *PlanDoc, root string) []validator.Issue {
	var issues []validator.Issue

	// Grammar issues carried from Parse (table shape, missing columns).
	for _, g := range doc.grammar {
		issues = append(issues, validator.Issue{File: "plan.md", Line: g.line, Msg: g.msg})
	}

	if len(doc.Feats) == 0 {
		issues = append(issues, validator.Issue{
			File: "plan.md",
			Msg:  "plan defines no feats (the '## Feats' table is empty or missing)",
		})
	}

	// Feat slugs: validity + uniqueness. Build the set of known slugs for the
	// dependency-resolution pass below.
	known := map[string]bool{}
	seen := map[string]int{}
	for _, f := range doc.Feats {
		if !reFeatSlug.MatchString(f.Slug) {
			issues = append(issues, validator.Issue{
				File: "plan.md", Line: f.Line,
				Msg: "invalid feat slug '" + f.Slug + "': must be kebab-case (a valid spec directory name)",
			})
		} else {
			known[f.Slug] = true
		}
		if prev, ok := seen[f.Slug]; ok {
			issues = append(issues, validator.Issue{
				File: "plan.md", Line: f.Line,
				Msg: "duplicate feat slug '" + f.Slug + "' (first seen on line " + itoa(prev) + ")",
			})
		} else {
			seen[f.Slug] = f.Line
		}
	}

	// Dependencies: range shorthand, then resolution against known feats.
	for _, f := range doc.Feats {
		for _, dep := range f.Depends {
			if reDependRange.MatchString(dep) || strings.Contains(dep, "..") {
				issues = append(issues, validator.Issue{
					File: "plan.md", Line: f.Line,
					Msg: "feat '" + f.Slug + "' Depends uses range shorthand '" + dep + "'; list feat slugs explicitly",
				})
				continue
			}
			if dep == f.Slug {
				issues = append(issues, validator.Issue{
					File: "plan.md", Line: f.Line,
					Msg: "feat '" + f.Slug + "' depends on itself",
				})
				continue
			}
			if !known[dep] {
				issues = append(issues, validator.Issue{
					File: "plan.md", Line: f.Line,
					Msg: "feat '" + f.Slug + "' depends on unknown feat '" + dep + "'",
				})
			}
		}
	}

	// Dependency cycles among feats.
	for _, cyc := range featCycles(doc) {
		issues = append(issues, validator.Issue{
			File: "plan.md",
			Msg:  "feat dependency cycle: " + strings.Join(cyc, " → "),
		})
	}

	// Ref resolution: stack:<name> against docs/stack.md, [[page]] against pages,
	// adr:<slug> against docs/adr/.
	decided := decidedTech(root)
	wikiPages := wikiPageSlugs(root)
	adrs := ScanADRs(root)
	for _, f := range doc.Feats {
		for _, name := range f.StackRefs {
			if !decided[normalizeTechName(name)] {
				issues = append(issues, validator.Issue{
					File: "plan.md", Line: f.Line,
					Msg: "feat '" + f.Slug + "' references undeclared tech 'stack:" + name + "' (no matching row in docs/stack.md Decided table)",
				})
			}
		}
		for _, page := range f.WikiRefs {
			if !wikiPages[normalizeWikiSlug(page)] {
				issues = append(issues, validator.Issue{
					File: "plan.md", Line: f.Line,
					Msg: "feat '" + f.Slug + "' has a broken plan ref '[[" + page + "]]' (no matching page under docs/wiki/pages/)",
				})
			}
		}
		issues = append(issues, adrRefIssues(f, adrs)...)
	}

	// docs/adr well-formedness (R3.2), surfaced only WHERE the directory exists.
	for _, ai := range adrs.issuesResolved() {
		issues = append(issues, validator.Issue{File: ai.file, Msg: ai.msg})
	}

	// Glossary: avoided terms in the plan/feat slugs + glossary well-formedness,
	// surfaced only WHERE docs/glossary.md exists (absent is silent).
	issues = append(issues, glossaryIssues(root, doc)...)

	// Quality gates presence (R3.3).
	if len(doc.Gates) == 0 {
		issues = append(issues, validator.Issue{
			File: "plan.md",
			Msg:  "no quality gates declared (the '## Quality Gates' section is empty or missing)",
		})
	}

	// EARS lint over each present seed requirements.md (R3.4).
	issues = append(issues, seedRequirementIssues(root, doc)...)

	sortIssues(issues)
	return issues
}

// glossaryIssues lints identifiers against docs/glossary.md (R2): a feat slug or
// the plan slug carrying an avoided alias as whole token(s) is a finding naming
// the canonical term, and the glossary's own well-formedness problems (R1.3) are
// surfaced in the same pass. An absent glossary yields nothing (Load reports
// not-present).
func glossaryIssues(root string, doc *PlanDoc) []validator.Issue {
	gl := glossary.Load(root)
	if !gl.Present {
		return nil
	}
	var out []validator.Issue
	avoided := func(slug string, line int) {
		for _, m := range gl.Match(slug) {
			if m.Alias == "" {
				continue // canonical use is correct, not a finding
			}
			out = append(out, validator.Issue{
				File: "plan.md", Line: line,
				Msg: "avoided term '" + m.Alias + "' in '" + slug + "' — canonical is '" + m.Canonical + "'",
			})
		}
	}
	avoided(doc.Slug, 0)
	for _, f := range doc.Feats {
		avoided(f.Slug, f.Line)
	}
	for _, gi := range gl.Issues() {
		out = append(out, validator.Issue{File: "docs/glossary.md", Line: gi.Line, Msg: gi.Msg})
	}
	return out
}

// featCycles returns the dependency cycles among feats as slug lists, using
// Tarjan SCC so each cycle is reported once, deterministically.
func featCycles(doc *PlanDoc) [][]string {
	adj := map[string][]string{}
	nodes := map[string]bool{}
	selfLoop := map[string]bool{}
	known := map[string]bool{}
	for _, f := range doc.Feats {
		known[f.Slug] = true
	}
	for _, f := range doc.Feats {
		for _, dep := range f.Depends {
			if !known[dep] { // unresolved deps are a separate finding
				continue
			}
			if dep == f.Slug {
				selfLoop[f.Slug] = true
			}
			adj[f.Slug] = append(adj[f.Slug], dep)
			nodes[f.Slug] = true
			nodes[dep] = true
		}
	}
	for k := range adj {
		sort.Strings(adj[k])
	}
	var cycles [][]string
	for _, comp := range tarjanSCC(nodes, adj) {
		if len(comp) > 1 || (len(comp) == 1 && selfLoop[comp[0]]) {
			cycles = append(cycles, comp)
		}
	}
	sort.Slice(cycles, func(i, j int) bool {
		return strings.Join(cycles[i], "\x00") < strings.Join(cycles[j], "\x00")
	})
	return cycles
}

// tarjanSCC returns the strongly-connected components of the dependency graph,
// each internally sorted, so cycle reporting is deterministic.
func tarjanSCC(nodes map[string]bool, adj map[string][]string) [][]string {
	ids := make([]string, 0, len(nodes))
	for n := range nodes {
		ids = append(ids, n)
	}
	sort.Strings(ids)

	index := 0
	indices := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, ok := indices[w]; !ok {
				strongconnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && indices[w] < low[v] {
				low[v] = indices[w]
			}
		}
		if low[v] == indices[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			sort.Strings(comp)
			out = append(out, comp)
		}
	}
	for _, v := range ids {
		if _, ok := indices[v]; !ok {
			strongconnect(v)
		}
	}
	return out
}

// seedRequirementIssues EARS-lints every present seeds/<feat>/requirements.md,
// reusing the spec validator. Issue paths are rewritten to the seed's workspace
// path so a finding points at the exact file.
func seedRequirementIssues(root string, doc *PlanDoc) []validator.Issue {
	var out []validator.Issue
	dir := Dir(root, doc.Slug)
	for _, f := range doc.Feats {
		seedDir := filepath.Join(seedsDir(dir), f.Slug)
		if _, err := os.Stat(filepath.Join(seedDir, "requirements.md")); err != nil {
			continue // seeds are optional; only lint what exists
		}
		rel := filepath.ToSlash(filepath.Join("docs", "plans", doc.Slug, "seeds", f.Slug))
		for _, iss := range validator.ValidateSpec(seedDir, validator.PhaseRequirements) {
			iss.File = rel + "/" + iss.File
			out = append(out, iss)
		}
	}
	return out
}

// StackRow is one row of the docs/stack.md Decided table (a one-line technology
// contract). The brief inlines it in full (R7.2).
type StackRow struct {
	Domain  string
	Choice  string
	Version string
	Why     string
	Refs    string
}

// DecidedRows is decidedRows for readers outside this package (the web read
// model), keyed the same way a `stack:<name>` citation is resolved — so the
// dashboard and `csdd plan validate` agree on what "the contract lists it"
// means, rather than each parsing docs/stack.md its own way.
func DecidedRows(root string) map[string]StackRow { return decidedRows(root) }

// NormalizeTechName is normalizeTechName for the same readers: it turns a
// `stack:<name>` citation into the key DecidedRows uses.
func NormalizeTechName(s string) string { return normalizeTechName(s) }

// decidedRows parses the Decided table of docs/stack.md into full rows, keyed by
// the normalized Choice name. A missing contract yields an empty map.
func decidedRows(root string) map[string]StackRow {
	out := map[string]StackRow{}
	data, err := os.ReadFile(paths.Stack(root))
	if err != nil {
		return out
	}
	lines := strings.Split(textutil.NormalizeNewlines(string(data)), "\n")
	inDecided := false
	headerSeen := false
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "## ") {
			title := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			inDecided = strings.HasPrefix(title, "decided")
			headerSeen = false
			continue
		}
		if !inDecided || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitCells(trimmed)
		if isTableSeparator(cells) {
			continue
		}
		if !headerSeen {
			headerSeen = true // the first row is the column header
			continue
		}
		if len(cells) < 2 {
			continue
		}
		cell := func(i int) string {
			if i < len(cells) {
				return strings.TrimSpace(cells[i])
			}
			return ""
		}
		choice := cell(1)
		if choice == "" || choice == "—" || choice == "-" {
			continue
		}
		out[normalizeTechName(choice)] = StackRow{
			Domain: cell(0), Choice: choice, Version: cell(2), Why: cell(3), Refs: cell(4),
		}
	}
	return out
}

// decidedTech returns the set of normalized Decided choice names (for resolving
// feat stack: refs during validate).
func decidedTech(root string) map[string]bool {
	rows := decidedRows(root)
	out := make(map[string]bool, len(rows))
	for k := range rows {
		out[k] = true
	}
	return out
}

// wikiPageSlugs returns the normalized base names of every page under
// docs/wiki/pages/, for resolving feat [[wiki-page]] refs.
func wikiPageSlugs(root string) map[string]bool {
	out := map[string]bool{}
	dir := filepath.Join(paths.DocsWiki(root), "pages")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".md")
		out[normalizeWikiSlug(base)] = true
	}
	return out
}

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
var reMajorVersionSeg = regexp.MustCompile(`^v[0-9]+$`)

// normalizeTechName reduces a tech name (a stack: ref or a Decided choice) to a
// comparable key: the last path segment of a module path (skipping a Go-style
// vN suffix), the first whitespace/operator-delimited word (dropping a version
// like "Go 1.22"), lowercased with punctuation stripped. Both sides of the
// stack-ref check pass through it, so the match is symmetric.
func normalizeTechName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.Contains(s, "/") {
		segs := strings.Split(strings.TrimRight(s, "/"), "/")
		last := segs[len(segs)-1]
		if reMajorVersionSeg.MatchString(last) && len(segs) > 1 {
			last = segs[len(segs)-2]
		}
		s = last
	}
	if f := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '@' || r == '=' || r == '>' || r == '<' || r == '~' || r == '^' || r == '!'
	}); len(f) > 0 {
		s = f[0]
	}
	return reNonAlnum.ReplaceAllString(s, "")
}

// normalizeWikiSlug reduces a [[wiki-page]] target or a page filename to a
// comparable slug (lowercased, non-alphanumeric runs to single hyphens).
func normalizeWikiSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ".md")
	return strings.Trim(reNonAlnum.ReplaceAllString(s, "-"), "-")
}

// sortIssues imposes a deterministic order (file, line, message) so validate
// output is byte-stable across runs.
func sortIssues(in []validator.Issue) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].File != in[j].File {
			return in[i].File < in[j].File
		}
		if in[i].Line != in[j].Line {
			return in[i].Line < in[j].Line
		}
		return in[i].Msg < in[j].Msg
	})
}
