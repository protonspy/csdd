package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/paths"
)

// missionSteps is the whole-feat contract every brief carries: the session owns
// the entire spec lifecycle and the implementation, drives its own git, and returns
// a verdict. The runner runs no gates of its own — it checks only the artifacts a
// `done` verdict implies (R10) — so the
// checks the runner used to enforce become checks the session runs itself before
// declaring the feat done — see the "Before you declare `done`" block the brief
// writes, which splits them into the state checks the session reads itself and
// the command gate it delegates to the `quality-gate` sub-agent.
var missionSteps = []string{
	"Author the spec if it does not exist yet: `csdd spec init <feat> [--flow unit|tdd|tdd-e2e]`, then for each phase DELEGATE the authoring to the `spec-author` sub-agent (sonnet): dispatch it with the feat, the phase, and any governing refs (ADRs/stack/wiki); it scaffolds (`csdd spec generate <feat> --artifact requirements|design|tasks`), consults the graph, authors the body, and runs `csdd spec validate <feat>`. REVIEW its artifact and run `csdd spec validate <feat>` yourself, then `csdd spec approve <feat> --phase requirements|design|tasks`. If the review or the validate finds gaps, RE-DISPATCH spec-author with a fix-list rather than hand-editing inline. Authoring runs on the cheap model; the JUDGMENT — review, validate, approve — stays on your (orchestrator) model. CHOOSE THE FLOW DELIBERATELY: `unit` (the default) for surfaces whose behaviors are render/CRUD states, which halves the verification round-trips per behavior; `tdd` REQUIRED for money, auth, tenancy, and anything irreversible. The flow you pick decides the shape of tasks.md, and `csdd spec validate` enforces that shape.",
	"Implement the tasks by DELEGATING each one to the `implementer` sub-agent (Agent/Task tool) — one leaf task per sub-agent. You orchestrate and decide; the implementer executes the already-made decision on its own (fast) model under the spec's `development_flow`, so do NOT hand-write task code inline. HAND IT THE TASK, NOT THE SPEC: the dispatch carries the task text with its `_Requirements:_`/`_Boundary:_`/`_Depends:_`, the acceptance criteria those IDs name, and the `### <Component>` section of design.md that owns the boundary — you already read the spec to author it, and making every implementer re-read all of it pays that cost again per sub-agent. Dispatch `(P)` tasks in DIFFERENT `_Boundary:_` groups concurrently (worktree isolation); honor every `_Depends:_` and keep same-boundary tasks sequential. Check each task's box `[x]` in specs/<feat>/tasks.md as its implementer lands it green. (If the `implementer` sub-agent is not installed in this workspace, fall back to running the cycle skill that matches the flow yourself — `tdd-cycle` under tdd/tdd-e2e, `unit-cycle` under unit — one task at a time.)",
	"COMMIT YOUR WORK, and commit it where you already are. You are running inside a git worktree the runner created for this feat, already checked out on this feat's own branch — do NOT create a branch, do NOT switch branches, do NOT push, and do NOT open a PR. Commit via /csdd-commit (the pre-push gate runs there) as you land each task, and make sure NOTHING is left uncommitted before you declare `done`: the runner merges your BRANCH into the run's base, so work you left in the working tree does not exist as far as the plan is concerned. The runner refuses a `done` whose worktree is dirty and hands the feat straight back. One PR covers the whole run and a human opens it after the run ends.",
	"Record any technology or hard-to-reverse trade-off the contract does not already cover — a docs/stack.md Decided row, plus a docs/adr record when the why needs more than a line. Prefer the option that deviates least from the decided stack.",
}

// forbiddenActions is the short list the session must not touch: the approved plan
// is the contract, and .csdd/ is the loop's operational state.
var forbiddenActions = []string{
	"Do NOT edit plan.md or plan.json — the approved plan is the contract. Leave .csdd/ (the loop's operational state, including progress.json) alone.",
}

// FeatBrief assembles the deterministic mission pack for one whole feat (R7). It
// draws only on explicit content — the feat row, its seeds, the governing ADR/stack
// refs (slugs/names/paths only — never ADR bodies, stack row details, or wiki
// descriptions), the Executor Notes, and the feat's own quality gates — so the same
// plan and feat always produce a byte-identical brief (R7.3). Compliance with the
// governors is enforced mechanically — designConformance requires design.md to cite
// each governing ADR, `plan validate` rejects tech outside the Decided stack, and
// the code-reviewer gate rejects it in code — not by inlining their bodies in the
// brief (R7.2). The autonomous run context (handoff, failure trail) is appended
// after this by the runner, so this prefix stays stable across a feat's sessions.
func FeatBrief(root string, doc *PlanDoc, feat Feat) (string, error) {
	if _, ok := doc.Feat(feat.Slug); !ok {
		return "", fmt.Errorf("feat %q is not in plan %q", feat.Slug, doc.Slug)
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("# Mission — %s / %s\n\n", doc.Slug, feat.Slug)
	w("You are a fresh session in a self-correcting autonomous loop. Your mission is to\n")
	w("fully deliver ONE feat of an approved csdd plan: %s. Drive the entire spec\n", feat.Slug)
	w("lifecycle and its implementation yourself, then return the verdict schema.\n\n")

	// 1. The mission contract.
	w("## Your mission (own the whole flow)\n\n")
	for i, s := range missionSteps {
		w("%d. %s\n", i+1, s)
	}

	// Authority: neutralize the interactive-development STOP rules (CLAUDE.md) that
	// otherwise make an autonomous session refuse to self-approve its spec phases and
	// stall waiting for a human. The plan-level `plan approve` gate already carried
	// the human authorization; inside this loop the session IS the approver.
	w("\n**Your authority — inside this loop, YOU are the approver:**\n")
	w("- A human already opened the gate for this whole plan by running `csdd plan approve %s`. That IS the human authorization CLAUDE.md's phase-gate and STOP rules require, and it covers every spec you author for this plan.\n", doc.Slug)
	w("- So you approve THIS feat's spec phases yourself: after each phase validates, run `csdd spec approve %s --phase requirements|design|tasks`. Do NOT pause for a human, do NOT return `continue` because a phase \"still needs approval\", and do NOT treat approving your own spec as routing around a gate — approving it is the mission.\n", feat.Slug)
	w("- CLAUDE.md's \"a human authorizes\" / \"STOP and surface the blocked item\" rules govern INTERACTIVE development. This is the autonomous plan loop: a blocked gate means fix the artifact until it validates, then approve and continue (use `--force` only to clear a stale prior-phase hash you have just re-validated).\n")
	w("- Follow the `plan-dev` skill for the exact per-phase workflow and completion criteria when it is installed (`.claude/skills/plan-dev/`); it restates these rules as an executable checklist.\n")

	w("\n**Forbidden actions:**\n")
	for _, f := range forbiddenActions {
		w("- %s\n", f)
	}
	w("\n**Verdict protocol:**\n")
	w("- `done` — the WHOLE feat is delivered and every self-check below passes. The loop checks this claim against your artifacts before accepting it (see below), so never claim done on hope.\n")
	w("- `continue` — honest partial work: you are out of room before the feat is complete. Put the handoff for your successor in `summary` (what is done, what remains, what to try next).\n")
	w("\n")

	// 2. Feat context.
	w("## Feat: %s\n\n", feat.Slug)
	w("- Objective: %s\n", orDash(feat.Objective))
	w("- Milestone: %s\n", orDash(feat.Milestone))
	if len(feat.Depends) > 0 {
		w("- Depends on (delivered earlier in the plan): %s\n", strings.Join(feat.Depends, ", "))
	}
	w("\n")

	// 3. Governing stack — refs only. The brief used to inline each Decided row
	// (choice/version/why) so a design could not drift from the stack; with the
	// bodies gone, compliance is enforced mechanically — `csdd spec validate` +
	// `plan validate` reject tech outside the Decided table, and the
	// `code-reviewer` gate rejects it in code. The session fetches a row's detail
	// via `csdd graph query <term>` when it needs it.
	if len(feat.StackRefs) > 0 {
		rows := decidedRows(root)
		w("## Governing stack (docs/stack.md Decided — use ONLY these)\n\n")
		w("Fetch a row's version/why via `csdd graph query <term>`; do NOT introduce tech outside this list — the `code-reviewer` gate rejects it.\n\n")
		for _, name := range feat.StackRefs {
			if _, ok := rows[normalizeTechName(name)]; ok {
				w("- stack:%s\n", name)
			} else {
				w("- stack:%s — WARNING: not found in the Decided table (validate should have caught this).\n", name)
			}
		}
		w("\n")
	}

	// 3b. Governing decisions — refs only. The brief used to inline each cited
	// ADR's title + body so a design could not silently ignore the decisions it
	// was bound to; with the bodies gone, `designConformance` requires the
	// authored design.md to cite each governing adr:<slug>, and the session
	// fetches the decision itself via `csdd graph explain adr:<slug>`.
	if len(feat.ADRRefs) > 0 {
		adrs := ScanADRs(root)
		w("## Governing decisions (docs/adr — fetch the why; cite each in design)\n\n")
		w("The brief no longer inlines ADR bodies. For each governor run `csdd graph explain adr:<slug>` for the decision; `csdd spec validate` requires your design.md to cite it.\n\n")
		for _, slug := range feat.ADRRefs {
			if _, res := adrs.Resolve(slug); res != ADRResolved {
				w("- adr:%s — WARNING: does not resolve to a docs/adr record (validate should have caught this).\n", slug)
				continue
			}
			w("- adr:%s\n", slug)
		}
		w("\n")
	}

	// 4. Wiki refs — path only (the session reads what it needs, when it needs it).
	if len(feat.WikiRefs) > 0 {
		w("## Reference pages (read what you need; do not assume the content)\n\n")
		for _, ref := range feat.WikiRefs {
			path, _ := wikiRefInfo(root, ref)
			if path == "" {
				w("- [[%s]] — WARNING: no matching page under docs/wiki/pages/.\n", ref)
				continue
			}
			w("- %s\n", path)
		}
		w("\n")
	}

	// 5. Seeds — pre-authored artifacts to fold into this feat's spec.
	if seeds := featSeedFiles(root, doc.Slug, feat.Slug); len(seeds) > 0 {
		w("## Seeds (pre-authored inputs for this feat's spec)\n\n")
		for _, s := range seeds {
			w("- %s\n", s)
		}
		w("\n")
	}

	// 6. Executor Notes — verbatim.
	if doc.ExecutorNotes != "" {
		w("## Executor Notes\n\n%s\n\n", doc.ExecutorNotes)
	}

	// 7. Graph-first consultation.
	w("## Consult the knowledge graph BEFORE reading code\n\n")
	w("- `csdd graph query <term>` — find the nodes for a concept.\n")
	w("- `csdd graph explain <id>` — see a node's neighborhood and provenance.\n")
	w("- `csdd graph path <a> <b>` — trace how two artifacts connect.\n")
	w("Traverse the graph to locate the relevant code; do not grep the whole tree.\n\n")

	// 8. Self-checks. The loop trusts the session's judgment but verifies its
	// artifacts (R10), so the brief states both halves: what only the session can
	// check, and what the gate will check regardless of what the verdict claims.
	w("## Before you declare `done` — run these checks YOURSELF\n\n")
	w("Run each of these ONCE, here at feat exit — not after every task:\n\n")
	w("- `csdd spec validate %s` (the spec is structurally sound: EARS, traceability, tasks)\n", feat.Slug)
	w("- `csdd graph analyze --strict` (spec↔task↔component traceability holds)\n")
	w("- every task box in specs/%s/tasks.md is `[x]`\n\n", feat.Slug)
	w("DELEGATE the command gate to the `quality-gate` sub-agent, dispatched together\n")
	w("with `code-reviewer` (and `security-reviewer` when auth/secrets/input were\n")
	w("touched) once the last implementer has finished. It runs the commands below\n")
	w("plus the Tier-3 `csdd spec test-report %s --run --lang <lang>` (coverage on,\n", feat.Slug)
	w("no --fast) and returns PASS/FAIL with the real failing output. Running them\n")
	w("inline instead lands the whole suite, lint and typecheck output in YOUR\n")
	w("context, which you then re-read on every remaining turn. The reviewers are\n")
	w("limited by their model and the gate by CPU, so they overlap for free.\n\n")
	for _, g := range planGateCommands(doc) {
		w("- %s\n", g)
	}
	w("\n## What the loop checks before accepting `done`\n\n")
	w("The loop trusts your JUDGMENT — it never reviews your code or re-runs your\n")
	w("suites — but it does check your ARTIFACTS. A `done` verdict is accepted only\n")
	w("when all of these hold on disk:\n\n")
	w("- every task box in specs/%s/tasks.md is `[x]`\n", feat.Slug)
	w("- specs/%s/spec.json has all three phases approved and `ready_for_implementation` true\n", feat.Slug)
	w("- specs/%s/test-report.json is green with no open attentions\n\n", feat.Slug)
	w("If any of them fails, your `done` becomes a `continue` and comes back to the\n")
	w("next session with a note naming what was missing. A feat that spends its whole\n")
	w("attempt budget is stopped and surfaced for a human.\n\n")
	w("An honest `continue` and a refused `done` cost the same one attempt — but the\n")
	w("`continue` spends it carrying YOUR handoff, which says what you learned and\n")
	w("what to try next. A refused `done` spends it to be told what you could have\n")
	w("checked yourself.\n\n")
	w("Only then return `{\"status\":\"done\"}`. If you ran out of room first, return\n")
	w("`{\"status\":\"continue\"}` with the handoff in `summary`.\n")

	return b.String(), nil
}

// planGateCommands renders the plan's Quality Gates as backticked commands, or a
// placeholder note when the plan declared none.
func planGateCommands(doc *PlanDoc) []string {
	if len(doc.Gates) == 0 {
		return []string{"(no Quality Gates declared in the plan)"}
	}
	out := make([]string, 0, len(doc.Gates))
	for _, g := range doc.Gates {
		out = append(out, fmt.Sprintf("`%s` (%s)", g.Command, g.Label))
	}
	return out
}

// wikiRefInfo resolves a [[wiki-page]] ref to its workspace path and frontmatter
// description (or title), reading only the frontmatter — never inlining the body
// (R7.2). Returns empty path when no page matches.
func wikiRefInfo(root, ref string) (path, desc string) {
	dir := filepath.Join(paths.DocsWiki(root), "pages")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	target := normalizeWikiSlug(ref)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if normalizeWikiSlug(strings.TrimSuffix(e.Name(), ".md")) != target {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("docs", "wiki", "pages", e.Name()))
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return rel, ""
		}
		fm := frontmatter.Parse(string(data))
		d := fm.AsString("description", "")
		if d == "" {
			d = fm.AsString("title", "")
		}
		return rel, d
	}
	return "", ""
}

// featSeedFiles returns the workspace paths of every file under
// docs/plans/<slug>/seeds/<feat>/, sorted, for the brief's seed list.
func featSeedFiles(root, slug, feat string) []string {
	dir := filepath.Join(seedsDir(Dir(root, slug)), feat)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join("docs", "plans", slug, "seeds", feat, e.Name())))
	}
	sort.Strings(out)
	return out
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
