package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dagPlan is a diamond plus a tail: b and c both need a, d needs both, and e is
// independent. It is the smallest shape that distinguishes a topological order
// from the table order the sequencer used to hand out.
const dagPlan = `---
name: p
status: draft
---

## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | d | D | b, c | M1 | | |
| 2 | b | B | a | M1 | P | |
| 3 | a | A | — | M1 | | |
| 4 | c | C | a | M1 | P | |
| 5 | e | E | — | M1 | | |

## Quality Gates

- verify: make check
`

func dagDoc(t *testing.T) *PlanDoc {
	t.Helper()
	doc := Parse(dagPlan)
	doc.Slug = "p"
	if len(doc.Feats) != 5 {
		t.Fatalf("expected 5 feats, got %d", len(doc.Feats))
	}
	return doc
}

func slugs(feats []Feat) []string {
	out := make([]string, 0, len(feats))
	for _, f := range feats {
		out = append(out, f.Slug)
	}
	return out
}

// TestReadyFeatsRespectsDepends is the core of the change: `d` sits first in the
// table but depends on b and c, so it must not be offered until they land. The old
// sequencer would have handed it out on iteration one.
func TestReadyFeatsRespectsDepends(t *testing.T) {
	doc := dagDoc(t)

	got := slugs(readyFeats(doc, map[string]bool{}, nil, nil))
	if strings.Join(got, ",") != "a,e" {
		t.Errorf("with nothing delivered only the root feats are ready, want [a e] got %v", got)
	}

	// a lands: b and c open up, d still cannot run.
	got = slugs(readyFeats(doc, map[string]bool{"a": true}, nil, nil))
	if strings.Join(got, ",") != "b,c,e" {
		t.Errorf("after a, want [b c e] got %v", got)
	}

	// b and c land: d finally becomes workable.
	got = slugs(readyFeats(doc, map[string]bool{"a": true, "b": true, "c": true}, nil, nil))
	if strings.Join(got, ",") != "d,e" {
		t.Errorf("after a,b,c, want [d e] got %v", got)
	}
}

// TestReadyFeatsKeepsTableOrder pins determinism. The ready set is a scheduling
// input, and a scheduler that reorders itself between identical runs is one nobody
// can reason about.
func TestReadyFeatsKeepsTableOrder(t *testing.T) {
	doc := dagDoc(t)
	for i := 0; i < 20; i++ {
		if got := slugs(readyFeats(doc, map[string]bool{"a": true}, nil, nil)); strings.Join(got, ",") != "b,c,e" {
			t.Fatalf("ready set must be stable in table order, got %v on run %d", got, i)
		}
	}
}

func TestReadyFeatsSkipsUnavailable(t *testing.T) {
	doc := dagDoc(t)
	got := slugs(readyFeats(doc, map[string]bool{"a": true}, map[string]bool{"b": true}, nil))
	if strings.Join(got, ",") != "c,e" {
		t.Errorf("an unavailable feat must not be offered, want [c e] got %v", got)
	}
}

func TestReadyFeatsHonorsDiscoveredDeps(t *testing.T) {
	doc := dagDoc(t)
	// e declares no dependency, but a session discovered it needs a.
	extra := map[string][]string{"e": {"a"}}
	if got := slugs(readyFeats(doc, map[string]bool{}, nil, extra)); strings.Join(got, ",") != "a" {
		t.Errorf("a discovered edge must gate the feat, want [a] got %v", got)
	}
	if got := slugs(readyFeats(doc, map[string]bool{"a": true}, nil, extra)); !contains(got, "e") {
		t.Errorf("e should open once its discovered dependency lands, got %v", got)
	}
}

// TestStrandedNamesTheRootCause covers the diagnosability half. When `a` is
// blocked, b/c/d are all unreachable — and the run has to say so instead of
// reporting a quiet early finish, which is what the old sequencer did.
func TestStrandedNamesTheRootCause(t *testing.T) {
	doc := dagDoc(t)
	strand := stranded(doc, map[string]bool{}, map[string]bool{"a": true}, nil)

	for _, feat := range []string{"b", "c"} {
		if strand[feat] != "a" {
			t.Errorf("%s is stranded directly behind a, got %q", feat, strand[feat])
		}
	}
	// d is two hops out: it depends on b, which is itself stranded.
	if strand["d"] == "" {
		t.Errorf("stranding is transitive: d should be unreachable too, got %v", strand)
	}
	if _, ok := strand["e"]; ok {
		t.Errorf("e depends on nothing and must stay workable, got %v", strand)
	}
}

func TestStrandedEmptyWhenEverythingIsReachable(t *testing.T) {
	doc := dagDoc(t)
	if s := stranded(doc, map[string]bool{"a": true}, nil, nil); len(s) != 0 {
		t.Errorf("nothing is blocked, so nothing is stranded, got %v", s)
	}
}

// --- discovered deps sidecar (R6.4, R6.5) -------------------------------------

func TestDiscoveredDepsRoundTrip(t *testing.T) {
	root := t.TempDir()
	if got := LoadDiscoveredDeps(root, "p"); len(got) != 0 {
		t.Errorf("a missing sidecar must read as empty, got %v", got)
	}

	merged, err := RecordDiscoveredDeps(root, "p", "e", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(merged["e"], ",") != "a,b" {
		t.Errorf("want [a b], got %v", merged["e"])
	}

	// Merging is additive, deduplicated and sorted, so the file is diff-stable.
	if _, err := RecordDiscoveredDeps(root, "p", "e", []string{"b", "c"}); err != nil {
		t.Fatal(err)
	}
	if got := LoadDiscoveredDeps(root, "p")["e"]; strings.Join(got, ",") != "a,b,c" {
		t.Errorf("edges should merge deduplicated and sorted, got %v", got)
	}

	// A self-edge is nonsense and is dropped rather than deadlocking the feat.
	if _, err := RecordDiscoveredDeps(root, "p", "e", []string{"e"}); err != nil {
		t.Fatal(err)
	}
	if got := LoadDiscoveredDeps(root, "p")["e"]; contains(got, "e") {
		t.Errorf("a feat must never depend on itself, got %v", got)
	}
}

// TestDiscoveredDepsNeverTouchPlanMd is the load-bearing constraint. Writing the
// edge back into plan.md is the obvious move and it is self-defeating: plan.json
// binds approval to a hash of plan.md, so the runner mutating it would make its own
// next preflight report drift and refuse to run.
func TestDiscoveredDepsNeverTouchPlanMd(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	planPath := filepath.Join(Dir(root, "p"), "plan.md")
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordDiscoveredDeps(root, "p", "b", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(planPath)
	if !bytes.Equal(before, after) {
		t.Fatal("recording a discovered edge must not modify plan.md")
	}
	if _, drift, err := IsApproved(root, "p"); err != nil || drift {
		t.Errorf("the approval must survive: drift=%v err=%v", drift, err)
	}
}

// --- the blocked verdict (R6.1 - R6.3) ----------------------------------------

// TestBlockedVerdictParksWithoutSpendingAnAttempt is why the status exists.
// `continue` would have been the only way to express this, and it both resets the
// stall guard and spends one of the feat's bounded attempts on work that cannot
// progress — a feat waiting on a peer would burn its whole budget and be reported
// as unable to converge.
func TestBlockedVerdictParksWithoutSpendingAnAttempt(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	calls := 0
	h := baseHooks(t, root)
	h.Session = func(feat Feat, _ string, _ float64) (SessionOutcome, error) {
		calls++
		if feat.Slug == "a" && calls == 1 {
			return SessionOutcome{Verdict: Verdict{
				Status: VerdictBlocked, Summary: "needs b's schema", BlockedOn: []string{"b"},
			}}, nil
		}
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}

	var out bytes.Buffer
	sum, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, FeatAttempts: 1, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	// FeatAttempts is 1. Had parking spent an attempt, `a` would have been blocked
	// before it ever got a real try.
	if len(sum.Blocked) != 0 {
		t.Fatalf("parking must not spend an attempt, got blocked=%v\n%s", sum.Blocked, out.String())
	}
	if !sum.Completed {
		t.Fatalf("a should run after b lands, leaving the plan complete: %+v\n%s", sum, out.String())
	}
	// The edge is persisted so a later run does not rediscover the same wall.
	if got := LoadDiscoveredDeps(root, "p")["a"]; strings.Join(got, ",") != "b" {
		t.Errorf("the discovered edge should be recorded, got %v", got)
	}
	// And it settles its `started` row as blocked, not as a crash.
	var blocked int
	for _, r := range LoadSessionRecords(root, "p") {
		if r.Status == SessionBlocked {
			blocked++
		}
	}
	if blocked != 1 {
		t.Errorf("expected exactly one blocked row, got %d", blocked)
	}
}

// TestBlockedVerdictRefusedWhenClaimDoesNotHold covers R6.2. `blocked` costs the
// session nothing, which is what makes it useful for a genuine dependency and what
// would make it an attractive escape from hard work — so the claim is checked
// against the plan the same way a `done` is checked against disk.
func TestBlockedVerdictRefusedWhenClaimDoesNotHold(t *testing.T) {
	cases := []struct {
		name    string
		verdict Verdict
		want    string
	}{
		{"unknown feat", Verdict{Status: VerdictBlocked, BlockedOn: []string{"nope"}}, "does not have"},
		{"itself", Verdict{Status: VerdictBlocked, BlockedOn: []string{"a"}}, "does not have"},
		{"already delivered", Verdict{Status: VerdictBlocked, BlockedOn: []string{"b"}}, "already delivered"},
		{"named nothing", Verdict{Status: VerdictBlocked}, "named no feat"},
	}
	doc := Parse(runnerPlan)
	doc.Slug = "p"
	feat, _ := doc.Feat("a")
	done := map[string]bool{"b": true}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, refusal := gateBlocked(doc, done, feat, tc.verdict)
			if refusal == "" {
				t.Fatalf("the claim should have been refused, got deps=%v", deps)
			}
			if !strings.Contains(refusal, tc.want) {
				t.Errorf("refusal should say %q, got %q", tc.want, refusal)
			}
		})
	}
}

// TestBlockedRefusalBecomesContinue proves the demotion path end to end: a session
// that names a bogus blocker gets handed the feat back as ordinary partial work,
// with a handoff explaining why, rather than parking for free.
func TestBlockedRefusalBecomesContinue(t *testing.T) {
	root := approvedRunnerWorkspace(t)
	var briefs []string
	calls := 0
	h := baseHooks(t, root)
	h.Session = func(feat Feat, brief string, _ float64) (SessionOutcome, error) {
		calls++
		briefs = append(briefs, brief)
		if feat.Slug == "a" && calls == 1 {
			return SessionOutcome{Verdict: Verdict{
				Status: VerdictBlocked, Summary: "too hard", BlockedOn: []string{"does-not-exist"},
			}}, nil
		}
		deliverSpec(t, root, feat.Slug)
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}
	if _, err := Run(RunOptions{Root: root, Slug: "p", Hooks: h, MaxIterations: 10, Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if len(briefs) < 2 {
		t.Fatalf("the feat should have come back, got %d sessions", len(briefs))
	}
	if !strings.Contains(briefs[1], "refused the claim") {
		t.Errorf("the successor must be told why the claim failed, got:\n%s", briefs[1])
	}
	if got := LoadDiscoveredDeps(root, "p"); len(got) != 0 {
		t.Errorf("a refused claim records no edge, got %v", got)
	}
}

func TestNormalizeVerdictAcceptsBlocked(t *testing.T) {
	v, err := normalizeVerdict(Verdict{Status: "BLOCKED", BlockedOn: []string{" a ", "", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != VerdictBlocked {
		t.Errorf("status should normalize to %q, got %q", VerdictBlocked, v.Status)
	}
	if strings.Join(v.BlockedOn, ",") != "a,b" {
		t.Errorf("blocked_on should be trimmed with empties dropped, got %v", v.BlockedOn)
	}
}

func TestVerdictSchemaOffersBlocked(t *testing.T) {
	if !strings.Contains(verdictSchema, `"blocked"`) || !strings.Contains(verdictSchema, "blocked_on") {
		t.Errorf("the model cannot report an intent the schema does not offer: %s", verdictSchema)
	}
}

// --- squad limit (R7) ----------------------------------------------------------

func TestSquadLimitDefaultsToOne(t *testing.T) {
	opts := RunOptions{Root: t.TempDir(), Slug: "p", Hooks: Hooks{Now: fixedNow, Sleep: func(time.Duration) {}}}
	fillRunDefaults(&opts)
	if opts.SquadLimit != 1 {
		t.Errorf("an unset squad limit must behave as serial, got %d", opts.SquadLimit)
	}
}
