package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reading a run back: what it spent (cost.go) and what it actually delivered
// (verify.go).
//
// Both are answers to questions the violet run raised and no command could
// answer — how much of the spend is still accounted for, and whether the four
// records that claim a feat was delivered agree with each other.

// --- cost ---------------------------------------------------------------------

func TestCostReportSeparatesSpendFromWhatItCanAccountFor(t *testing.T) {
	root := t.TempDir()
	slug := "p"
	rows := []SessionRecord{
		// A feat that took two sessions: one refused `done`, then a delivery.
		{Feat: "a", Attempt: 1, Status: SessionStarted},
		{Feat: "a", Attempt: 1, Status: SessionContinue, Gated: true, CostUSD: 11.16, NumTurns: 25,
			Tokens: SessionTokens{Input: 60, Output: 20000, CacheRead: 1500000}},
		{Feat: "a", Attempt: 2, Status: SessionStarted},
		{Feat: "a", Attempt: 2, Status: SessionDone, CostUSD: 4.22, NumTurns: 12,
			Tokens: SessionTokens{Input: 40, Output: 10000, CacheRead: 900000}},
		// A feat whose session the host outlived: opened, never settled.
		{Feat: "b", Attempt: 1, Status: SessionStarted},
	}
	for _, r := range rows {
		if err := AppendSessionRecord(root, slug, r); err != nil {
			t.Fatal(err)
		}
	}

	rep := BuildCostReport(root, slug)
	if len(rep.Feats) != 2 {
		t.Fatalf("want a row per feat, got %d", len(rep.Feats))
	}
	a := rep.Feats[0]
	if a.Attempts != 2 || a.Settled != 2 || a.Gated != 1 || !a.Delivered {
		t.Errorf("feat a: %+v", a)
	}
	if !nearUSD(a.CostUSD, 15.38) {
		t.Errorf("feat a must carry both sessions' cost, got $%.2f", a.CostUSD)
	}
	// The whole point of the report: an attempt with no settled row is spend the
	// run cannot account for, and it must be named rather than quietly dropped.
	if len(rep.Unmeasured) != 1 || rep.Unmeasured[0] != "b#1" {
		t.Errorf("the crashed attempt must be reported as unmeasured, got %v", rep.Unmeasured)
	}
	if rep.Totals.Attempts != 3 || rep.Totals.Settled != 2 {
		t.Errorf("totals must count opened and settled separately: %+v", rep.Totals)
	}
	if !nearUSD(rep.Totals.CostUSD, 15.38) {
		t.Errorf("totals cost = $%.2f", rep.Totals.CostUSD)
	}
}

func TestCostReportSplitsSpendAcrossModels(t *testing.T) {
	root := t.TempDir()
	rows := []SessionRecord{
		{Feat: "a", Attempt: 1, Status: SessionStarted},
		{Feat: "a", Attempt: 1, Status: SessionDone, CostUSD: 6,
			Tokens: SessionTokens{Output: 1000},
			ByModel: []ModelTokens{
				{Model: "claude-opus-5", Tokens: SessionTokens{Output: 1000}, CostUSD: 5},
				{Model: "claude-sonnet-5", Tokens: SessionTokens{Output: 4000}, CostUSD: 1},
			}},
	}
	for _, r := range rows {
		if err := AppendSessionRecord(root, "p", r); err != nil {
			t.Fatal(err)
		}
	}
	rep := BuildCostReport(root, "p")
	if len(rep.ByModel) != 2 {
		t.Fatalf("want a row per billing model, got %+v", rep.ByModel)
	}
	if rep.ByModel[0].Model != "claude-opus-5" || rep.ByModel[1].Model != "claude-sonnet-5" {
		t.Errorf("the breakdown must be sorted by model, got %+v", rep.ByModel)
	}
	// The gap between the two totals is the whole reason both are reported: the
	// session's own `usage` counted 1000 tokens, the per-model breakdown 5000.
	if got, want := rep.ModelTotal().Total(), 5000; got != want {
		t.Errorf("per-model total = %d, want %d", got, want)
	}
	if rep.Totals.Tokens.Total() != 1000 {
		t.Errorf("the session total must stay what the envelope reported, got %d", rep.Totals.Tokens.Total())
	}
}

func TestParseSessionMetricsReadsPerModelUsage(t *testing.T) {
	// camelCase is what the CLI has been observed to emit; snake_case is what the
	// API uses. Both are read, so a rename upstream degrades one column instead of
	// zeroing the breakdown.
	raw := `{"type":"result","total_cost_usd":9.5,"usage":{"input_tokens":10,"output_tokens":20},
	  "modelUsage":{
	    "claude-opus-5":{"inputTokens":100,"outputTokens":200,"cacheReadInputTokens":3000,"costUSD":7.5},
	    "claude-sonnet-5":{"input_tokens":5,"output_tokens":6,"cache_read_input_tokens":7,"cost_usd":2.0}}}`
	m := parseSessionMetrics([]byte(raw))
	if len(m.ByModel) != 2 {
		t.Fatalf("want both models, got %+v", m.ByModel)
	}
	opus := m.ByModel[0]
	if opus.Model != "claude-opus-5" || opus.Tokens.Input != 100 || opus.Tokens.CacheRead != 3000 || opus.CostUSD != 7.5 {
		t.Errorf("camelCase entry misread: %+v", opus)
	}
	sonnet := m.ByModel[1]
	if sonnet.Tokens.Input != 5 || sonnet.Tokens.CacheRead != 7 || sonnet.CostUSD != 2.0 {
		t.Errorf("snake_case entry misread: %+v", sonnet)
	}
	if len(m.Models) != 2 {
		t.Errorf("the model-name list must survive alongside the breakdown, got %v", m.Models)
	}
}

// --- verify -------------------------------------------------------------------

// verifyPlan is two independent feats, which is enough to put one in every state
// the report distinguishes.
const verifyPlanMD = `---
name: p
status: draft
---

## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | a | A | — | M1 | | |
| 2 | b | B | — | M1 | | |

## Quality Gates

- verify: make check
`

// TestVerifyDistinguishesMergedFromReconciled is the case the violet workspace
// produced and no command could report: two feats both marked done, one carrying a
// merge commit and one that was finished by hand before the run and imported into
// the ledger. `plan status` calls both "done"; only git tells them apart.
func TestVerifyDistinguishesMergedFromReconciled(t *testing.T) {
	root := gitRepo(t)
	seedApprovedPlan(t, root, verifyPlanMD)
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}

	// Only `a` is landed the way the runner lands a feat. Driving the real
	// Integrate rather than hand-writing a commit is the point: it is the only way
	// this test can catch the merge subject and the grep drifting apart.
	trees := newTrees(root)
	dir, err := trees.Ensure("a")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "a.go", "package p\n")
	commitAll(t, dir, "work for a")
	if err := trees.Integrate("a"); err != nil {
		t.Fatalf("could not land a on the base: %v", err)
	}
	if err := trees.Discard("a"); err != nil {
		t.Fatal(err)
	}

	// Both feats have complete artifacts and both are in the ledger.
	for _, slug := range []string{"a", "b"} {
		if err := deliverSpecFiles(root, slug); err != nil {
			t.Fatal(err)
		}
	}
	ledger := LoadLedger(root, "p")
	ledger.MarkDone("a", "merged by the run", fixedNow())
	ledger.MarkDone("b", "reconciled: delivered on disk before this run", fixedNow())
	if err := ledger.Save(root, "p"); err != nil {
		t.Fatal(err)
	}

	rep := Verify(root, doc)
	if !rep.Git || rep.Branch != "main" {
		t.Fatalf("the merge check should run against main, got git=%v branch=%q", rep.Git, rep.Branch)
	}
	byFeat := map[string]FeatVerification{}
	for _, f := range rep.Feats {
		byFeat[f.Feat] = f
	}
	if got := byFeat["a"]; got.State != DeliveredMerged || !got.Merged || len(got.Findings) != 0 {
		t.Errorf("a is corroborated by every record and must be clean: %+v", got)
	}
	if got := byFeat["b"]; got.State != DeliveredOffRun || got.Merged {
		t.Errorf("b has no merge commit and must be reported as delivered off-run: %+v", got)
	}
	if got := byFeat["b"]; len(got.Findings) == 0 {
		t.Error("a feat the branch holds no evidence for must produce a finding")
	}
	if rep.OK {
		t.Error("a report with a finding is not OK")
	}
}

// TestVerifyCatchesADeliveredFeatWithNoArtifacts is the failure that matters most:
// the ledger is what the sequencer trusts, so a feat marked done without its
// artifacts is skipped by every later run while the work it stands for is absent.
func TestVerifyCatchesADeliveredFeatWithNoArtifacts(t *testing.T) {
	root := gitRepo(t)
	seedApprovedPlan(t, root, verifyPlanMD)
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	ledger := LoadLedger(root, "p")
	ledger.MarkDone("a", "claimed", fixedNow())
	if err := ledger.Save(root, "p"); err != nil {
		t.Fatal(err)
	}

	rep := Verify(root, doc)
	var a FeatVerification
	for _, f := range rep.Feats {
		if f.Feat == "a" {
			a = f
		}
	}
	if a.Artifacts {
		t.Fatal("no spec was written; the artifacts cannot hold")
	}
	if len(a.ArtifactGaps) == 0 {
		t.Error("the report must name which artifact is missing, not just that one is")
	}
	if len(a.Findings) == 0 || !strings.Contains(a.Findings[0], "skip") {
		t.Errorf("the finding must say the feat will be skipped forever, got %v", a.Findings)
	}
	if rep.OK {
		t.Error("a ledger that outruns the artifacts is not OK")
	}
}

// TestVerifyReportsAnUndeliveredFeatWithoutComplaining keeps the command usable:
// a plan half-way through a run is not a plan with problems, and a verifier that
// cries about every pending feat gets ignored.
func TestVerifyReportsAnUndeliveredFeatWithoutComplaining(t *testing.T) {
	root := gitRepo(t)
	seedApprovedPlan(t, root, verifyPlanMD)
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	// `a` has a live worktree: a session is mid-flight or left uncommitted work.
	trees := newTrees(root)
	if _, err := trees.Ensure("a"); err != nil {
		t.Fatal(err)
	}

	rep := Verify(root, doc)
	byFeat := map[string]FeatVerification{}
	for _, f := range rep.Feats {
		byFeat[f.Feat] = f
	}
	if got := byFeat["a"]; got.State != InProgress || !got.LiveWorktree {
		t.Errorf("a feat holding a live worktree is in progress: %+v", got)
	}
	if got := byFeat["b"]; got.State != Pending {
		t.Errorf("a feat nothing has touched is pending: %+v", got)
	}
	if !rep.OK {
		t.Errorf("an unfinished plan is not an inconsistent one: %+v", rep.Feats)
	}
}

// --- the worktree entry document ----------------------------------------------

// TestWorktreeGetsTheLeanEntryDocAndCannotCommitIt pins both halves of the swap.
// The saving is worthless if the session can commit the replacement back over the
// file a human maintains, and the uncommitted-work check would refuse every
// delivery if the swap showed up as a dirty path.
func TestWorktreeGetsTheLeanEntryDocAndCannotCommitIt(t *testing.T) {
	root := gitRepo(t)
	writeRepoFile(t, root, "CLAUDE.md", "# the repository's own, long and interactive\n")
	commitAll(t, root, "add the entry point")

	trees := newTrees(root)
	trees.entry = "# lean plan-session entry\n"
	dir, err := trees.Ensure("a")
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != trees.entry {
		t.Errorf("the worktree must read the lean entry doc, got %q", got)
	}
	// Invisible to git: nothing to commit, nothing to merge back, and the
	// uncommitted-work check stays clean.
	status, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range gitLines(status) {
		if strings.Contains(line, "CLAUDE.md") {
			t.Errorf("the swapped entry doc must not appear as a change, status: %q", status)
		}
	}
	// And the repository's own copy is untouched.
	if repo := readRepoFile(t, root, "CLAUDE.md"); !strings.Contains(repo, "long and interactive") {
		t.Errorf("the human's CLAUDE.md must not be rewritten, got %q", repo)
	}
}

// TestWorktreeKeepsAnUntrackedEntryDocAlone is the guard on the guard. The swap is
// only safe because skip-worktree hides it, and that needs a tracked file — so an
// untracked CLAUDE.md must be left alone rather than created, which would surface
// as untracked work and fail the check that protects a delivered feat.
func TestWorktreeKeepsAnUntrackedEntryDocAlone(t *testing.T) {
	root := gitRepo(t) // no CLAUDE.md committed
	trees := newTrees(root)
	trees.entry = "# lean plan-session entry\n"
	dir, err := trees.Ensure("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("no CLAUDE.md is tracked, so none may be written; stat err = %v", err)
	}
	status, _ := runGit(dir, "status", "--porcelain")
	if strings.Contains(status, "CLAUDE.md") {
		t.Errorf("the worktree must stay clean, status: %q", status)
	}
}
