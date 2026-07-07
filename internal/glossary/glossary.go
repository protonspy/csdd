// Package glossary parses and matches docs/glossary.md — the project's
// ubiquitous-language contract (canonical terms + _Avoid_ aliases). It is a leaf
// shared package consumed by both internal/plan (validate findings) and
// internal/graph (term nodes + references edges + analyze); it exists so the
// parse/normalize/match logic lives in one place while keeping plan and graph
// free of a dependency on each other.
//
// The contract, like docs/stack.md, is authored prose the CLI never writes — this
// package only reads, parses, and lints it.
package glossary

import (
	"os"
	"regexp"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/textutil"
)

// Term is one glossary entry: a canonical name, its definition, the aliases it
// bans (_Avoid_), and the cluster (### subheading) it sits under. canonTokens and
// aliasTokens are the precomputed whole-token sequences the matcher compares
// against identifier tokens.
type Term struct {
	Canonical  string
	Definition string
	Avoid      []string
	Cluster    string
	Line       int

	canonTokens []string
	aliasTokens [][]string
}

// Issue is a glossary well-formedness problem (R1.3), carried with its 1-based
// line in docs/glossary.md. Consumers map it into their own finding type.
type Issue struct {
	Line int
	Msg  string
}

// Match is one hit of an identifier against the glossary. Alias is empty when the
// identifier matched the canonical term; otherwise it names the avoided alias that
// matched (which is what the avoided-term lint surfaces).
type Match struct {
	Canonical string
	Alias     string
}

// Glossary is a parsed docs/glossary.md. Present records whether the file exists —
// every lint and the extractor are gated on it (an absent glossary is silent).
type Glossary struct {
	Present bool
	Terms   []Term
	issues  []Issue
}

var (
	// reTermLine matches an entry head: `**<Term>**:` optionally followed by inline
	// definition prose on the same line.
	reTermLine = regexp.MustCompile(`^\*\*(.+?)\*\*:\s*(.*)$`)
	// reAvoidLine matches the optional `_Avoid_: a, b, c` line of an entry.
	reAvoidLine = regexp.MustCompile(`^_Avoid_:\s*(.*)$`)
	// reNonAlnum collapses every run of non-alphanumerics — the normalizeTechName
	// discipline, kept local so plan and graph share one home for term matching.
	reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
)

// Load reads docs/glossary.md under root and parses it. A missing file yields a
// not-present, empty glossary (the lazy-creation convention: absent is silent).
func Load(root string) *Glossary {
	data, err := os.ReadFile(paths.Glossary(root))
	if err != nil {
		return &Glossary{}
	}
	g := Parse(string(data))
	g.Present = true
	return g
}

// Parse reads a glossary body. Only the `## Language` section is structured;
// everything else is free prose the parser ignores (the plan.md discipline).
// Within it, an entry is `**<Term>**:` then definition prose (≥1 line) then an
// optional `_Avoid_: …` line; `###` subheadings set the current cluster without
// affecting parsing. Present is left false — callers use Load to set it from file
// existence.
func Parse(content string) *Glossary {
	g := &Glossary{}
	lines := strings.Split(textutil.NormalizeNewlines(content), "\n")
	inLang := false
	cluster := ""
	var cur *Term
	flush := func() {
		if cur != nil {
			g.Terms = append(g.Terms, *cur)
			cur = nil
		}
	}
	for i, raw := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(raw, " \t"))
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			title := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			inLang = title == "language"
			cluster = ""
			continue
		}
		if !inLang {
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			flush()
			cluster = strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			continue
		}
		if m := reTermLine.FindStringSubmatch(trimmed); m != nil {
			flush()
			cur = &Term{Canonical: strings.TrimSpace(m[1]), Cluster: cluster, Line: i + 1}
			if rest := strings.TrimSpace(m[2]); rest != "" {
				cur.Definition = rest
			}
			continue
		}
		if cur == nil {
			continue
		}
		if m := reAvoidLine.FindStringSubmatch(trimmed); m != nil {
			for _, a := range strings.Split(m[1], ",") {
				if t := strings.TrimSpace(a); t != "" {
					cur.Avoid = append(cur.Avoid, t)
				}
			}
			continue
		}
		if trimmed != "" {
			if cur.Definition != "" {
				cur.Definition += " "
			}
			cur.Definition += trimmed
		}
	}
	flush()
	for i := range g.Terms {
		g.Terms[i].canonTokens = tokenize(g.Terms[i].Canonical)
		for _, a := range g.Terms[i].Avoid {
			g.Terms[i].aliasTokens = append(g.Terms[i].aliasTokens, tokenize(a))
		}
	}
	g.finalize()
	return g
}

// Issues returns the well-formedness problems found during parsing (R1.3).
func (g *Glossary) Issues() []Issue { return g.issues }

// Match returns every glossary hit for an identifier (a feat/plan slug, spec
// directory, or wiki page name). Matching is deterministic whole-token: the
// identifier is split on non-alphanumerics and a term (or alias) matches only when
// its token sequence appears as a contiguous run — so a multi-word term like
// "purchase order" matches `purchase-order-import` but "client" never matches
// `clientele`. A term contributes a canonical Match and a separate Match per
// matching alias.
func (g *Glossary) Match(identifier string) []Match {
	idTokens := tokenize(identifier)
	if len(idTokens) == 0 {
		return nil
	}
	var out []Match
	for i := range g.Terms {
		t := &g.Terms[i]
		if containsRun(idTokens, t.canonTokens) {
			out = append(out, Match{Canonical: t.Canonical})
		}
		for j, at := range t.aliasTokens {
			if containsRun(idTokens, at) {
				out = append(out, Match{Canonical: t.Canonical, Alias: t.Avoid[j]})
			}
		}
	}
	return out
}

// NormalizeTerm reduces a term or alias to a comparable collapsed key (lowercase,
// non-alphanumerics removed). Used for collision detection and node ids; the
// whole-token matcher uses tokenize instead so word boundaries are preserved.
func NormalizeTerm(s string) string {
	return reNonAlnum.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "")
}

// tokenize splits a string into lowercased alphanumeric tokens, dropping empties.
func tokenize(s string) []string {
	fields := reNonAlnum.Split(strings.ToLower(strings.TrimSpace(s)), -1)
	out := fields[:0]
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// containsRun reports whether needle appears as a contiguous whole-token run in
// hay. An empty needle never matches.
func containsRun(hay, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(hay) {
		return false
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		ok := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// finalize records the well-formedness findings (R1.3): duplicate canonical
// terms, an entry with no definition, and an alias colliding with another entry's
// canonical term or with an alias claimed by a different term.
func (g *Glossary) finalize() {
	canonLine := map[string]int{}
	for i := range g.Terms {
		t := &g.Terms[i]
		key := NormalizeTerm(t.Canonical)
		if _, ok := canonLine[key]; ok {
			g.issues = append(g.issues, Issue{Line: t.Line, Msg: "duplicate glossary term '" + t.Canonical + "'"})
		} else if key != "" {
			canonLine[key] = t.Line
		}
		if strings.TrimSpace(t.Definition) == "" {
			g.issues = append(g.issues, Issue{Line: t.Line, Msg: "glossary term '" + t.Canonical + "' has no definition"})
		}
	}
	aliasOwner := map[string]string{}
	for i := range g.Terms {
		t := &g.Terms[i]
		for _, a := range t.Avoid {
			ak := NormalizeTerm(a)
			if ak == "" {
				continue
			}
			if _, ok := canonLine[ak]; ok {
				g.issues = append(g.issues, Issue{
					Line: t.Line,
					Msg:  "glossary alias '" + a + "' of term '" + t.Canonical + "' collides with a canonical term",
				})
				continue
			}
			if prev, ok := aliasOwner[ak]; ok && prev != t.Canonical {
				g.issues = append(g.issues, Issue{
					Line: t.Line,
					Msg:  "glossary alias '" + a + "' is claimed by two terms ('" + prev + "' and '" + t.Canonical + "')",
				})
				continue
			}
			aliasOwner[ak] = t.Canonical
		}
	}
}
