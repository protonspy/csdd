package graph

import (
	"path"
	"regexp"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/textutil"
)

// wikiExtractor indexes the documentation knowledge base under docs/: every
// markdown page outside docs/raw/ becomes a wiki_page carrying its frontmatter,
// and every file under docs/raw/ becomes an opaque raw_source (path only). It is
// wiki-aware — [[wikilinks]] and `sources:` provenance become links_to and
// derived_from edges — while treating docs/plans/ and other docs the same way.
type wikiExtractor struct{}

func (wikiExtractor) Matches(p string) bool {
	if !strings.HasPrefix(p, "docs/") {
		return false
	}
	if strings.HasPrefix(p, "docs/graph/") {
		return false // the only generated subtree in docs/ — never re-indexed
	}
	if p == "docs/raw/README.md" {
		return false // the scaffold's dropzone explainer, not a raw source to ingest
	}
	if isRawSourcePath(p) {
		return true // any file, any extension, indexed opaquely
	}
	return strings.HasSuffix(p, ".md")
}

func (wikiExtractor) Extract(src Source) ([]Fragment, error) {
	if isRawSourcePath(src.Path) {
		return rawSourceFragment(src), nil
	}
	return wikiPageFragment(src), nil
}

// rawSourceID is the canonical ID for an immutable source under docs/raw/, keyed
// by its path relative to docs/raw/ so a `sources:` reference (with or without
// the docs/raw/ prefix) resolves to it.
func rawSourceID(rawRelPath string) string {
	rel := strings.TrimPrefix(rawRelPath, "docs/raw/")
	return MakeID("raw", rel)
}

// wikiPageID is the canonical ID for a page, keyed by its base filename so a
// [[wikilink]] written with the page's title or slug resolves to it (both
// normalize identically).
func wikiPageID(pagePath string) string {
	base := strings.TrimSuffix(path.Base(pagePath), ".md")
	return MakeID("wiki", base)
}

func rawSourceFragment(src Source) []Fragment {
	rel := strings.TrimPrefix(src.Path, "docs/raw/")
	return []Fragment{{Nodes: []Node{{
		ID: rawSourceID(src.Path), Label: rel, FileType: TypeRawSource,
		SourceFile: src.Path, SourceLocation: "L1",
	}}}}
}

var reWikiLink = regexp.MustCompile(`\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]`)

func wikiPageFragment(src Source) []Fragment {
	fm := frontmatter.Parse(string(src.Content))
	pid := wikiPageID(src.Path)
	attrs := map[string]any{}
	if tags := fm.AsStringSlice("tags"); len(tags) > 0 {
		attrs["tags"] = tags
	}
	for _, k := range []string{"date", "updated", "created", "status", "category"} {
		if v := fm.AsString(k, ""); v != "" {
			attrs[k] = v
		}
	}
	label := fm.AsString("title", "")
	if label == "" {
		label = firstHeading(fm.Body)
	}
	if label == "" {
		label = strings.TrimSuffix(path.Base(src.Path), ".md")
	}
	frag := Fragment{Nodes: []Node{{
		ID: pid, Label: label, FileType: TypeWikiPage,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: attrs,
	}}}

	// derived_from from the frontmatter sources: list (provenance).
	for _, s := range fm.AsStringSlice("sources") {
		addDerivedFrom(&frag, src, pid, s)
	}

	// Body: [[wikilinks]], raw/ provenance links, and other cited repo paths.
	// Commented-out scaffold examples are stripped so they are never parsed.
	body := stripHTMLComments(textutil.NormalizeNewlines(string(src.Content)))
	for i, raw := range strings.Split(body, "\n") {
		for _, m := range reWikiLink.FindAllStringSubmatch(raw, -1) {
			text := strings.TrimSpace(m[1])
			if strings.ContainsAny(text, "<>") {
				continue // a placeholder like [[<slug>]], not a real link
			}
			frag.Edges = append(frag.Edges, Edge{
				Source: pid, Target: wikiTargetID(text), Relation: RelLinksTo,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
				Ref: text,
			})
		}
		for _, cited := range citedPathsInline(raw) {
			if isRawSourcePath(cited) {
				addDerivedFrom(&frag, src, pid, cited)
				continue
			}
			emitCitedPath(src, i, cited, pid, &frag)
		}
	}
	return []Fragment{frag}
}

// wikiTargetID resolves a [[wikilink]] target text to a wiki_page ID. A link may
// carry a path (docs/wiki/pages/foo.md) or a bare title (Foo); both reduce to the
// base name, normalized.
func wikiTargetID(text string) string {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "/") {
		text = path.Base(text)
	}
	text = strings.TrimSuffix(text, ".md")
	return MakeID("wiki", text)
}

// addDerivedFrom appends a derived_from edge from a page to the raw source it was
// derived from, resolving the reference to a raw_source node ID.
func addDerivedFrom(frag *Fragment, src Source, pageID, rawRef string) {
	rawRef = strings.TrimSpace(rawRef)
	if rawRef == "" {
		return
	}
	frag.Edges = append(frag.Edges, Edge{
		Source: pageID, Target: rawSourceID(rawRef), Relation: RelDerivedFrom,
		Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path, Ref: rawRef,
	})
}

var reHeading = regexp.MustCompile(`^#{1,6}\s+(.*)$`)

// firstHeading returns the text of the first markdown heading in a body, or "".
func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if m := reHeading.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return collapseSpaces(m[1])
		}
	}
	return ""
}
