package web

import (
	"net/http"
	"testing"
)

func wikiWorkspace(t *testing.T) string {
	return tempWorkspace(t, map[string]string{
		// The index.md catalog is the structuring file: it sets categories, order,
		// and which pages are official. Guides is listed before Concepts on purpose,
		// to prove the view follows index order, not alphabetical.
		"docs/wiki/index.md": `# Wiki index

## Guides
- [Album Service](pages/album-service.md) — the album boundary.

## Concepts
- [Postgres Notes](pages/postgres-notes.md) — persistence.
<!-- - [Example](pages/example.md) — commented scaffold, must be ignored. -->
`,
		"docs/wiki/pages/album-service.md": `---
title: Album Service
tags: [domain]
sources:
  - paper.pdf
---
# Album Service
Owns the album lifecycle. See [[postgres-notes]] and [[missing-page]].
`,
		"docs/wiki/pages/postgres-notes.md": `---
title: Postgres Notes
---
# Postgres Notes
Schema conventions. Back to [[album-service]].
`,
		// A page on disk but absent from the index → last group, not-in-index.
		"docs/wiki/pages/scratch.md": "# Scratch\nUnlisted page.\n",
		"docs/wiki/log.md":           "# Log\n",
		"docs/raw/paper.pdf":         "bytes",
		"docs/raw/README.md":         "dropzone explainer, must be skipped",
	})
}

func TestAPIWiki(t *testing.T) {
	srv := testServer(t, wikiWorkspace(t))
	var ov wikiOverview
	if code := getJSON(t, srv.URL+"/api/wiki", &ov); code != http.StatusOK {
		t.Fatalf("GET /api/wiki = %d", code)
	}
	if !ov.Present || !ov.HasIndex {
		t.Fatalf("expected present wiki with index; got %+v", ov)
	}
	// Categories in index order (Guides before Concepts).
	if len(ov.Categories) != 2 || ov.Categories[0] != "Guides" || ov.Categories[1] != "Concepts" {
		t.Fatalf("categories should follow index order [Guides Concepts]; got %v", ov.Categories)
	}
	// Pages ordered by index structure: Guides (album-service), Concepts
	// (postgres-notes), then the unlisted page last.
	if len(ov.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(ov.Pages))
	}
	if ov.Pages[0].Slug != "album-service" || ov.Pages[1].Slug != "postgres-notes" || ov.Pages[2].Slug != "scratch" {
		t.Fatalf("page order should follow the index: %v", []string{ov.Pages[0].Slug, ov.Pages[1].Slug, ov.Pages[2].Slug})
	}

	album := ov.Pages[0]
	if album.Title != "Album Service" || album.Category != "Guides" || !album.InIndex {
		t.Errorf("album-service metadata wrong: %+v", album)
	}
	if len(album.Sources) != 1 || album.Sources[0] != "paper.pdf" {
		t.Errorf("block-sequence sources not parsed: %v", album.Sources)
	}
	// Links resolve: postgres-notes → a real page, missing-page → broken.
	var toPostgres, toMissing *wikiLink
	for i := range album.Links {
		switch album.Links[i].Text {
		case "postgres-notes":
			toPostgres = &album.Links[i]
		case "missing-page":
			toMissing = &album.Links[i]
		}
	}
	if toPostgres == nil || toPostgres.Target != "postgres-notes" || toPostgres.Broken {
		t.Errorf("expected resolved link to postgres-notes; got %+v", toPostgres)
	}
	if toMissing == nil || !toMissing.Broken || toMissing.Target != "" {
		t.Errorf("expected broken link for missing-page; got %+v", toMissing)
	}

	// The unlisted page sorts last and is flagged not-in-index.
	if ov.Pages[2].InIndex {
		t.Errorf("scratch page must be flagged not-in-index")
	}
	// Raw sources exclude the README dropzone explainer.
	if len(ov.RawSources) != 1 || ov.RawSources[0] != "paper.pdf" {
		t.Errorf("raw sources should be [paper.pdf]; got %v", ov.RawSources)
	}
}

func TestAPIWikiAbsent(t *testing.T) {
	srv := testServer(t, tempWorkspace(t, map[string]string{"CLAUDE.md": "# x\n"}))
	var ov wikiOverview
	if code := getJSON(t, srv.URL+"/api/wiki", &ov); code != http.StatusOK {
		t.Fatalf("GET /api/wiki = %d", code)
	}
	if ov.Present {
		t.Errorf("wiki should be absent when docs/wiki/ does not exist")
	}
}
