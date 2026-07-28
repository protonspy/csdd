package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/workspace"
)

// The enrichment pass answers the one question the deterministic brief cannot: what
// does THIS feat actually touch in THIS repository. Everything the plan already
// states — the feat row, its declared refs, the gates, the Executor Notes — stays
// derived, because there it is exact and free. The pass covers only what has to be
// discovered by reading the tree, which is otherwise discovered by the session's own
// orchestrator model, once per attempt, at orchestrator prices.
//
// It answers into a SCHEMA rather than prose, and that is the load-bearing decision
// rather than a serialization detail. Asked for "a prompt for this feat", both a
// cheap and a strong model re-emitted the development process — and the stronger one
// re-emitted it worse, because it faithfully reproduced the INTERACTIVE contract
// from the repository's CLAUDE.md: create a branch, get a human to approve each
// phase, open a PR. Every one of those instructions contradicts the plan loop, and
// the "approve with the human" one deadlocks a session that has no human. A schema
// with no free-text field cannot carry any of it — the failure is removed by
// construction rather than forbidden by wording.

// EnrichPack is the discovered half of a feat's brief. Every field is bounded by
// enrichSchema and every citation is checked against the workspace before the pack
// is stored (see VerifyPack), so a hallucinated path or decision never reaches the
// session as though the runner had vouched for it.
type EnrichPack struct {
	Touches   []PackTouch    `json:"touches"`
	Governors []PackGovernor `json:"governors"`
	Exists    []string       `json:"exists"`
	Missing   []string       `json:"missing"`
	Flow      PackFlow       `json:"flow"`
	Traps     []string       `json:"traps"`
	// Key fingerprints the inputs the pack was derived from. It is written by the
	// runner, never by the model, and is what makes the pack a cache rather than a
	// per-attempt cost: a feat's second attempt reuses the first attempt's pack
	// unless the plan row that produced it changed.
	Key string `json:"key,omitempty"`
}

// PackTouch is one file or directory the feat changes, and what changes in it.
type PackTouch struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

// PackGovernor is one decision or stack row that constrains the feat. Declared
// says whether the plan's own Refs column named it: `spec validate` requires the
// design to cite the DECLARED ones, so a discovered governor is context the session
// may use and must not be blocked on citing.
type PackGovernor struct {
	ID         string `json:"id"`
	Constraint string `json:"constraint"`
	Declared   bool   `json:"declared"`
}

// PackFlow is the enricher's reading of which development_flow fits the feat. It is
// advice, not an instruction — the flow is the session's decision, and the brief
// renders it as such.
type PackFlow struct {
	Choice string `json:"choice"`
	Why    string `json:"why"`
}

// Empty reports whether the pack carries nothing worth rendering.
func (p *EnrichPack) Empty() bool {
	return p == nil || (len(p.Touches) == 0 && len(p.Governors) == 0 && len(p.Exists) == 0 &&
		len(p.Missing) == 0 && len(p.Traps) == 0 && strings.TrimSpace(p.Flow.Choice) == "")
}

// enrichSchemaVersion is bumped whenever enrichSchema or the enricher prompt
// changes in a way that makes a stored pack the wrong shape. It is part of the
// cache key, so a bump invalidates every pack on disk without anyone deleting one.
const enrichSchemaVersion = "1"

// enrichSchema is the contract the enricher's answer must satisfy. The bounds are
// the pack's size ceiling: at these caps a full pack renders to roughly 3 KB, which
// is what the brief can afford to spend on discovery. There is deliberately no
// free-text field — see the note at the top of this file.
const enrichSchema = `{"type":"object","additionalProperties":false,` +
	`"required":["touches","governors","exists","missing","flow","traps"],"properties":{` +
	`"touches":{"type":"array","maxItems":8,"items":{"type":"object","additionalProperties":false,` +
	`"required":["path","why"],"properties":{"path":{"type":"string","maxLength":120},` +
	`"why":{"type":"string","maxLength":200}}}},` +
	`"governors":{"type":"array","maxItems":6,"items":{"type":"object","additionalProperties":false,` +
	`"required":["id","constraint","declared"],"properties":{"id":{"type":"string","pattern":"^(adr|stack):","maxLength":120},` +
	`"constraint":{"type":"string","maxLength":200},"declared":{"type":"boolean"}}}},` +
	`"exists":{"type":"array","maxItems":6,"items":{"type":"string","maxLength":200}},` +
	`"missing":{"type":"array","maxItems":6,"items":{"type":"string","maxLength":200}},` +
	`"flow":{"type":"object","additionalProperties":false,"required":["choice","why"],` +
	`"properties":{"choice":{"enum":["unit","tdd","tdd-e2e"]},"why":{"type":"string","maxLength":200}}},` +
	`"traps":{"type":"array","maxItems":6,"items":{"type":"string","maxLength":200}}}}`

// EnrichRequest is what the Enrich hook is handed: a deterministic prompt, the
// schema its answer must satisfy, and the worktree to answer from. The worktree
// matters as much as it does for a session — a feat whose dependencies were merged
// into its tree is describable there and nowhere else.
type EnrichRequest struct {
	Feat   Feat
	Prompt string
	Schema string
	Dir    string
}

// enrichPrompt builds the enricher's instructions for one feat. It is deterministic
// (same plan + feat -> same prompt) and it hands over the feat row verbatim: the
// enricher is told WHICH feat to describe rather than asked to pick one, because
// choosing the next feat belongs to the sequencer and an enricher invited to have an
// opinion about ordering produces instructions to halt.
func enrichPrompt(doc *PlanDoc, feat Feat) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("You are the context enricher for `csdd plan run`. Your output is consumed by a\n")
	w("machine, not read by a human: fill the schema for ONE feat and nothing else.\n\n")
	w("Plan: docs/plans/%s/plan.md\n", doc.Slug)
	w("Feat, verbatim from the `## Feats` table:\n\n")
	w("- slug: %s\n", feat.Slug)
	w("- objective: %s\n", orDash(feat.Objective))
	w("- milestone: %s\n", orDash(feat.Milestone))
	if len(feat.Depends) > 0 {
		w("- depends on: %s\n", strings.Join(feat.Depends, ", "))
	}
	if len(feat.Refs) > 0 {
		w("- declared refs: %s\n", strings.Join(feat.Refs, " "))
	}
	w("\nInvestigate the repository before answering — do not answer from memory:\n")
	w("- `csdd graph query <term>` and `csdd graph explain <id>` to find the nodes\n")
	w("- read docs/adr/, docs/stack.md, docs/wiki/pages/ and the code the feat touches\n")
	w("- check what the feats delivered before this one already left in the tree\n\n")
	w("Rules — breaking any one of them invalidates the whole pack:\n\n")
	w("- Do NOT describe process. No workflow, no phase order, no `spec init`, no branch,\n")
	w("  commit, PR, `/clear`, human approval, gates or commands to run. Whoever delivers\n")
	w("  the feat already has that contract; repeating it here contradicts the runner.\n")
	w("- Do not comment on ordering or blockers. The scheduler decides when the feat runs.\n")
	w("- `touches[].path` — a file or directory that EXISTS today, relative to the\n")
	w("  workspace root. `why` says what the feat changes in it.\n")
	w("- `governors[].id` — `adr:<file-slug-without-the-number>` or `stack:<name>` that\n")
	w("  really resolves. `constraint` is the restriction in one line, not a summary of\n")
	w("  the record. Set `declared: true` only for an id that appears in the declared\n")
	w("  refs above; governors you found yourself carry `declared: false`.\n")
	w("- `exists` / `missing` — what is already in the tree versus what is not, verified,\n")
	w("  so the feat does not redo delivered work.\n")
	w("- `flow.choice` — `unit` unless the feat touches money, auth, tenancy or something\n")
	w("  irreversible. One line of `why`.\n")
	w("- `traps` — concrete pitfalls of this feat in this repository. No generic\n")
	w("  engineering advice.\n")
	w("- Invent nothing: what you did not confirm by reading does not go in.\n")
	return b.String()
}

// PackKey fingerprints everything the pack is derived from. A pack whose key still
// matches is reused untouched; anything else is re-enriched. The plan row is the
// whole input — the tree the enricher reads is not hashed, deliberately: re-reading
// a worktree that moved is exactly what the next feat's own pass does, and hashing
// the tree would re-enrich every feat after every merge for no gain.
func PackKey(doc *PlanDoc, feat Feat) string {
	h := sha256.New()
	fmt.Fprintf(h, "v%s\n%s\n%s\n%s\n%s\n%s\n%s\n", enrichSchemaVersion, doc.Slug,
		feat.Slug, feat.Objective, feat.Milestone,
		strings.Join(feat.Depends, ","), strings.Join(feat.Refs, " "))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// packPath is where a feat's pack lives: under the runner's own transient state, not
// in docs/. The pack is a derived cache the CLI can rebuild at will, and .csdd/ is
// where derived, regenerable, never-committed state belongs.
func packPath(root, slug, feat string) string {
	return filepath.Join(stateDir(root, slug), "briefs", feat+".json")
}

// LoadPack reads a feat's stored pack. A missing or unreadable pack is not an error
// — the brief renders without it — so callers get (nil, nil) rather than a failure
// they would all have to ignore.
func LoadPack(root, slug, feat string) (*EnrichPack, error) {
	data, err := os.ReadFile(packPath(root, slug, feat))
	if err != nil {
		return nil, nil
	}
	var p EnrichPack
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("the stored context pack for %s is not valid JSON: %w", feat, err)
	}
	return &p, nil
}

// SavePack stores a verified pack.
func SavePack(root, slug, feat string, p *EnrichPack) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return workspace.AtomicWrite(packPath(root, slug, feat), append(data, '\n'), 0o644)
}

// RemovePack forgets a feat's stored pack, so the next EnsurePack re-runs the
// discovery pass. A pack that was never there is not an error — the caller wanted it
// gone, and it is.
func RemovePack(root, slug, feat string) error {
	if err := os.Remove(packPath(root, slug, feat)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ParsePack reads a pack out of whatever the enricher returned — the bare object,
// the `--output-format json` envelope, or an object embedded in text.
func ParsePack(output []byte) (*EnrichPack, error) {
	var p EnrichPack
	if json.Unmarshal(output, &p) == nil && !p.Empty() {
		return &p, nil
	}
	var env struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(output, &env) == nil && env.Result != "" {
		if json.Unmarshal([]byte(env.Result), &p) == nil && !p.Empty() {
			return &p, nil
		}
		if m := reJSONObject.FindString(env.Result); m != "" {
			if json.Unmarshal([]byte(m), &p) == nil && !p.Empty() {
				return &p, nil
			}
		}
	}
	if m := reJSONObject.FindString(string(output)); m != "" {
		if json.Unmarshal([]byte(m), &p) == nil && !p.Empty() {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("could not parse a context pack from the enricher output")
}

// VerifyPack drops every claim the workspace does not corroborate and returns what
// it dropped. This is the half of the design that keeps a generated pack honest: a
// path that does not exist or a decision that does not resolve reads as authoritative
// to the session precisely because the runner put it there, which makes an unchecked
// citation worse than a missing one. Resolution goes through the same helpers the
// validators use — ScanADRs and decidedRows — so the pack cannot disagree with
// `plan validate` about what a governor is.
//
// It also corrects `declared` rather than trusting it: whether the plan named a
// governor is a fact about the feat row, not a judgment call, and getting it wrong
// in either direction misleads about what the design must cite.
func VerifyPack(root string, feat Feat, p *EnrichPack) []string {
	if p == nil {
		return nil
	}
	var dropped []string
	declared := declaredRefs(feat)

	touches := p.Touches[:0]
	for _, t := range p.Touches {
		rel := strings.TrimSpace(t.Path)
		if rel == "" || !safeRelPath(rel) {
			dropped = append(dropped, "touches "+t.Path+" (unsafe path)")
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			dropped = append(dropped, "touches "+rel+" (does not exist)")
			continue
		}
		t.Path = rel
		touches = append(touches, t)
	}
	p.Touches = touches

	adrs := ScanADRs(root)
	rows := decidedRows(root)
	govs := p.Governors[:0]
	for _, g := range p.Governors {
		id := strings.TrimSpace(g.ID)
		switch {
		case strings.HasPrefix(id, "adr:"):
			if _, res := adrs.Resolve(strings.TrimPrefix(id, "adr:")); res != ADRResolved {
				dropped = append(dropped, id+" (does not resolve to a docs/adr record)")
				continue
			}
		case strings.HasPrefix(id, "stack:"):
			if _, ok := rows[normalizeTechName(strings.TrimPrefix(id, "stack:"))]; !ok {
				dropped = append(dropped, id+" (not in the Decided stack table)")
				continue
			}
		default:
			dropped = append(dropped, id+" (not an adr: or stack: reference)")
			continue
		}
		g.ID = id
		g.Declared = declared[strings.ToLower(id)]
		govs = append(govs, g)
	}
	p.Governors = govs

	if p.Flow.Choice != "" && !validFlow(p.Flow.Choice) {
		dropped = append(dropped, "flow "+p.Flow.Choice+" (not a development_flow)")
		p.Flow = PackFlow{}
	}
	return dropped
}

// safeRelPath reports whether a model-authored path is a workspace-relative one.
// The pack is the one part of the brief a language model wrote, so its paths get the
// same treatment as any other hostile input: an absolute path or one that climbs out
// with `..` is dropped rather than stat-ed and rendered, which would put a file from
// outside the workspace in front of the session as though the runner had vouched for
// it.
func safeRelPath(rel string) bool {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return false
	}
	if vol := filepath.VolumeName(rel); vol != "" {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// declaredRefs indexes the feat row's own Refs column, lowercased, so a governor's
// `declared` flag is decided by the plan rather than by the enricher.
func declaredRefs(feat Feat) map[string]bool {
	out := map[string]bool{}
	for _, slug := range feat.ADRRefs {
		out["adr:"+strings.ToLower(strings.TrimSpace(slug))] = true
	}
	for _, name := range feat.StackRefs {
		out["stack:"+strings.ToLower(strings.TrimSpace(name))] = true
	}
	return out
}

// validFlow reports whether s is a development_flow the spec layer accepts.
func validFlow(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "unit", "tdd", "tdd-e2e":
		return true
	}
	return false
}

// EnsurePack returns the feat's context pack, enriching only when there is no
// current one. It never fails a caller: enrichment is an optimization over the
// deterministic brief, and a run that dies because a cheap discovery pass errored
// would be a strictly worse trade than a run whose brief carries less context. Every
// failure is reported through logf and swallowed, and the brief renders without it.
func EnsurePack(root string, doc *PlanDoc, feat Feat, dir string, enrich func(EnrichRequest) (string, error), logf func(string, ...any)) *EnrichPack {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	key := PackKey(doc, feat)
	if p, err := LoadPack(root, doc.Slug, feat.Slug); err == nil && p != nil && p.Key == key {
		return p
	}
	if enrich == nil {
		return nil
	}
	out, err := enrich(EnrichRequest{
		Feat:   feat,
		Prompt: enrichPrompt(doc, feat),
		Schema: enrichSchema,
		Dir:    dir,
	})
	if err != nil {
		logf("  · context enrichment for %s failed (%v); briefing without it", feat.Slug, err)
		return nil
	}
	p, err := ParsePack([]byte(out))
	if err != nil {
		logf("  · context enrichment for %s returned nothing usable; briefing without it", feat.Slug)
		return nil
	}
	if dropped := VerifyPack(root, feat, p); len(dropped) > 0 {
		logf("  · context pack for %s: dropped %d unverifiable claim(s): %s",
			feat.Slug, len(dropped), strings.Join(dropped, "; "))
	}
	if p.Empty() {
		logf("  · nothing in the context pack for %s survived verification; briefing without it", feat.Slug)
		return nil
	}
	p.Key = key
	if err := SavePack(root, doc.Slug, feat.Slug, p); err != nil {
		// Worth saying out loud: the pack is still used for this session, but the
		// next attempt at this feat will pay for it again.
		logf("  · could not store the context pack for %s (%v); the next attempt will re-enrich", feat.Slug, err)
	}
	return p
}
