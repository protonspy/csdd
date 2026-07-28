package plan

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// enrichPlan carries one feat whose Refs declare a stack row and a wiki page, so a
// pack's `declared` flag has both a true and a false case to get wrong.
const enrichPlan = `---
name: p
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest and store photos | — | M1 | | stack:go [[storage-design]] |
`

// errStub is any error the enricher might return; only its presence matters.
var errStub = errors.New("the enricher went away")

func enrichFeat(t *testing.T, root string) (*PlanDoc, Feat) {
	t.Helper()
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := doc.Feat("upload")
	if !ok {
		t.Fatal("feat upload not in plan")
	}
	return doc, f
}

// TestVerifyPackDropsUnverifiableClaims is the guarantee that makes a generated
// pack safe to put in a brief: a claim the workspace does not corroborate is
// dropped, not rendered.
//
// An unchecked citation is worse than a missing one — the session trusts it
// precisely because the runner handed it over — and a model asked to describe a
// repository will occasionally name a file that sounds like it should exist.
func TestVerifyPackDropsUnverifiableClaims(t *testing.T) {
	root := setupWorkspace(t, "p", enrichPlan)
	writeADRs(t, root, map[string]string{"0001-pick-store.md": "# Pick the store\n\nbody\n"})
	_, feat := enrichFeat(t, root)

	p := &EnrichPack{
		Touches: []PackTouch{
			{Path: "docs/stack.md", Why: "real"},
			{Path: "docs/nowhere.md", Why: "invented"},
			{Path: "../outside.md", Why: "climbs out"},
		},
		Governors: []PackGovernor{
			{ID: "adr:pick-store", Constraint: "resolves"},
			{ID: "adr:never-written", Constraint: "does not resolve"},
			{ID: "stack:go", Constraint: "decided", Declared: false},
			{ID: "stack:rust", Constraint: "not decided"},
			{ID: "wiki:storage-design", Constraint: "wrong prefix"},
		},
		Flow: PackFlow{Choice: "vibes", Why: "not a flow"},
	}
	dropped := VerifyPack(root, feat, p)

	if len(p.Touches) != 1 || p.Touches[0].Path != "docs/stack.md" {
		t.Errorf("only the existing path should survive, got %+v", p.Touches)
	}
	if len(p.Governors) != 2 {
		t.Fatalf("only the resolvable governors should survive, got %+v", p.Governors)
	}
	// `declared` is a fact about the feat row, not the enricher's opinion: the plan
	// declares stack:go and nothing else, and VerifyPack corrects both directions.
	for _, g := range p.Governors {
		want := g.ID == "stack:go"
		if g.Declared != want {
			t.Errorf("%s: declared=%v, want %v (the feat row decides this)", g.ID, g.Declared, want)
		}
	}
	if p.Flow.Choice != "" {
		t.Errorf("an invalid development_flow should be dropped, got %q", p.Flow.Choice)
	}
	if len(dropped) != 6 {
		t.Errorf("every drop should be reported, got %d: %v", len(dropped), dropped)
	}
}

// TestEnsurePackIsCachedPerFeat: discovery is paid once per feat, not once per
// attempt. A feat that comes back for a second session reuses the pack its first
// session was briefed with — the point of storing it at all.
func TestEnsurePackIsCachedPerFeat(t *testing.T) {
	root := setupWorkspace(t, "p", enrichPlan)
	doc, feat := enrichFeat(t, root)
	calls := 0
	enrich := func(EnrichRequest) (string, error) {
		calls++
		return `{"touches":[{"path":"docs/stack.md","why":"the contract"}],"governors":[],` +
			`"exists":[],"missing":[],"flow":{"choice":"unit","why":"docs"},"traps":[]}`, nil
	}

	first := EnsurePack(root, doc, feat, root, enrich, nil)
	if first == nil || len(first.Touches) != 1 {
		t.Fatalf("first pass should produce a pack, got %+v", first)
	}
	if EnsurePack(root, doc, feat, root, enrich, nil) == nil || calls != 1 {
		t.Errorf("a second dispatch should reuse the stored pack (calls=%d)", calls)
	}
	// A feat whose row changed is a different feat as far as the pack is concerned.
	feat.Objective = "Ingest, store and thumbnail photos"
	if EnsurePack(root, doc, feat, root, enrich, nil); calls != 2 {
		t.Errorf("an edited feat row should re-enrich (calls=%d)", calls)
	}
}

// TestEnsurePackNeverFailsTheDispatch: enrichment is an optimization over the
// deterministic brief. Every way the pass can go wrong has to end with the feat
// still dispatchable — a run that died because a cheap discovery pass errored would
// be strictly worse than one whose brief carried less context.
func TestEnsurePackNeverFailsTheDispatch(t *testing.T) {
	root := setupWorkspace(t, "p", enrichPlan)
	doc, feat := enrichFeat(t, root)

	for name, enrich := range map[string]func(EnrichRequest) (string, error){
		"enricher errored":   func(EnrichRequest) (string, error) { return "", errStub },
		"answer is not JSON": func(EnrichRequest) (string, error) { return "I could not find the feat.", nil },
		"answer is empty":    func(EnrichRequest) (string, error) { return "{}", nil },
		"nothing verifies": func(EnrichRequest) (string, error) {
			return `{"touches":[{"path":"docs/ghost.md","why":"x"}],"governors":[],"exists":[],` +
				`"missing":[],"flow":{"choice":"","why":""},"traps":[]}`, nil
		},
	} {
		var logged []string
		got := EnsurePack(root, doc, feat, root, enrich, func(f string, a ...any) {
			logged = append(logged, f)
		})
		if got != nil {
			t.Errorf("%s: expected no pack, got %+v", name, got)
		}
		if len(logged) == 0 {
			t.Errorf("%s: a failed pass should say so in the run log", name)
		}
		if p, _ := LoadPack(root, "p", "upload"); p != nil {
			t.Errorf("%s: nothing should be stored from a failed pass", name)
		}
	}
	// And with no hook at all — `--enrich-model none` — the pass is simply skipped.
	if EnsurePack(root, doc, feat, root, nil, nil) != nil {
		t.Errorf("a nil hook should disable enrichment, not synthesize a pack")
	}
}

// TestEnrichPromptForbidsProcess guards the prompt against the failure the schema
// was chosen to prevent.
//
// Asked in prose for "a prompt to develop this feat", both a cheap and a strong
// model wrote the development process instead of the context — and the strong one
// wrote it wrong, reproducing the INTERACTIVE contract (create a branch, have a
// human approve each phase, open a PR). "Have a human approve" deadlocks a session
// that has no human. The schema removes the slot; the prompt says so too.
func TestEnrichPromptForbidsProcess(t *testing.T) {
	root := setupWorkspace(t, "p", enrichPlan)
	doc, feat := enrichFeat(t, root)
	prompt := enrichPrompt(doc, feat)

	for _, want := range []string{
		"Do NOT describe process",
		"human approval",
		"Do not comment on ordering",
		"Invent nothing",
		"upload",                      // the feat is handed over, never chosen
		"Ingest and store photos",     // …verbatim from the row
		"stack:go [[storage-design]]", // …with its declared refs, which decide `declared`
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the enricher prompt should carry %q:\n%s", want, prompt)
		}
	}
	// Deterministic: the same plan and feat always ask the same question.
	if enrichPrompt(doc, feat) != prompt {
		t.Errorf("the enricher prompt is not deterministic")
	}
}

// TestEnrichSchemaIsBounded: the pack's size ceiling is the schema's, not a request
// for brevity a model may ignore.
func TestEnrichSchemaIsBounded(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(enrichSchema), &schema); err != nil {
		t.Fatalf("the enrich schema is not valid JSON: %v", err)
	}
	if schema["additionalProperties"] != false {
		t.Errorf("the schema must be closed: a free-text field is where process leaks back in")
	}
	props, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"touches", "governors", "exists", "missing", "flow", "traps"} {
		if _, ok := props[field]; !ok {
			t.Errorf("the schema is missing the %q field", field)
		}
	}
	for _, field := range []string{"touches", "governors", "exists", "missing", "traps"} {
		f, _ := props[field].(map[string]any)
		if _, ok := f["maxItems"]; !ok {
			t.Errorf("%s has no maxItems: the pack would have no size ceiling", field)
		}
	}
}

// TestParsePackReadsEveryEnvelope: the pass may answer with the bare object, the
// `--output-format json` envelope, or an object wrapped in text.
func TestParsePackReadsEveryEnvelope(t *testing.T) {
	body := `{"touches":[{"path":"docs/stack.md","why":"x"}],"governors":[],"exists":[],` +
		`"missing":[],"flow":{"choice":"unit","why":"y"},"traps":[]}`
	envelope, err := json.Marshal(map[string]string{"result": body})
	if err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]string{
		"bare object":      body,
		"json envelope":    string(envelope),
		"wrapped in prose": "Here is the pack:\n" + body + "\nHope that helps.",
	} {
		p, err := ParsePack([]byte(in))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(p.Touches) != 1 {
			t.Errorf("%s: parsed the wrong thing: %+v", name, p)
		}
	}
	if _, err := ParsePack([]byte("no json at all")); err == nil {
		t.Errorf("prose with no object should not parse as a pack")
	}
}

// TestPackPathIsTransientState: a pack is derived, regenerable and never committed,
// so it belongs under .csdd/ with the rest of the run's state — not in docs/, which
// is the human's.
func TestPackPathIsTransientState(t *testing.T) {
	got := filepath.ToSlash(packPath("/w", "p", "upload"))
	if !strings.Contains(got, "/.csdd/") || !strings.HasSuffix(got, "/briefs/upload.json") {
		t.Errorf("unexpected pack path: %s", got)
	}
}
