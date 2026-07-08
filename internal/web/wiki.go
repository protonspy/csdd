package web

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/graph"
	"github.com/protonspy/csdd/internal/paths"
)

// wikiLink is one outbound [[wikilink]] from a page, resolved against the page
// set. Target is the destination page slug, or "" when it resolves to nothing
// (Broken), so the UI can render it as a dead link.
type wikiLink struct {
	Text   string `json:"text"`
	Target string `json:"target"`
	Broken bool   `json:"broken"`
}

// wikiPageSummary is one page in GET /api/wiki. Content is NOT inlined — the UI
// loads it through the hardened /api/file route (host guard + secret redaction),
// exactly as PlansView loads a plan's log.md.
type wikiPageSummary struct {
	Slug     string     `json:"slug"`
	Title    string     `json:"title"`
	Path     string     `json:"path"`
	Category string     `json:"category"`
	Tags     []string   `json:"tags,omitempty"`
	Sources  []string   `json:"sources,omitempty"`
	Links    []wikiLink `json:"links"`
	InIndex  bool       `json:"in_index"`
}

// wikiOverview is GET /api/wiki: the whole read-only wiki view. Present is false
// when docs/wiki/ has not been scaffolded, so the UI can prompt `csdd wiki init`.
type wikiOverview struct {
	Present    bool              `json:"present"`
	HasIndex   bool              `json:"has_index"`
	Categories []string          `json:"categories"`
	Pages      []wikiPageSummary `json:"pages"`
	RawSources []string          `json:"raw_sources"`
}

var (
	reWikiLinkWeb     = regexp.MustCompile(`\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]`)
	reWikiIndexHead   = regexp.MustCompile(`^##\s+(.*)$`)
	reWikiIndexLink   = regexp.MustCompile(`\]\(([^)]*pages/[^)]+\.md)\)`)
	reWikiHeadingWeb  = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	reWikiHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// loadWikiOverview reads docs/wiki/ into the read model: the index.md catalog
// (category per page, in file order), every page under pages/ (title from
// frontmatter/heading, tags, sources, resolved [[wikilinks]]), and the raw
// sources under docs/raw/. A page that fails to read is skipped rather than
// failing the whole request.
func loadWikiOverview(root string) wikiOverview {
	wikiDir := paths.DocsWiki(root)
	ov := wikiOverview{Pages: []wikiPageSummary{}, RawSources: []string{}, Categories: []string{}}
	if fi, err := os.Stat(wikiDir); err != nil || !fi.IsDir() {
		return ov
	}
	ov.Present = true

	catBySlug, indexed, orderBySlug, cats := parseWikiIndex(wikiDir, &ov)
	ov.Categories = cats

	// Pass 1: read every page and its raw link texts; build the slug lookup used
	// to resolve links (which can point at a page discovered later).
	type pending struct {
		sum       wikiPageSummary
		linkTexts []string
	}
	var pages []pending
	keyToSlug := map[string]string{}
	entries, _ := os.ReadDir(filepath.Join(wikiDir, "pages"))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(wikiDir, "pages", e.Name()))
		if err != nil {
			continue
		}
		fm := frontmatter.Parse(string(data))
		title := strings.TrimSpace(fm.AsString("title", ""))
		if title == "" {
			title = wikiFirstHeading(fm.Body)
		}
		if title == "" {
			title = slug
		}
		sum := wikiPageSummary{
			Slug: slug, Title: title, Path: "docs/wiki/pages/" + e.Name(),
			Category: catBySlug[slug], Tags: fm.AsStringSlice("tags"),
			Sources: fm.AsStringSlice("sources"), InIndex: indexed[slug],
			Links: []wikiLink{},
		}
		keyToSlug[graph.NormalizeID(slug)] = slug
		pages = append(pages, pending{sum, wikiLinkTexts(data)})
	}

	// Pass 2: resolve each page's links against the full page set.
	for _, p := range pages {
		seen := map[string]bool{}
		for _, text := range p.linkTexts {
			key := graph.NormalizeID(wikiLinkBase(text))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			target := keyToSlug[key]
			p.sum.Links = append(p.sum.Links, wikiLink{
				Text: text, Target: target, Broken: target == "",
			})
		}
		ov.Pages = append(ov.Pages, p.sum)
	}
	// Structure follows docs/wiki/index.md — the catalog is the source of truth:
	// pages are grouped by their index category (in the order the index lists
	// categories) and, within a category, in the order the index lists them. Pages
	// absent from the index sort last, alphabetically.
	catRank := map[string]int{}
	for i, c := range cats {
		catRank[c] = i
	}
	rank := func(p wikiPageSummary) (int, int) {
		cr, ok := catRank[p.Category]
		if !ok || !p.InIndex {
			cr = len(cats) // uncategorized / not-in-index → last group
		}
		ord, ok := orderBySlug[p.Slug]
		if !ok {
			ord = 1 << 30
		}
		return cr, ord
	}
	sort.SliceStable(ov.Pages, func(i, j int) bool {
		ci, oi := rank(ov.Pages[i])
		cj, oj := rank(ov.Pages[j])
		if ci != cj {
			return ci < cj
		}
		if oi != oj {
			return oi < oj
		}
		return ov.Pages[i].Slug < ov.Pages[j].Slug
	})

	if rentries, err := os.ReadDir(paths.DocsRaw(root)); err == nil {
		for _, e := range rentries {
			if e.IsDir() || e.Name() == "README.md" {
				continue
			}
			ov.RawSources = append(ov.RawSources, e.Name())
		}
		sort.Strings(ov.RawSources)
	}
	return ov
}

// parseWikiIndex reads docs/wiki/index.md — the catalog that structures the wiki —
// into (category per slug, indexed set, index-order per slug, category order).
// Commented-out scaffold examples are stripped so they are never treated as real
// entries. A category heading with no real entries under it is not listed, so the
// scaffold's empty example sections don't show as phantom groups.
func parseWikiIndex(wikiDir string, ov *wikiOverview) (map[string]string, map[string]bool, map[string]int, []string) {
	catBySlug := map[string]string{}
	indexed := map[string]bool{}
	orderBySlug := map[string]int{}
	var cats []string
	data, err := os.ReadFile(filepath.Join(wikiDir, "index.md"))
	if err != nil {
		return catBySlug, indexed, orderBySlug, cats
	}
	ov.HasIndex = true
	text := reWikiHTMLComment.ReplaceAllString(strings.ReplaceAll(string(data), "\r\n", "\n"), "")
	cur := ""
	order := 0
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if m := reWikiIndexHead.FindStringSubmatch(t); m != nil {
			cur = strings.TrimSpace(m[1])
			continue
		}
		for _, m := range reWikiIndexLink.FindAllStringSubmatch(t, -1) {
			slug := wikiSlugFromPagePath(m[1])
			if slug == "" {
				continue
			}
			indexed[slug] = true
			if _, ok := catBySlug[slug]; !ok {
				catBySlug[slug] = cur
				orderBySlug[slug] = order
				order++
				if cur != "" && !wikiContains(cats, cur) {
					cats = append(cats, cur)
				}
			}
		}
	}
	return catBySlug, indexed, orderBySlug, cats
}

// wikiLinkTexts returns the trimmed [[link]] target texts in a page body, with
// commented-out regions stripped and placeholder tokens (`<...>`) dropped.
func wikiLinkTexts(data []byte) []string {
	body := reWikiHTMLComment.ReplaceAllString(strings.ReplaceAll(string(data), "\r\n", "\n"), "")
	var out []string
	for _, m := range reWikiLinkWeb.FindAllStringSubmatch(body, -1) {
		text := strings.TrimSpace(m[1])
		if text == "" || strings.ContainsAny(text, "<>") {
			continue
		}
		out = append(out, text)
	}
	return out
}

// wikiLinkBase reduces a [[link]] target (which may be a path or carry .md) to the
// base name used to match a page slug.
func wikiLinkBase(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.LastIndexByte(text, '/'); i >= 0 {
		text = text[i+1:]
	}
	return strings.TrimSuffix(text, ".md")
}

func wikiSlugFromPagePath(target string) string {
	base := filepath.Base(strings.TrimSpace(target))
	return strings.TrimSuffix(base, ".md")
}

func wikiFirstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if m := reWikiHeadingWeb.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func wikiContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
