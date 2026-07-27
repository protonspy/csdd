package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// citingWorkspace is a workspace whose plan cites every kind of reference, with
// one of each failure mode deliberately present: a wikilink to nothing, a
// citation of a superseded record, and a technology absent from the contract.
func citingWorkspace(t *testing.T) string {
	t.Helper()
	return tempWorkspace(t, map[string]string{
		"CLAUDE.md": "# c\n",
		"docs/plans/photo-sharing/plan.md": "---\nname: Photo sharing\nstatus: draft\n---\n\n" +
			"## Feats\n\n" +
			"| # | Feat | Objective | Depends | Milestone | (P) | Refs |\n" +
			"|---|---|---|---|---|---|---|\n" +
			"| 1 | album-sharing | Share an album | — | M1 | | [[storage-design]] adr:signed-urls stack:Go |\n" +
			"| 2 | link-expiry | Expire a link | album-sharing | M1 | P | [[nowhere]] adr:url-signing-v1 stack:redis |\n\n" +
			"## Quality Gates\n\n- tests: go test ./...\n",
		"docs/wiki/pages/storage-design.md": "---\ntitle: Storage design\n---\n\n# Storage design\n\nContent-addressed.\n",
		"docs/adr/0001-url-signing-v1.md": "---\nstatus: superseded\nsuperseded-by: 2\n---\n\n" +
			"# Public links are signed URLs with a global TTL\n\nThe first cut.\n",
		"docs/adr/0002-signed-urls.md": "# Signed URLs with a per-share key\n\n" +
			"A global TTL could not express revocation.\n",
		"docs/stack.md": "# Tech contract\n\n## Decided\n\n" +
			"| Domain | Choice | Version | Why | Refs |\n|---|---|---|---|---|\n" +
			"| Language | Go | 1.24 | One static binary. | [[storage-design]] |\n",
		// Terms live under `## Language` — that heading is what the parser keys on.
		"docs/glossary.md": "# Glossary\n\n## Language\n\n### Access\n\n" +
			"**Public link**: a share whose recipient is anyone holding the URL.\n" +
			"_Avoid_: magic link, anonymous link\n",
		"specs/album-sharing/spec.json": `{"feature_name":"album-sharing","phase":"tasks-approved"}`,
	})
}

func TestParseRefToken(t *testing.T) {
	cases := []struct{ in, kind, target string }{
		{"[[storage-design]]", "wiki", "storage-design"},
		{"[[storage-design|Storage]]", "wiki", "storage-design"},
		{"[[storage-design#Layout]]", "wiki", "storage-design"},
		{"adr:signed-urls", "adr", "signed-urls"},
		{"stack:postgres", "stack", "postgres"},
		{"spec:album-sharing", "spec", "album-sharing"},
		{"feat:link-expiry", "feat", "link-expiry"},
		{"term:Public link", "term", "Public link"},
		// Not citations: the resolver has to say so rather than guess.
		{"adr:", "", ""},
		{"just-a-word", "", ""},
		{"[[]]", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		kind, target := parseRefToken(c.in)
		if kind != c.kind || target != c.target {
			t.Errorf("parseRefToken(%q) = (%q, %q), want (%q, %q)", c.in, kind, target, c.kind, c.target)
		}
	}
}

// A citation resolves to a state and a place to go. These are the four outcomes
// the UI has to render differently, so each one is pinned.
func TestResolveRefStates(t *testing.T) {
	root := citingWorkspace(t)
	got := map[string]refResolution{}
	for _, r := range resolveRefs(root, []string{
		"[[storage-design]]", "[[nowhere]]", "adr:signed-urls", "adr:url-signing-v1",
		"stack:Go", "stack:redis", "spec:album-sharing", "spec:no-such-spec",
		"feat:link-expiry", "term:Public link", "term:gallery", "adr:Not A Slug",
	}) {
		got[r.Token] = r
	}

	want := map[string]struct{ state, route string }{
		"[[storage-design]]": {refOK, "#/wiki/storage-design"},
		"[[nowhere]]":        {refBroken, ""},
		"adr:signed-urls":    {refOK, "#/adr/signed-urls"},
		"adr:url-signing-v1": {refSuperseded, "#/adr/url-signing-v1"},
		"stack:Go":           {refOK, "#/stack?row=go"},
		"stack:redis":        {refBroken, ""},
		"spec:album-sharing": {refOK, "#/specs/album-sharing"},
		"spec:no-such-spec":  {refBroken, ""},
		"feat:link-expiry":   {refOK, "#/plans/photo-sharing?feat=link-expiry"},
		"term:Public link":   {refOK, "#/glossary?term=Public+link"},
		"term:gallery":       {refBroken, ""},
		"adr:Not A Slug":     {refBroken, ""},
	}
	for token, w := range want {
		g, ok := got[token]
		if !ok {
			t.Fatalf("%s was not resolved at all", token)
		}
		if g.State != w.state {
			t.Errorf("%s state = %q, want %q (%s)", token, g.State, w.state, g.Title)
		}
		if g.Route != w.route {
			t.Errorf("%s route = %q, want %q", token, g.Route, w.route)
		}
	}

	// A superseded record must hand the reader its replacement, or the state is
	// a dead end: the whole point is that the decision moved, not vanished.
	if s := got["adr:url-signing-v1"].Successor; s != "adr:signed-urls" {
		t.Errorf("superseded successor = %q, want adr:signed-urls", s)
	}
	// A resolved citation carries enough to preview without a second request.
	if got["adr:signed-urls"].Title == "" || got["adr:signed-urls"].Body == "" {
		t.Errorf("a resolved ADR should carry its title and body: %+v", got["adr:signed-urls"])
	}
	if !strings.HasPrefix(got["adr:signed-urls"].Meta, "docs/adr/") {
		t.Errorf("meta should be the record's own path, got %q", got["adr:signed-urls"].Meta)
	}
}

// A workspace with nothing in it must answer "broken", never crash: every
// resolver reads directories that may not exist.
func TestResolveRefsOnBareWorkspace(t *testing.T) {
	root := tempWorkspace(t, map[string]string{"CLAUDE.md": "# c\n"})
	for _, r := range resolveRefs(root, []string{"[[x]]", "adr:x", "stack:x", "spec:x", "feat:x", "term:x"}) {
		if r.State != refBroken {
			t.Errorf("%s on a bare workspace = %q, want broken", r.Token, r.State)
		}
		if r.Route != "" {
			t.Errorf("%s should route nowhere, got %q", r.Token, r.Route)
		}
	}
}

// A spec citation must not be able to walk out of specs/.
func TestResolveSpecRejectsTraversal(t *testing.T) {
	root := citingWorkspace(t)
	for _, evil := range []string{"../../etc", "..", "a/b", `..\..\windows`} {
		r := resolveRefs(root, []string{"spec:" + evil})[0]
		if r.State != refBroken || r.Route != "" {
			t.Errorf("spec:%s resolved to %+v, want a broken ref with no route", evil, r)
		}
	}
}

func TestRefEndpointBatches(t *testing.T) {
	srv := testServer(t, citingWorkspace(t))
	q := url.Values{}
	q.Add("token", "adr:signed-urls")
	q.Add("token", "[[nowhere]]")
	q.Add("token", "adr:signed-urls") // repeated: answered again, resolved once
	var out []refResolution
	if code := getJSON(t, srv.URL+"/api/ref?"+q.Encode(), &out); code != http.StatusOK {
		t.Fatalf("GET /api/ref = %d", code)
	}
	if len(out) != 3 {
		t.Fatalf("want one resolution per token in order, got %d", len(out))
	}
	if out[0].Token != "adr:signed-urls" || out[1].Token != "[[nowhere]]" || out[2].Token != "adr:signed-urls" {
		t.Errorf("resolutions came back out of order: %+v", out)
	}
	if out[0].State != refOK || out[1].State != refBroken {
		t.Errorf("states = %q, %q", out[0].State, out[1].State)
	}
}

func TestKnowledgeEndpoints(t *testing.T) {
	srv := testServer(t, citingWorkspace(t))

	var adr adrOverview
	if code := getJSON(t, srv.URL+"/api/adr", &adr); code != http.StatusOK {
		t.Fatalf("GET /api/adr = %d", code)
	}
	if !adr.Present || len(adr.Records) != 2 {
		t.Fatalf("want 2 records from a present docs/adr/, got present=%v n=%d", adr.Present, len(adr.Records))
	}
	if adr.Records[0].Number != 1 || adr.Records[1].Number != 2 {
		t.Errorf("records should come back in number order, got %d then %d", adr.Records[0].Number, adr.Records[1].Number)
	}
	// The superseded record points at its successor by slug, not just by number.
	if adr.Records[0].SupersededBySlug != "signed-urls" {
		t.Errorf("superseded_by_slug = %q, want signed-urls", adr.Records[0].SupersededBySlug)
	}
	// cited_by is the reverse of the citation — the question a reader actually has.
	if len(adr.Records[0].CitedBy) != 1 || adr.Records[0].CitedBy[0] != "feat:link-expiry" {
		t.Errorf("cited_by = %v, want [feat:link-expiry]", adr.Records[0].CitedBy)
	}

	var stack stackOverview
	if code := getJSON(t, srv.URL+"/api/stack", &stack); code != http.StatusOK {
		t.Fatalf("GET /api/stack = %d", code)
	}
	if !stack.Present || len(stack.Rows) != 1 {
		t.Fatalf("want the one Decided row, got present=%v n=%d", stack.Present, len(stack.Rows))
	}
	row := stack.Rows[0]
	if row.Name != "go" || row.Choice != "Go" || row.Version != "1.24" {
		t.Errorf("row = %+v", row)
	}
	// The Refs cell is tokenised, so the UI links it like any other citation.
	if len(row.Refs) != 1 || row.Refs[0] != "[[storage-design]]" {
		t.Errorf("row refs = %v, want [[[storage-design]]]", row.Refs)
	}
	if len(row.CitedBy) != 1 || row.CitedBy[0] != "feat:album-sharing" {
		t.Errorf("row cited_by = %v, want [feat:album-sharing]", row.CitedBy)
	}

	var gloss glossaryOverview
	if code := getJSON(t, srv.URL+"/api/glossary", &gloss); code != http.StatusOK {
		t.Fatalf("GET /api/glossary = %d", code)
	}
	if !gloss.Present || len(gloss.Terms) != 1 {
		t.Fatalf("want the one term, got present=%v n=%d", gloss.Present, len(gloss.Terms))
	}
	if gloss.Terms[0].Canonical != "Public link" || len(gloss.Terms[0].Avoid) != 2 {
		t.Errorf("term = %+v", gloss.Terms[0])
	}
}

// An absent knowledge base is a normal state, not an error: the endpoints say
// "not present" so the UI can explain how to create it.
func TestKnowledgeEndpointsOnBareWorkspace(t *testing.T) {
	srv := testServer(t, tempWorkspace(t, map[string]string{"CLAUDE.md": "# c\n"}))
	var adr adrOverview
	getJSON(t, srv.URL+"/api/adr", &adr)
	var stack stackOverview
	getJSON(t, srv.URL+"/api/stack", &stack)
	var gloss glossaryOverview
	getJSON(t, srv.URL+"/api/glossary", &gloss)
	if adr.Present || stack.Present || gloss.Present {
		t.Errorf("nothing should report present: adr=%v stack=%v glossary=%v", adr.Present, stack.Present, gloss.Present)
	}
	// Non-nil slices, so the UI can map over them without a null guard.
	if adr.Records == nil || stack.Rows == nil || gloss.Terms == nil {
		t.Errorf("empty collections must marshal as [], not null")
	}
}

// The plan detail carries each feat's citations verbatim; without this the Refs
// column has nothing to render.
func TestPlanFeatsCarryRefs(t *testing.T) {
	srv := testServer(t, citingWorkspace(t))
	var d planDetail
	if code := getJSON(t, srv.URL+"/api/plan/photo-sharing", &d); code != http.StatusOK {
		t.Fatalf("GET /api/plan = %d", code)
	}
	if len(d.Feats) != 2 {
		t.Fatalf("want 2 feats, got %d", len(d.Feats))
	}
	want := []string{"[[storage-design]]", "adr:signed-urls", "stack:Go"}
	if strings.Join(d.Feats[0].Refs, " ") != strings.Join(want, " ") {
		t.Errorf("feat 1 refs = %v, want %v", d.Feats[0].Refs, want)
	}
	if d.Feats[1].Refs == nil {
		t.Errorf("refs must never be null")
	}
}

// A plan reads as complete only when every feat is delivered — the state the
// Plans list, the rail and the Overview all render, derived once so they cannot
// disagree.
func TestPlanCompletion(t *testing.T) {
	// Two feats, both delivered: every phase approved and every task checked.
	deliveredSpec := func(name string) map[string]string {
		return map[string]string{
			"specs/" + name + "/spec.json": `{"feature_name":"` + name + `","phase":"tasks-approved",` +
				`"ready_for_implementation":true,"approvals":{"requirements":{"generated":true,"approved":true},` +
				`"design":{"generated":true,"approved":true},"tasks":{"generated":true,"approved":true}}}`,
			"specs/" + name + "/tasks.md": "## Phase 1: Foundation\n\n- [x] 1. done\n  - _Requirements: 1.1_\n",
		}
	}
	files := map[string]string{
		"CLAUDE.md": "# c\n",
		"docs/plans/finished/plan.md": "---\nname: Finished\n---\n\n## Feats\n\n" +
			"| # | Feat | Objective | Depends | Milestone | (P) | Refs |\n|---|---|---|---|---|---|---|\n" +
			"| 1 | alpha | First | — | M1 | | |\n| 2 | beta | Second | alpha | M1 | | |\n\n" +
			"## Quality Gates\n\n- tests: go test ./...\n",
		"docs/plans/partial/plan.md": "---\nname: Partial\n---\n\n## Feats\n\n" +
			"| # | Feat | Objective | Depends | Milestone | (P) | Refs |\n|---|---|---|---|---|---|---|\n" +
			"| 1 | alpha | First | — | M1 | | |\n| 2 | gamma | Not started | alpha | M1 | | |\n\n" +
			"## Quality Gates\n\n- tests: go test ./...\n",
		// A plan with no feats at all: unstarted, which is not the same as finished.
		"docs/plans/empty/plan.md": "---\nname: Empty\n---\n\n## Feats\n\n" +
			"| # | Feat | Objective | Depends | Milestone | (P) | Refs |\n|---|---|---|---|---|---|---|\n\n" +
			"## Quality Gates\n\n- tests: go test ./...\n",
	}
	for k, v := range deliveredSpec("alpha") {
		files[k] = v
	}
	for k, v := range deliveredSpec("beta") {
		files[k] = v
	}
	srv := testServer(t, tempWorkspace(t, files))

	var list []planSummary
	if code := getJSON(t, srv.URL+"/api/plans", &list); code != http.StatusOK {
		t.Fatalf("GET /api/plans = %d", code)
	}
	want := map[string]bool{"finished": true, "partial": false, "empty": false}
	for _, p := range list {
		if got, ok := want[p.Slug]; ok && p.Complete != got {
			t.Errorf("plan %q complete = %v (%d/%d done), want %v", p.Slug, p.Complete, p.Done, p.Feats, got)
		}
	}

	// The per-plan view has to agree with the list, or the badge changes when
	// you click through.
	for slug, wantComplete := range want {
		var d planDetail
		if code := getJSON(t, srv.URL+"/api/plan/"+slug, &d); code != http.StatusOK {
			t.Fatalf("GET /api/plan/%s = %d", slug, code)
		}
		if d.Complete != wantComplete {
			t.Errorf("plan detail %q complete = %v, want %v", slug, d.Complete, wantComplete)
		}
	}
}
