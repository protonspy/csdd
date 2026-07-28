package plan

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Proving a feat was delivered.
//
// "Delivered" is asserted by four independent records, and they can disagree —
// which is the whole reason this exists. In a real workspace (violet, plan
// `frontend-design-refresh`) `plan status` reported two feats done while git held
// exactly one merge commit: the other had been finished by hand before the run and
// imported by reconcileLedgerFromDisk. Both readings are correct and they mean
// completely different things, and nothing in the CLI could tell them apart.
//
// The four records, and what each one alone cannot prove:
//
//   - the LEDGER says a `done` verdict cleared the gate — but not that anything
//     reached the base, and it is regenerable transient state;
//   - the ARTIFACTS (spec.json, tasks.md, test-report.json) are the runner's own
//     definition of delivered (gateDone) — but they are files a session can write
//     without the work landing anywhere;
//   - a MERGE COMMIT is the only proof the code is on the branch the run
//     integrates into;
//   - a LIVE WORKTREE is proof it is not finished: a delivered feat has its tree
//     discarded, so one that survives holds work nobody committed.
//
// Verify reads all four and reports only where they contradict each other.

// mergeMessage is the subject Integrate writes for a feat's merge commit, and the
// string Verify greps for. They share it so a reworded merge message can never
// silently turn every delivered feat into an unverifiable one.
func mergeMessage(feat, slug string) string {
	return fmt.Sprintf("Merge feat %s of plan %s", feat, slug)
}

// Delivery states a feat can be in. They are reported, not judged: only Findings
// mark something as wrong.
const (
	// DeliveredMerged is the fully corroborated state: all four records agree.
	DeliveredMerged = "delivered"
	// DeliveredOffRun is a feat whose artifacts are complete but which carries no
	// merge commit — finished before or outside the run and imported into the
	// ledger. Legitimate, and worth naming rather than reporting as "delivered".
	DeliveredOffRun = "delivered-off-run"
	// InProgress is an undelivered feat that still holds a live worktree.
	InProgress = "in-progress"
	// Pending is an undelivered feat nothing has started.
	Pending = "pending"
)

// FeatVerification is what each record says about one feat.
type FeatVerification struct {
	Feat         string   `json:"feat"`
	State        string   `json:"state"`
	LedgerDone   bool     `json:"ledger_done"`
	Artifacts    bool     `json:"artifacts_complete"`
	ArtifactGaps []string `json:"artifact_gaps,omitempty"`
	Merged       bool     `json:"merged"`
	MergeCommit  string   `json:"merge_commit,omitempty"`
	LiveWorktree bool     `json:"live_worktree"`
	// Findings are contradictions between the records. A feat with none is
	// consistent, whatever state it is in.
	Findings []string `json:"findings,omitempty"`
}

// VerifyReport is the whole plan's delivery evidence.
type VerifyReport struct {
	Plan string `json:"plan"`
	// Branch is what the merge check was run against, or "" when the workspace is
	// not a git repository — in which case Merged is unknowable and never a finding.
	Branch string             `json:"branch,omitempty"`
	Git    bool               `json:"git"`
	Feats  []FeatVerification `json:"feats"`
	OK     bool               `json:"ok"`
}

// Verify cross-checks every feat in the plan against all four records.
func Verify(root string, doc *PlanDoc) VerifyReport {
	rep := VerifyReport{Plan: doc.Slug, OK: true}
	ledger := LoadLedger(root, doc.Slug)
	trees := gitTrees{root: root, slug: doc.Slug}
	if branch, ok := currentBranch(root); ok {
		rep.Git, rep.Branch = true, branch
	}

	for _, f := range doc.Feats {
		v := FeatVerification{Feat: f.Slug, LedgerDone: ledger.Done(f.Slug)}

		gaps := gateDone(root, f)
		v.Artifacts = len(gaps) == 0
		for _, g := range gaps {
			v.ArtifactGaps = append(v.ArtifactGaps, g.check+": "+g.detail)
		}
		if rep.Git {
			v.MergeCommit, v.Merged = mergeCommitFor(root, f.Slug, doc.Slug)
		}
		if live, err := liveWorktree(trees.path(f.Slug)); err == nil {
			v.LiveWorktree = live
		}

		switch {
		case v.LedgerDone && !v.Artifacts:
			// The serious one. The ledger is what the sequencer trusts, so a feat
			// marked done without its artifacts is skipped forever by every later
			// run while the work it stands for does not exist.
			v.State = DeliveredOffRun
			v.Findings = append(v.Findings, fmt.Sprintf(
				"the ledger marks %s delivered but its artifacts do not hold (%s); "+
					"every later run will skip it", f.Slug, strings.Join(v.ArtifactGaps, "; ")))
		case v.LedgerDone && v.Merged:
			v.State = DeliveredMerged
		case v.LedgerDone:
			// Artifacts complete, no merge commit: finished outside this run. Only a
			// finding when git could actually have shown one.
			v.State = DeliveredOffRun
			if rep.Git {
				v.Findings = append(v.Findings, fmt.Sprintf(
					"%s is marked delivered and its artifacts hold, but no merge commit for it "+
						"exists on %s — it was delivered outside this run, so the code on the branch "+
						"is not evidence of it", f.Slug, rep.Branch))
			}
		case v.Artifacts && v.Merged:
			// Delivered by every other measure, invisible to the ledger — the next
			// run will hand it out and redo finished work.
			v.State = DeliveredMerged
			v.Findings = append(v.Findings, fmt.Sprintf(
				"%s is merged and its artifacts hold, but the ledger does not mark it delivered — "+
					"the next `plan run` will work it again", f.Slug))
		case v.LiveWorktree:
			v.State = InProgress
		default:
			v.State = Pending
		}

		// A delivered feat's tree is discarded once its work is on the base, so one
		// that survives is either uncommitted work or a leak. Either way it is the
		// difference between "done" and "done and cleaned up".
		if v.LedgerDone && v.LiveWorktree {
			v.Findings = append(v.Findings, fmt.Sprintf(
				"%s is marked delivered but its worktree is still live at %s — "+
					"anything uncommitted in it is not on the branch",
				f.Slug, filepath.ToSlash(trees.path(f.Slug))))
		}

		if len(v.Findings) > 0 {
			rep.OK = false
		}
		rep.Feats = append(rep.Feats, v)
	}
	return rep
}

// currentBranch reports the branch the merge check runs against. It is HEAD rather
// than the run's recorded base because the base lives only in the runner's memory,
// and the question `plan verify` answers is "is the work on the branch I am on".
func currentBranch(root string) (string, bool) {
	out, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", false
	}
	b := strings.TrimSpace(out)
	if b == "" || b == "HEAD" { // detached: no branch to reason about
		return "", false
	}
	return b, true
}

// mergeCommitFor finds the merge Integrate wrote for a feat, if it is reachable
// from HEAD. --fixed-strings so a slug containing regex metacharacters cannot
// match the wrong commit or none at all.
func mergeCommitFor(root, feat, slug string) (string, bool) {
	out, err := runGit(root, "log", "--merges", "--fixed-strings",
		"--grep", mergeMessage(feat, slug), "--format=%h", "-n", "1")
	if err != nil {
		return "", false
	}
	h := strings.TrimSpace(out)
	return h, h != ""
}
