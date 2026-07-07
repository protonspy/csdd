package plan

import (
	"regexp"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/textutil"
)

// featColumns is the fixed column vocabulary of the `## Feats` table (R2.1),
// matched case-insensitively against the header row. Each data row is read by
// column name (not position) so a well-formed table survives incidental column
// reordering, while a missing column is reported precisely.
var featColumns = []string{"#", "feat", "objective", "depends", "milestone", "(p)", "refs"}

// reWikiRef extracts the target of a [[wiki-page]] token, tolerating an alias or
// fragment (`[[page|Alias]]`, `[[page#Section]]`) exactly as the wiki extractor
// does, so a Refs cell and a wiki body resolve links identically.
var reWikiRef = regexp.MustCompile(`^\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]$`)

// stackRefPrefix is the token prefix that declares a technology dependency in a
// feat's Refs cell (resolved against the docs/stack.md Decided table by validate).
const stackRefPrefix = "stack:"

// adrRefPrefix is the token prefix that cites a decision record in a feat's Refs
// cell (resolved against docs/adr/ by validate — the third ref token beside
// stack: and [[wiki]]).
const adrRefPrefix = "adr:"

// Parse reads a plan.md body into a PlanDoc. It never fails: a malformed feat
// table yields grammar issues carried on the doc (surfaced by ValidatePlan),
// never a hard error — the same lenient discipline the graph extractors use so a
// half-authored document still lints instead of crashing. Slug is not set here
// (the loader supplies it from the directory name).
func Parse(content string) *PlanDoc {
	doc := &PlanDoc{}
	fm := frontmatter.Parse(content)
	doc.Name = fm.AsString("name", "")
	doc.Status = fm.AsString("status", "")

	lines := strings.Split(textutil.NormalizeNewlines(content), "\n")
	section := ""               // lowercased current H2 title
	var featCols map[string]int // header column → index, nil until the header row
	var notes []string          // verbatim Executor Notes lines

	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			section = strings.ToLower(title)
			featCols = nil // a new section resets any in-progress feat table
			continue
		}

		switch section {
		case "feats":
			if !strings.HasPrefix(trimmed, "|") {
				continue
			}
			cells := splitCells(trimmed)
			if isTableSeparator(cells) {
				continue
			}
			if featCols == nil {
				featCols = parseFeatHeader(doc, cells, i+1)
				continue
			}
			if f, ok := parseFeatRow(doc, featCols, cells, i+1); ok {
				doc.Feats = append(doc.Feats, f)
			}
		case "quality gates":
			if g, ok := parseGate(trimmed, i+1); ok {
				doc.Gates = append(doc.Gates, g)
			}
		case "executor notes":
			notes = append(notes, raw)
		}
	}

	doc.ExecutorNotes = strings.TrimSpace(strings.Join(notes, "\n"))
	return doc
}

// parseFeatHeader records the index of each recognized column and reports any
// required column that is absent, so `plan validate` can point at the exact gap.
func parseFeatHeader(doc *PlanDoc, cells []string, line int) map[string]int {
	cols := map[string]int{}
	for idx, c := range cells {
		cols[strings.ToLower(strings.TrimSpace(c))] = idx
	}
	for _, want := range featColumns {
		if _, ok := cols[want]; !ok {
			doc.grammar = append(doc.grammar, gramIssue{
				line: line, msg: "Feats table is missing the '" + want + "' column",
			})
		}
	}
	return cols
}

// parseFeatRow builds a Feat from one data row. A row with fewer cells than the
// header declares is a grammar issue (and still parsed as far as it goes, so a
// truncated row never silently vanishes). A row whose Feat cell is empty is
// skipped entirely — it carries no addressable feat.
func parseFeatRow(doc *PlanDoc, cols map[string]int, cells []string, line int) (Feat, bool) {
	get := func(name string) string {
		idx, ok := cols[name]
		if !ok || idx >= len(cells) {
			return ""
		}
		return strings.TrimSpace(cells[idx])
	}
	if len(cells) < len(cols) {
		doc.grammar = append(doc.grammar, gramIssue{
			line: line,
			msg:  "malformed feat row: expected " + itoa(len(cols)) + " columns, got " + itoa(len(cells)),
		})
	}
	slug := get("feat")
	if slug == "" {
		return Feat{}, false
	}
	f := Feat{
		Num:       get("#"),
		Slug:      slug,
		Objective: get("objective"),
		Depends:   splitDepends(get("depends")),
		Milestone: get("milestone"),
		Parallel:  parseParallel(get("(p)")),
		Line:      line,
	}
	f.WikiRefs, f.StackRefs, f.ADRRefs, f.Refs = tokenizeRefs(get("refs"))
	return f, true
}

// parseGate matches a `- <label>: <command>` bullet. Non-matching lines under
// the section are prose and ignored.
var reGate = regexp.MustCompile(`^-\s+([^:]+):\s*(.+?)\s*$`)

func parseGate(trimmed string, line int) (Gate, bool) {
	m := reGate.FindStringSubmatch(trimmed)
	if m == nil {
		return Gate{}, false
	}
	return Gate{Label: strings.TrimSpace(m[1]), Command: strings.TrimSpace(m[2]), Line: line}, true
}

// splitDepends splits a Depends cell into verbatim feat-slug tokens. Empty and
// the "none" placeholders ("-", "—") collapse to no dependencies. Range shorthand
// is preserved as a single token (never expanded) so validate can report it.
func splitDepends(cell string) []string {
	cell = strings.TrimSpace(cell)
	if cell == "" || cell == "-" || cell == "—" || strings.EqualFold(cell, "none") {
		return nil
	}
	var out []string
	for _, part := range strings.Split(cell, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseParallel reads the (P) column. Any affirmative marker means parallel; the
// blank and "none" placeholders mean sequential.
func parseParallel(cell string) bool {
	switch strings.ToLower(strings.TrimSpace(cell)) {
	case "p", "(p)", "yes", "y", "x", "true", "✓", "*":
		return true
	}
	return false
}

// tokenizeRefs splits a Refs cell on whitespace and classifies each token into
// wiki refs ([[page]]), stack refs (stack:name), and adr refs (adr:slug),
// returning the three plus the raw token list. An adr: token's slug is captured
// verbatim — including empty or non-kebab — so validate can report it malformed
// (R2.2). Unrecognized tokens are kept only in raw (the brief may still show
// them; validate ignores them).
func tokenizeRefs(cell string) (wiki, stack, adr, raw []string) {
	for _, tok := range strings.Fields(cell) {
		raw = append(raw, tok)
		if m := reWikiRef.FindStringSubmatch(tok); m != nil {
			wiki = append(wiki, strings.TrimSpace(m[1]))
			continue
		}
		if strings.HasPrefix(tok, stackRefPrefix) {
			if name := strings.TrimSpace(strings.TrimPrefix(tok, stackRefPrefix)); name != "" {
				stack = append(stack, name)
			}
			continue
		}
		if strings.HasPrefix(tok, adrRefPrefix) {
			adr = append(adr, strings.TrimSpace(strings.TrimPrefix(tok, adrRefPrefix)))
		}
	}
	return wiki, stack, adr, raw
}

// --- shared parsing helpers (kept local so the package has no graph dependency) ---

// splitCells splits a markdown table row into trimmed cell values (leading and
// trailing pipe removed), mirroring internal/graph's helper of the same name.
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

// itoa is a tiny int→string used in grammar messages (avoids importing strconv
// for one call and keeps message construction allocation-light).
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
