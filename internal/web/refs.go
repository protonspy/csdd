package web

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protonspy/csdd/internal/glossary"
	"github.com/protonspy/csdd/internal/graph"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/plan"
	"github.com/protonspy/csdd/internal/workspace"
)

// The citation resolver.
//
// A workspace cites things in one grammar — `[[wiki-page]]`, `adr:<slug>`,
// `stack:<name>` — and `csdd plan validate` already decides what each token
// resolves to and when it is broken, superseded or ambiguous. This resolves the
// same tokens for the dashboard, from the same parsers, so a chip in the UI and
// a finding from the gate can never disagree. The browser is told the answer; it
// does not re-derive it.
//
// Three more kinds are addressable with the same syntax because the workspace
// already holds them: `spec:<feature>`, `feat:<slug>` and `term:<canonical>`.
// They are not part of the Refs grammar the linter enforces — nothing validates
// them — but they cost nothing to resolve and make prose linkable.

// Resolution states. `warn` is deliberately absent: a citation either resolves
// or it does not, and "superseded" is the one shade in between.
const (
	refOK         = "ok"
	refBroken     = "broken"
	refSuperseded = "superseded"
	refAmbiguous  = "ambiguous"
)

// maxRefTokens caps one batch. The UI asks for a whole Refs column at once; this
// stops a hand-rolled URL from turning into an unbounded filesystem walk.
const maxRefTokens = 256

// refResolution is one resolved citation. Route is the dashboard location for
// the target, empty when nothing resolves. Successor carries the replacement
// token for a superseded record, so the UI can offer it directly.
type refResolution struct {
	Token     string `json:"token"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	State     string `json:"state"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
	Meta      string `json:"meta,omitempty"`
	Route     string `json:"route,omitempty"`
	Successor string `json:"successor,omitempty"`
}

var reWikiToken = regexp.MustCompile(`^\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]$`)

// parseRefToken splits a token into (kind, target). An unrecognised string
// yields an empty kind — callers treat that as "not a citation" rather than as
// an error, because these tokens are found in free prose.
func parseRefToken(token string) (kind, target string) {
	t := strings.TrimSpace(token)
	if m := reWikiToken.FindStringSubmatch(t); m != nil {
		return "wiki", strings.TrimSpace(m[1])
	}
	for _, k := range []string{"adr", "stack", "spec", "feat", "term"} {
		if rest, ok := strings.CutPrefix(t, k+":"); ok {
			rest = strings.TrimSpace(rest)
			if rest == "" {
				return "", ""
			}
			return k, rest
		}
	}
	return "", ""
}

// refIndex is one snapshot of everything citable in a workspace, built once per
// request and reused across the tokens in that request.
type refIndex struct {
	root   string
	adrs   *plan.ADRSet
	stack  map[string]plan.StackRow
	wiki   map[string]string        // normalized slug key → real slug
	terms  map[string]glossary.Term // lowercased canonical → term
	planOf map[string]string        // feat slug → plan slug
	featOf map[string]plan.Feat
}

func newRefIndex(root string) *refIndex {
	ix := &refIndex{
		root:   root,
		adrs:   plan.ScanADRs(root),
		stack:  plan.DecidedRows(root),
		wiki:   map[string]string{},
		terms:  map[string]glossary.Term{},
		planOf: map[string]string{},
		featOf: map[string]plan.Feat{},
	}
	// Wiki pages, keyed exactly as wiki.go resolves a [[link]].
	entries, _ := os.ReadDir(filepath.Join(paths.DocsWiki(root), "pages"))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		ix.wiki[graph.NormalizeID(slug)] = slug
	}
	for _, t := range glossary.Load(root).Terms {
		ix.terms[strings.ToLower(t.Canonical)] = t
	}
	if slugs, err := plan.List(root); err == nil {
		for _, planSlug := range slugs {
			doc, err := plan.Load(root, planSlug)
			if err != nil {
				continue
			}
			for _, f := range doc.Feats {
				ix.planOf[f.Slug] = planSlug
				ix.featOf[f.Slug] = f
			}
		}
	}
	return ix
}

// resolve answers one token.
func (ix *refIndex) resolve(token string) refResolution {
	kind, target := parseRefToken(token)
	res := refResolution{Token: token, Kind: kind, Label: target, State: refBroken}
	switch kind {
	case "wiki":
		return ix.resolveWiki(res, target)
	case "adr":
		return ix.resolveADR(res, target)
	case "stack":
		return ix.resolveStack(res, target)
	case "spec":
		return ix.resolveSpec(res, target)
	case "feat":
		return ix.resolveFeat(res, target)
	case "term":
		return ix.resolveTerm(res, target)
	default:
		res.Kind = "unknown"
		res.Label = strings.TrimSpace(token)
		res.Title = "not a citation token"
		return res
	}
}

func (ix *refIndex) resolveWiki(res refResolution, target string) refResolution {
	base := wikiLinkBase(target)
	slug, ok := ix.wiki[graph.NormalizeID(base)]
	if !ok {
		res.Title = "no such wiki page"
		res.Body = "No page under docs/wiki/pages/ matches this link; `csdd wiki lint` reports it as broken."
		res.Meta = "docs/wiki/pages/" + base + ".md"
		return res
	}
	res.State = refOK
	res.Label = slug
	res.Title = slug
	res.Meta = "docs/wiki/pages/" + slug + ".md"
	res.Route = "#/wiki/" + urlSegment(slug)
	return res
}

func (ix *refIndex) resolveADR(res refResolution, target string) refResolution {
	if !plan.ValidADRSlug(target) {
		res.Title = "malformed decision ref"
		res.Body = "An `adr:` citation is a kebab-case slug — the tail of the record's filename."
		res.Meta = "docs/adr/"
		return res
	}
	rec, outcome := ix.adrs.Resolve(target)
	switch outcome {
	case plan.ADRAmbiguous:
		res.State = refAmbiguous
		res.Title = "ambiguous decision ref"
		res.Body = "Two or more records under docs/adr/ share this slug, so the citation names no single decision."
		res.Meta = "docs/adr/"
		return res
	case plan.ADRMissing:
		res.Title = "no such decision record"
		res.Body = "`csdd plan validate` breaks on a citation that resolves to nothing."
		res.Meta = "docs/adr/"
		return res
	}
	res.Title = rec.Title
	res.Body = rec.Body
	res.Meta = rec.File
	res.Route = "#/adr/" + urlSegment(rec.Slug)
	res.State = refOK
	if rec.Status == plan.ADRStatusSuperseded {
		res.State = refSuperseded
		if s, ok := ix.adrs.Resolve(successorSlug(ix.adrs, rec.SupersededBy)); ok == plan.ADRResolved && s != nil {
			res.Successor = "adr:" + s.Slug
		}
	}
	return res
}

func (ix *refIndex) resolveStack(res refResolution, target string) refResolution {
	key := plan.NormalizeTechName(target)
	row, ok := ix.stack[key]
	if !ok {
		res.Title = "not in the Decided table"
		res.Body = "A technology absent from docs/stack.md is an open decision: propose options and ask, never adopt it silently."
		res.Meta = "docs/stack.md"
		return res
	}
	res.State = refOK
	res.Label = key
	res.Title = strings.TrimSpace(row.Choice + " " + row.Version)
	res.Body = row.Why
	res.Meta = "docs/stack.md · " + row.Domain
	res.Route = "#/stack?row=" + urlQuery(key)
	return res
}

func (ix *refIndex) resolveSpec(res refResolution, target string) refResolution {
	if workspace.SafeName(target, "spec") != nil {
		res.Title = "not a spec name"
		return res
	}
	if _, err := os.Stat(filepath.Join(paths.Specs(ix.root), target, "spec.json")); err != nil {
		res.Title = "no such spec"
		res.Meta = "specs/" + target + "/"
		return res
	}
	res.State = refOK
	res.Title = target
	res.Meta = "specs/" + target + "/"
	res.Route = "#/specs/" + urlSegment(target)
	return res
}

func (ix *refIndex) resolveFeat(res refResolution, target string) refResolution {
	planSlug, ok := ix.planOf[target]
	if !ok {
		res.Title = "no such feat"
		res.Body = "No plan under docs/plans/ declares a feat with this slug."
		res.Meta = "docs/plans/"
		return res
	}
	f := ix.featOf[target]
	res.State = refOK
	res.Title = "feat " + f.Num + " — " + target
	res.Body = f.Objective
	res.Meta = "docs/plans/" + planSlug + "/plan.md"
	res.Route = "#/plans/" + urlSegment(planSlug) + "?feat=" + urlQuery(target)
	return res
}

func (ix *refIndex) resolveTerm(res refResolution, target string) refResolution {
	t, ok := ix.terms[strings.ToLower(target)]
	if !ok {
		res.Title = "not a canonical term"
		res.Body = "docs/glossary.md holds one canonical term per concept; this is not one of them."
		res.Meta = "docs/glossary.md"
		return res
	}
	res.State = refOK
	res.Label = t.Canonical
	res.Title = t.Canonical
	res.Body = t.Definition
	res.Meta = "docs/glossary.md"
	res.Route = "#/glossary?term=" + urlQuery(t.Canonical)
	return res
}

// urlSegment and urlQuery encode a value for the hash routes the dashboard
// parses: segments are decoded with decodeURIComponent, query values by
// URLSearchParams (which is why `+` is fine for a space here and not there).
func urlSegment(s string) string { return url.PathEscape(s) }
func urlQuery(s string) string   { return url.QueryEscape(s) }

// successorSlug maps a superseded record's `superseded-by` number back to a slug
// by scanning the set — the number is the identity, the slug is the currency.
func successorSlug(set *plan.ADRSet, number int) string {
	if number == 0 {
		return ""
	}
	for _, a := range set.All {
		if a.Number == number {
			return a.Slug
		}
	}
	return ""
}

// resolveRefs answers a batch in request order, skipping blanks. Duplicate
// tokens are resolved once and repeated, so a Refs column that cites the same
// decision from six feats costs one lookup.
func resolveRefs(root string, tokens []string) []refResolution {
	ix := newRefIndex(root)
	seen := map[string]refResolution{}
	out := make([]refResolution, 0, len(tokens))
	for _, t := range tokens {
		if strings.TrimSpace(t) == "" {
			continue
		}
		r, ok := seen[t]
		if !ok {
			r = ix.resolve(t)
			seen[t] = r
		}
		out = append(out, r)
	}
	return out
}
