package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/session"
	"github.com/protonspy/csdd/internal/workspace"
)

// The verdict gate (R10).
//
// The loop used to accept `done` verbatim: parseVerdict validated only that the
// string was one of done|continue, so a session that believed it had finished
// marked the feat delivered and the run advanced. That is the one place in the
// system where a wrong answer is expensive — a false "tests pass" inside a task
// gets caught by the next run of the suite, but a false `done` on a whole feat
// was caught nowhere.
//
// The checks that close it are the cheapest in the system: counting `[x]` in
// tasks.md, reading three booleans out of spec.json, and reading the evidence file
// the session was already required to write. No model, no subprocess, no test
// suite (R10.2) — three file reads, single-digit milliseconds, against the ~30
// minutes per feat the templates used to spend re-running suites to catch less.
//
// The gate is deliberately NOT a judgement of the work. It asserts only that the
// artifacts the session contracted to produce exist and agree with each other. A
// rejection therefore always means the contract was not met, which is why the
// handoff can name the failing check and let the next session self-heal.

// gateFinding is one failed mechanical check: what was checked, and what was
// found instead. Both halves matter — the next session needs to know which
// obligation it missed AND the state that proves it.
type gateFinding struct {
	check  string
	detail string
}

// gateDone runs every on-disk check behind a `done` verdict and returns what
// failed. An empty result means the verdict stands. Checks are independent on
// purpose: a session that missed two obligations should be told both at once
// rather than discovering the second only after fixing the first.
func gateDone(root string, feat Feat) []gateFinding {
	specDir := filepath.Join(paths.Specs(root), feat.Slug)
	if _, err := os.Stat(specDir); err != nil {
		return []gateFinding{{
			check:  "spec exists",
			detail: fmt.Sprintf("specs/%s/ does not exist — the feat was never specced", feat.Slug),
		}}
	}
	var out []gateFinding
	if f, ok := gateSpecApproved(specDir, feat.Slug); !ok {
		out = append(out, f)
	}
	if f, ok := gateTasksChecked(specDir, feat.Slug); !ok {
		out = append(out, f)
	}
	if f, ok := gateEvidenceGreen(specDir, feat.Slug); !ok {
		out = append(out, f)
	}
	return out
}

// gateSpecApproved checks that all three phases are approved and the spec is
// flagged ready. Only csdd flips ready_for_implementation, and only after the
// three approvals, so the two agreeing is the signal that the phases were driven
// through the real gate rather than hand-edited into spec.json.
func gateSpecApproved(specDir, slug string) (gateFinding, bool) {
	ap := readSpecApprovals(specDir)
	var missing []string
	for _, p := range workspace.SpecPhases {
		if !ap[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return gateFinding{
			check:  "spec phases approved",
			detail: fmt.Sprintf("specs/%s/spec.json: %s not approved — run `csdd spec validate %s`, fix every finding, then `csdd spec approve %s --phase <phase>`", slug, strings.Join(missing, ", "), slug, slug),
		}, false
	}
	if !readSpecReady(specDir) {
		return gateFinding{
			check:  "spec ready for implementation",
			detail: fmt.Sprintf("specs/%s/spec.json: every phase is approved but ready_for_implementation is false — re-approve through csdd rather than hand-editing spec.json", slug),
		}, false
	}
	return gateFinding{}, true
}

// gateTasksChecked checks that tasks.md has tasks and every one of them is `[x]`.
// A spec with zero parsed tasks fails too: an empty tasks.md would otherwise
// satisfy "every task is checked" vacuously, which is the exact shape of a feat
// that was declared done before it was broken down.
func gateTasksChecked(specDir, slug string) (gateFinding, bool) {
	total, checked := taskProgress(specDir)
	switch {
	case total == 0:
		return gateFinding{
			check:  "tasks are broken down",
			detail: fmt.Sprintf("specs/%s/tasks.md declares no tasks — a feat cannot be delivered before it is broken down", slug),
		}, false
	case checked < total:
		return gateFinding{
			check:  "every task is checked",
			detail: fmt.Sprintf("specs/%s/tasks.md: %d of %d task boxes are `[x]` — %d task(s) remain", slug, checked, total, total-checked),
		}, false
	}
	return gateFinding{}, true
}

// gateEvidenceGreen checks the recorded test evidence: the file exists, it counts
// tests, none failed, and it carries no open attentions. The attention check is
// what makes R11 enforce itself — a report written by a command that excluded
// tests carries an attention, so it can never satisfy a `done` (§5.6).
func gateEvidenceGreen(specDir, slug string) (gateFinding, bool) {
	rep := session.LoadSpecReport(specDir)
	switch {
	case rep == nil:
		return gateFinding{
			check:  "test evidence recorded",
			detail: fmt.Sprintf("specs/%s/%s is missing or unreadable — record it with `csdd spec test-report %s --run --lang <lang>`", slug, session.SpecReportFile, slug),
		}, false
	case rep.Tests == nil || rep.Tests.Total == 0:
		return gateFinding{
			check:  "test evidence records tests",
			detail: fmt.Sprintf("specs/%s/%s records no tests — re-run `csdd spec test-report %s --run --lang <lang>` so the suite's real result is captured", slug, session.SpecReportFile, slug),
		}, false
	case rep.Tests.Failed > 0:
		return gateFinding{
			check:  "test evidence is green",
			detail: fmt.Sprintf("specs/%s/%s records %d failing test(s) of %d", slug, session.SpecReportFile, rep.Tests.Failed, rep.Tests.Total),
		}, false
	case len(rep.Attentions) > 0:
		return gateFinding{
			check:  "test evidence has no open attentions",
			detail: fmt.Sprintf("specs/%s/%s carries %d open attention(s): %s", slug, session.SpecReportFile, len(rep.Attentions), strings.Join(rep.Attentions, "; ")),
		}, false
	}
	return gateFinding{}, true
}

// readSpecReady reports spec.json's ready_for_implementation flag. A missing or
// malformed file reads as not ready — the same direction readSpecApprovals fails
// in, so a broken spec.json can never widen the gate.
func readSpecReady(specDir string) bool {
	data, err := os.ReadFile(filepath.Join(specDir, "spec.json"))
	if err != nil {
		return false
	}
	var doc struct {
		Ready bool `json:"ready_for_implementation"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	return doc.Ready
}

// gateHandoff renders the rejection the next session receives in place of the
// summary the current one returned. It is written as instructions, not as a
// complaint: the session that reads it is a fresh process with no memory of the
// verdict that was refused, so it needs the state and the remedy, not a verdict
// post-mortem (R10.3).
func gateHandoff(feat Feat, findings []gateFinding, summary string) string {
	var b strings.Builder
	b.WriteString("The previous session returned `done` for this feat, but the loop's\n")
	b.WriteString("on-disk checks refused it — the artifacts it contracted to produce are not\n")
	b.WriteString("all there. Nothing was rolled back; finish the work below and return `done`\n")
	b.WriteString("again.\n\n### Checks that failed\n\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "- **%s** — %s\n", f.check, f.detail)
	}
	b.WriteString("\nFix every item above, then re-run your own step-8 self-checks before\n")
	b.WriteString("declaring done. If one of them genuinely cannot be satisfied for this feat,\n")
	b.WriteString("return `continue` and say so explicitly rather than declaring `done` again —\n")
	b.WriteString("the same checks will refuse it.\n")
	if s := strings.TrimSpace(summary); s != "" {
		b.WriteString("\n### What that session reported it had finished\n\n" + s + "\n")
	}
	return b.String()
}

// mergeConflictHandoff is the brief for a session picking up a feat that is finished
// but no longer applies to the run's base.
//
// It is deliberately not phrased as a failure. The work is done and committed on the
// feat's own branch; what changed is the base underneath it, because a peer feat
// landed while this one was in flight. So the instruction is to rebase and re-declare
// — not to redo the feat, which is the mistake a session told only "merge failed"
// would reasonably make.
func mergeConflictHandoff(feat Feat, conflict *MergeConflictError, summary string) string {
	var b strings.Builder
	b.WriteString("The previous session finished this feat and its `done` passed the loop's\n")
	b.WriteString("on-disk checks. The work could NOT be landed on the run's base branch: another\n")
	b.WriteString("feat merged while this one was in flight and the two touch the same lines.\n\n")
	fmt.Fprintf(&b, "Your work is safe and committed on `%s`. Nothing was rolled back or lost.\n\n", conflict.Branch)
	if len(conflict.Files) > 0 {
		b.WriteString("### Files that conflict\n\n")
		for _, f := range conflict.Files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}
	b.WriteString("### What to do\n\n")
	b.WriteString("Rebase this feat's branch onto the updated base, resolve each conflict above\n")
	b.WriteString("keeping BOTH feats' behavior, re-run the plan's Quality Gates, and return\n")
	b.WriteString("`done` again. Do NOT re-implement the feat from scratch — it is already\n")
	b.WriteString("written; this is an integration step, not a rewrite.\n")
	if s := strings.TrimSpace(summary); s != "" {
		b.WriteString("\n### What that session reported it had finished\n\n" + s + "\n")
	}
	return b.String()
}

// gateCheckNames lists the failed checks compactly, for one-line logs and the
// journal.
func gateCheckNames(findings []gateFinding) string {
	names := make([]string, 0, len(findings))
	for _, f := range findings {
		names = append(names, f.check)
	}
	return strings.Join(names, ", ")
}

// --- the blocked gate (R6.2) --------------------------------------------------

// gateBlocked decides whether a `blocked` verdict is believable, and returns the
// dependencies worth recording plus the reason to refuse when it is not.
//
// The check exists because `blocked` is the one verdict that costs the session
// nothing: it parks the feat without spending an attempt, which is exactly the
// property that makes it useful for a genuine cross-feat dependency and exactly
// the property that would make it an attractive escape from hard work. So the
// claim is verified the same way a `done` is — mechanically, against what is on
// disk, with no judgement about the session's reasoning.
//
// A named feat must exist in the plan and must not already be delivered. An
// unknown slug cannot block anything, and a delivered one is not blocking anybody:
// in both cases the session has mis-stated its situation, and the honest handling
// is to refuse the verdict and hand the feat back as ordinary partial work.
func gateBlocked(doc *PlanDoc, done map[string]bool, feat Feat, v Verdict) (deps []string, refusal string) {
	if len(v.BlockedOn) == 0 {
		return nil, "the verdict declared `blocked` but named no feat in blocked_on"
	}
	var unknown, delivered []string
	for _, dep := range v.BlockedOn {
		switch {
		case dep == feat.Slug:
			unknown = append(unknown, dep+" (itself)")
		case !planHasFeat(doc, dep):
			unknown = append(unknown, dep)
		case done[dep]:
			delivered = append(delivered, dep)
		default:
			deps = append(deps, dep)
		}
	}
	switch {
	case len(unknown) > 0:
		return nil, "blocked_on names a feat this plan does not have: " + strings.Join(unknown, ", ")
	case len(deps) == 0 && len(delivered) > 0:
		return nil, "blocked_on names only feats that are already delivered: " + strings.Join(delivered, ", ")
	}
	return deps, ""
}

func planHasFeat(doc *PlanDoc, slug string) bool {
	_, ok := doc.Feat(slug)
	return ok
}

// blockedRefusalHandoff tells the successor session why its predecessor's
// `blocked` claim was not accepted, so it re-plans instead of repeating it.
func blockedRefusalHandoff(feat Feat, refusal, summary string) string {
	var b strings.Builder
	b.WriteString("Your predecessor declared feat `" + feat.Slug + "` blocked on another feat, and the runner refused the claim:\n\n")
	b.WriteString("  " + refusal + "\n\n")
	b.WriteString("`blocked` is only for a feat that genuinely cannot proceed until a DIFFERENT, ")
	b.WriteString("not-yet-delivered feat of this plan lands. If what you need already exists, build against it. ")
	b.WriteString("If the work is merely hard or large, do it incrementally and report `continue`.\n")
	if s := strings.TrimSpace(summary); s != "" {
		b.WriteString("\nWhat it reported:\n\n" + s + "\n")
	}
	return b.String()
}
