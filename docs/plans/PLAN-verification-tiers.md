# Master Plan — Reallocating the verification budget of `csdd plan run`

> **Audience:** the LLM agent (and humans) implementing this end-to-end.
> **Status:** draft for review — **revision 4** (2026-07-19). Not yet approved.
> **Companions:** `docs/plans/PLAN-plan-mode.md` (commit `9455242`, removed by `13fb01b`)
> — this plan tunes the runner that plan defined.
> **Evidence base:** one `csdd plan run` of `openrouter-gateway` in `violeta-chat`
> (14 tasks, 13 implementers logged, 2h19 wall, ~1.35M implementer tokens); plus a
> full audit of the template corpus and `internal/plan/` (2026-07-19).
> **Storage note:** `docs/` was emptied by `13fb01b chore: remove docs`. That is
> deliberately reversed here: `docs/plans/` is revived as hand-authored plan storage,
> alongside the CLI-written `docs/graph/`. This file is the first entry back.

## Revision history

**r1 → r2** corrected four material errors: blast radius (4 → 7+ files —
`rules/definition-of-done.md.tmpl` alone would have nullified the change); the time
model (wall clock is `Σ max(batch)`, not `Σ ÷ concurrency`, so ~25% not ~40%); an
unsound requirement (the orchestrator cannot read a shared artifact with no task
attribution); and scope (batch width and task sizing are the dominant term).

**r2 → r3** changed what the plan *is*. r2 only cut verification. r3 found the
verification budget is spent backwards and **reallocates** it: cuts where errors are
cheap, adds where they are expensive (Requirements 10, 11), with the safety net
landing **before** the cuts.

**r3 → r4** removes the derived diff artifact (Requirement 12). `diff-report.json`
is a cache of `git diff` that is itself committed to git — the repo versions a file
describing what the repo already records. It is also one of the three artifacts that
make concurrent implementers unsafe, so deleting it shrinks Phase 5's problem before
Phase 5 has to solve it.

---

## 1. Context and vision

### 1.1 The core finding: the verification budget is inverted

The templates are maximally paranoid:

> `verify-change:19` — *"A prior run doesn't count — edits since then may have broken it."*
> `verify-change:75` — *"You're trusting a previous run, or an agent's 'success', instead of output you just saw."* — a STOP-level red flag.

The runner is entirely credulous:

> `runner.go:211-215` — *"Nothing here verifies the work — the loop trusts the session (Ralph-style)."*
> `runner_exec.go:253-289` — `parseVerdict` validates only that the string is `done|continue`.

The result:

| Failure | Where it is caught | Cost if missed |
|---|---|---|
| A false "tests pass" inside one task | the 2nd of ~5 redundant suite runs | low |
| A false `{"status":"done"}` on a whole feat | **nowhere** | high — the feat is marked done and the loop advances |

Maximum distrust where errors are cheap; zero where they are expensive. And the
checks that would close the expensive hole are the **cheapest in the system**:
counting `[x]` in `tasks.md` and running `csdd spec validate` cost under a second,
against the ~30 min per feat currently spent on redundant re-execution.

**A second hole compounds it.** `ValidateTestCommandForLang` (`reports.go:314-332`)
validates a custom `--cmd` by substring — if it contains `"pytest"`, it passes. In
the reference run, twice:

```text
spec test-report openrouter-gateway --run --lang python --path backend \
  --cmd "uv run pytest --junitxml=junit.xml --cov --cov-report=xml \
         --ignore=tests/unit/test_pinned_embedder.py"        · 2m12s
```

`test-report.json` was written **green with a test file excluded**, no attention
raised. The Iron Law polices *whether* a command ran, never *what* it ran — worse
than having no evidence, because the artifact then asserts green with authority.

**A third: an artifact that asserts without knowing.** `diff-report.json` caches
`git diff` into a per-spec JSON that is itself committed (`gitignore.go:24-32`
ignores only the binary and the pinggy files). The repo versions a file describing
what the repo already records. `tdd-cycle:88-89` admits the failure mode in its own
mitigation — *"Pair it with the test-report run so the test metrics and the change
view never show different states"* — a pairing rule exists only because the cache
drifts by construction.

**This plan therefore does three things, in this order:** install the cheap checks
that close the expensive holes, delete the artifact that cannot be made correct, then
remove the expensive redundancy that closed cheap holes by accident.

### 1.2 Cache versus record — the test that decides what stays

`test-report.json` shares `diff-report.json`'s contention problem but survives the
same test, and the distinction is the general rule:

| Artifact | Derivable from the repo? | Verdict |
|---|---|---|
| `diff-report.json` | **Yes** — it is `git diff` | a *cache*; delete it |
| `test-report.json` | **No** — nothing in the tree knows the suite passed at 14:32 | a *record*; keep and harden it |

Persist records. Never persist caches of the version-control system inside the
version-control system.

### 1.3 What the methodology is demonstrably buying (keep this)

The redundancy is an implementation defect, not a verdict on SDD+TDD. The same log
shows the discipline paying off in ways that do not happen by accident:

- **API verification against the installed library** — dozens of
  `inspect.signature(...)`, `model_fields[...]`, `_invocation_params` calls. The
  agent checked real signatures instead of writing against a remembered API
  (`implementer.md:38-40`). The most common LLM coding failure, closed.
- **Secret-leak sentinel tests** — `print('sk-sentinel-do-not-leak' in repr(e))`.
  No task asked for it; the discipline produced it.
- **Pre-existing-error discrimination** — `git stash && mypy src; git stash pop`,
  to confirm the errors were the agent's before "fixing" them. Prevents sprawl.
- **Boundary discipline** — 14 tasks across 13 boundaries, no visible cross-invasion.
- **Cost of the SDD ceremony: ~10 of 139 min (~7%).** The ceremony is not the
  problem. The redundant execution is.

One honest qualification: **SDD's review value comes from the human**, and
`plan-dev:17-31` removes the human by design. What survives is mechanical validation
(EARS, traceability, boundary-to-component matching) — real, but smaller than the
interactive flow. In the autonomous path the phases are a *structuring* device, not a
review. Worth the 7%; not to be sold as a gate. TDD loses nothing without the human:
RED-first mechanically closes the failure `tdd-cycle:107` itself names.

### 1.4 Where the time goes (measured, then modelled)

**Per-command timings** observed in the reference run:

| Command | Observed | Note |
|---|---|---|
| `ruff check .` + `ruff format --check .` | 0–2s | negligible — **do not defer** |
| `mypy src` | 28–57s (~35s avg) | ~15 invocations ≈ 9 min |
| `pytest -q --no-cov` (full) | 25s–1m19 | the fast path |
| `pytest -q` (full, coverage on) | 1m39–2m18 | ~3× the fast path |
| `csdd spec test-report --run` | 1m02–2m27 | full suite **with** coverage |
| `csdd spec diff-report` | 0–3s | negligible — its removal is *not* a speed fix |

**The critical path is batched, not pooled.** The log shows phase barriers — an
explicit `Verify mypy/ruff after phase 2` between batches. With barriers, wall clock
is `Σ max(batch)`:

| Batch | Tasks | Durations | Max |
|---|---|---|---|
| B1 | 1, 2 | 11m22, 7m23 | **11m22** |
| B2 | 3, 4, 5 | 19m52, 9m00, 8m27 | **19m52** |
| B3 | 6, 7, 8 | 22m13, 15m18, 12m32 | **22m13** |
| B4 | 9, 10, 13 | 22m36, 7m40, 7m30 | **22m36** |
| B5 | 14, 11 | 31m46, 14m36 | **31m46** |
| | | | **Σ = 108 min** |

108 (batches) + ~31 (orchestrator re-verification, spec authoring, git/PR) = **139
min**, reconciling exactly with the observed 2h19. The model is validated against the
data, not assumed.

**Two consequences:**

- Verification savings reach only the slowest task per batch — 5 tasks, not 13. That
  caps the doctrine phase at ~25–30 min.
- The four long poles (31m46, 22m36, 22m13, 19m52) are **96 of the 108 min**.
  Verification is ~6–8 min of each; the rest is implementation work proportional to
  context volume (140k, 111k, 179k, 108k tokens).

**The dependency floor.** Ordering the same 13 tasks by dependency depth rather than
phase label yields four levels — settings → factories → services → integrations —
whose maxima sum to **~86 min**: a ~22-min saving from packing alone.

### 1.5 Value thesis (why this build order)

- **Instrumentation is nearly free.** The session `result` event already carries
  `duration_ms`, `duration_api_ms`, `total_cost_usd`, `usage` and `modelUsage`; the
  runner keeps the whole raw line (`session_stream.go:103-105`) and discards all but
  `status`/`summary` (`runner_exec.go:153-156`). Baseline first, and cheaply.
- **The net before the cut.** Requirements 10 and 11 are seconds of runtime and
  mostly wire up code that already exists (`deriveFeatStatus`, `status.go:70-105`,
  is already called at `runner.go:359`). Landing them first means no window in which
  redundancy is gone and nothing replaced it.
- **Deleting beats fixing.** Requirement 12 removes ~500 LOC, a dashboard view, an
  MCP tool, and one of three contended artifacts — work Phase 5 would otherwise have
  to do on an artifact that should not exist.
- **The doctrine change is template-only** — but only works if it covers
  `definition-of-done.md.tmpl`, a *rule* (ambiently loaded) that binds harder than
  any skill needing invocation.
- **Artifact contention gates parallelism.** After Requirement 12, `tasks.md` and
  `test-report.json` remain; both are addressed in Phase 5.
- **The Go command-mode change touches all seven languages** and is last because it
  is the riskiest.

---

## 2. Scope

**In scope:** the seven templates that mandate verification; verdict handling and
session-result capture in `internal/plan/`; the per-spec evidence artifacts and
command validation in `internal/session/`; full removal of the diff artifact across
CLI, MCP, dashboard, and templates.

**Out of scope, deliberately:**

- **Moving the workspace off `/mnt/c`.** Environment, not code. Prerequisite (§8,
  open decision 2) — it multiplies every number here.
- **Task-sizing heuristics.** Splitting a 31-minute task is a task-*authoring*
  problem. Phase 5 packs the batches we have; resizing them is a sibling plan (§8,
  open decision 3).
- **Feat-level parallelism.** The runner is strictly sequential (`seq.go:22-29`) and
  `Feat.Parallel` is parsed but honored for ordering only (`model.go:45`).
- **The spec phase gates.** Untouched.

---

## 3. Design principles (inviolable)

1. **Claim scope equals evidence scope.** A task may claim "this task is green" from
   a Tier-2 run. Only an integration gate may claim "the feat is green".
2. **Trust the judgment, verify the artifacts.** The loop may accept the session's
   reasoning; it must not accept unchecked claims about files it can read in
   milliseconds.
3. **Persist records, never caches of the VCS.** If git can recompute it, git owns it.
4. **Attribution survives.** Every level keeps a green point to bisect from.
5. **Cheap checks stay eager.** Ruff costs ~0. Only expensive checks move to a gate.
6. **The Iron Law is scoped, never deleted.** The failure it closes — claiming green
   without running — stays closed.
7. **Evidence names its own limits.** A report produced by a command that excluded
   tests must say so.
8. **Coordination flows through the agent protocol, not through files.**

---

## 4. Requirements (content of `requirements.md`)

### Requirement 1: The three-tier verification contract

1. THE SYSTEM SHALL define exactly three verification tiers — inner loop, task exit,
   and integration — each with a declared command scope and a declared claim scope.
2. WHERE a check completes in under 5 seconds THE SYSTEM SHALL run it at task exit
   rather than deferring it to an integration gate.
3. THE SYSTEM SHALL exclude coverage collection from every tier except the feat-exit
   integration gate.
4. THE SYSTEM SHALL state the tier contract identically in every template that
   mandates verification, so no template imposes a stricter rule than another.

### Requirement 2: A single task-exit gate

1. WHEN an implementer completes one task THEN the system SHALL run the full suite at
   most once for that task.
2. THE SYSTEM SHALL make `csdd spec test-report <feat> --run` that single gate, so
   the existing rule that a task box is checked only against recorded green evidence
   continues to hold.
3. THE SYSTEM SHALL NOT require a typecheck or a build at task exit.
4. WHEN the task-exit gate passes THEN the system SHALL treat its recorded output as
   the evidence for that task without re-running the suite.

### Requirement 3: Scoped Iron Law

1. THE SYSTEM SHALL permit a layer to satisfy its evidence obligation with a run
   performed earlier in the same session WHERE that run's scope covers the claim and
   its result is recorded.
2. IF no recorded run covers the claim's scope THEN the system SHALL require a fresh
   run before the claim is made.
3. THE SYSTEM SHALL permit an orchestrator to accept a sub-agent's reported result as
   evidence for that sub-agent's task.
4. THE SYSTEM SHALL continue to reject any "done", "it works", or "tests pass" claim
   that has no recorded output at all.

### Requirement 4: Orchestrator non-duplication

1. WHILE the plan-run orchestrator is dispatching implementers THE SYSTEM SHALL take
   each task's result from the implementer's structured return message.
2. THE SYSTEM SHALL NOT read a shared evidence artifact to determine whether an
   individual task passed.
3. THE SYSTEM SHALL run the plan's Quality Gate commands exactly once per feat, at
   the feat-exit self-check.
4. IF an implementer returns without a reported result THEN the system SHALL treat
   that task as unverified and re-dispatch it rather than verifying it inline.

### Requirement 5: Integration gate at batch merge

1. WHEN a batch of concurrent implementers completes THEN the system SHALL run the
   full suite and the typecheck once for the merged result.
2. IF the batch integration gate fails THEN the system SHALL dispatch a fix task
   naming the failing check before dispatching the next batch.
3. THE SYSTEM SHALL record which tasks composed each merged batch.

### Requirement 6: Evidence artifacts survive concurrent authors

1. WHEN two implementers complete tasks of the same spec concurrently THEN the system
   SHALL preserve both results.
2. THE SYSTEM SHALL attribute each recorded test result to the task that produced it.
3. THE SYSTEM SHALL keep the dashboard's remaining views working against the recorded
   evidence without requiring a dashboard change in the same release.

### Requirement 7: Batch packing by dependency depth

1. THE SYSTEM SHALL group tasks for concurrent dispatch by dependency depth rather
   than by phase heading.
2. THE SYSTEM SHALL dispatch concurrently every task whose `_Depends:_` set is
   already satisfied and whose `_Boundary:_` is not held by a running implementer.
3. WHERE a phase heading and the dependency graph disagree THE SYSTEM SHALL honor the
   dependency graph.

### Requirement 8: Fast and evidence test-command modes

1. THE SYSTEM SHALL expose, for every supported language, a fast test command that
   omits coverage collection.
2. WHEN `--run` executes without an explicit `--cmd` and evidence is requested THEN
   the system SHALL use the coverage-bearing command.
3. THE SYSTEM SHALL keep the existing coverage-bearing commands unchanged as the
   evidence default, so no current invocation changes behavior.

### Requirement 9: Session instrumentation

1. WHEN a session ends THEN the system SHALL record its duration, token usage, and
   cost alongside the verdict.
2. THE SYSTEM SHALL record one entry per session attempt, including attempts that
   returned `continue` or failed.
3. WHEN an optimization lands THEN the system SHALL permit comparing a feat's run
   against an earlier recorded run.

### Requirement 10: Mechanical verdict verification

1. WHEN a session returns `done` THEN the system SHALL verify, before accepting it,
   that every task box in `specs/<feat>/tasks.md` is checked, that the spec
   validates, and that the recorded test evidence is green with no open attentions.
2. THE SYSTEM SHALL perform these checks without invoking a model and without
   executing the project's test suite.
3. IF any check fails THEN the system SHALL convert the verdict to `continue` with a
   generated handoff naming each failed check.
4. THE SYSTEM SHALL count each converted verdict toward a per-feat attempt bound.
5. IF the per-feat attempt bound is reached THEN the system SHALL stop that feat and
   surface it, rather than continuing indefinitely.

### Requirement 11: Evidence command integrity

1. WHEN a custom `--cmd` contains a test-exclusion or test-selection flag THEN the
   system SHALL record an attention on the resulting report naming that flag.
2. THE SYSTEM SHALL treat a report carrying such an attention as not green for the
   purposes of the definition of done.
3. THE SYSTEM SHALL record the executed command verbatim on the report.

### Requirement 12: Removal of the derived diff artifact

1. THE SYSTEM SHALL NOT persist a git-derived diff to a per-spec JSON artifact.
2. THE SYSTEM SHALL remove the `spec diff-report` command, its MCP tool, and the
   dashboard view that consumes the artifact.
3. THE SYSTEM SHALL remove every instruction that mandates producing or refreshing
   the diff artifact.
4. WHERE a workspace still contains the orphaned artifact THE SYSTEM SHALL ignore it
   without error.
5. THE SYSTEM SHALL announce the removed MCP tool as a breaking change in the release
   notes.

---

## 5. Design (content of `design.md`)

### 5.1 What actually runs today

Under `plan run`, a single task triggers the suite up to six times, plus a diff
refresh:

```text
1. tdd-cycle:62-64        "Widen the net"            suite + lint/typecheck
2. tdd-cycle:67-78        test-report --run          suite + COVERAGE
   tdd-cycle:80-89        diff-report                git cache, "paired so they never drift"
3. implementer:55-56      "Widen the net"            suite + lint/typecheck/build
4. implementer:57-58      → verify-change:38-46      full gate: tests/lint/typecheck/build
5. implementer:59-63      test-report --run          suite + COVERAGE
6. pre-push.tmpl          the push gate              suite, not bypassable
   plan-dev:102-107       plan Quality Gates         the orchestrator runs them again
```

After: one Tier-2 run per task; no diff artifact; coverage, typecheck and build at
integration; and the verdict gate catching what redundancy never could.

### 5.2 Goals / Non-Goals

**Goals.** Move verification from where errors are cheap to where they are expensive;
delete the artifact that cannot be kept correct; remove redundant executions;
preserve attribution; make concurrent implementers safe; pack by dependency depth.

**Non-Goals.** Faster individual tests; changing what the gates check; touching the
spec validators; feat-level parallelism.

### 5.3 Reuse map (verified against the current tree)

| # | Path | Current content | Change |
|---|---|---|---|
| 1 | `rules/definition-of-done.md.tmpl:15-18,26,36-39` | test-report absence is *"a blocked item"*; *"Full verification gate is green: tests, lint, typecheck, build"* | **Split by tier.** A rule file is ambiently loaded, so leaving it intact nullifies every other edit. |
| 2 | `skills/verify-change/SKILL.md.tmpl:15-19,67,75,87` | Iron Law + *"A prior run doesn't count"* + trusting an agent's success is a red flag | Scoped per Requirement 3 |
| 3 | `skills/tdd-cycle/SKILL.md.tmpl:62-64,67-89` | "Widen the net" + test-report + diff-report | §4 deleted; §5 becomes the single Tier-2 gate; diff block deleted |
| 4 | `agents/implementer.md.tmpl:55-63,110` | steps 3, 4, 5 each trigger a run; diff-report in the criteria | collapsed to one gate + one report; diff lines deleted |
| 5 | `skills/plan-dev/SKILL.md.tmpl:78-83,102-107` | sub-agents verify, then the orchestrator verifies again | no-reverify + batch gate + dependency packing; diff mention deleted |
| 6 | `agents/wf-development.md.tmpl:113-116` | green suite per task + test-report + coverage | aligned to the tier contract |
| 7 | `skills/pr-review/SKILL.md.tmpl:38` | *"Refresh the spec's change record first"* | deleted (only diff-report touch in this file) |
| 8 | `root/pre-push.tmpl` | full suite per push, not bypassable | **left as-is** — last mechanical gate, once per push |
| 9 | `internal/plan/runner.go:240-265` | verdict accepted verbatim | verdict gate (Requirement 10) |
| 10 | `internal/plan/status.go:70-105` | `deriveFeatStatus` already computes phase state from disk; called at `runner.go:359` | reused at verdict time — near-free |
| 11 | `internal/plan/runner_exec.go:153-156` | parses only `status`/`summary` | also capture duration/usage/cost |
| 12 | `internal/session/reports.go:314-332` | `--cmd` validated by substring only | flag-level integrity check |
| 13 | `internal/session/reports.go:266-281` | all 7 languages carry coverage | add fast variants |
| 14 | `internal/session/reports.go:23-31` | `SpecReport` — no task field, no history | per-task attribution |
| 15 | `internal/cli/diff_report.go` (210 LOC), `internal/session/diff.go` (290 LOC), `spec.go:160`, `session.go:96` | the diff artifact | **deleted** |
| 16 | `internal/web/frontend/src/components/DiffView.tsx` | the Changes view | **deleted** |
| 17 | `mcp-server/src/tools/spec.ts:152-171` + README:156 | `csdd_spec_diff_report` | **deleted** — breaking change |

The coverage default is not Python-specific — `--cov`, `--coverage`,
`jacoco:report`, `-coverprofile` and `llvm-cov` all appear in the same map.

### 5.4 The tier contract

| | Tier 1 — inner | Tier 2 — task exit | Tier 3 — integration |
|---|---|---|---|
| Trigger | each RED→GREEN | implementer finishes | batch merge; feat exit |
| Tests | focused file, `-x` | full suite, fast mode | full suite |
| Lint | — | touched files | whole tree |
| Typecheck | — | — | yes |
| Build | — | — | yes |
| Coverage | — | — | feat exit only |
| Claim | "this behavior works" | "this task is green" | "the feat is green" |
| Cost | seconds | ~1m15 | ~2m / ~3m |

### 5.5 The verdict gate (Requirement 10)

The runner spawns exactly one subprocess — `claude` — and verifies nothing today.
The gate adds no subprocess and no model call. It reads three artifacts already on
disk:

| Check | Source | Cost |
|---|---|---|
| every task box `[x]` | `specs/<feat>/tasks.md` | parse, ~ms |
| phases approved, `ready_for_implementation` | `spec.json` via `deriveFeatStatus` (`status.go:70-105`) | already implemented |
| evidence green, no open attentions | `specs/<feat>/test-report.json` via `loadSpecReport` (`reports.go:50`) | ~ms |

On failure the verdict becomes `continue` with a generated handoff — self-healing,
because the next session is told exactly what is missing.

**Coupling that must not be missed.** `runner.go:181-186` resets the stall counter on
`continue` (`case iterAdvanced, iterContinue: stall = 0`), so a feat can `continue`
indefinitely — bounded only by the global `MaxIterations` of 100
(`runner.go:553-557`). A rejected `done` becomes a `continue`, so **Requirement 10
without the per-feat bound (10.4, 10.5) converts a silent false-done into a silent
infinite loop.** They ship together; this is why the pre-existing bound defect (§8)
is folded into this plan rather than deferred.

### 5.6 Evidence integrity (Requirement 11)

`ValidateTestCommandForLang` (`reports.go:314-332`) returns true if the command
contains any marker substring for the language. A flag-level check runs alongside it:
exclusion and selection flags (`--ignore`, `--deselect`, `-k`, `-m`, `--last-failed`
for pytest; `--testPathIgnorePatterns`, `-t`, `--onlyChanged` for jest; `-run` for
go; and the equivalents for the remaining languages) produce an **attention** rather
than a hard rejection — legitimate exclusions exist, but they must be visible.

This reuses existing machinery end to end: `definition-of-done.md.tmpl:16-18` already
requires evidence with *"no open attentions"*, so an attention automatically blocks
the definition of done, and Requirement 10.1 already refuses a `done` whose evidence
carries one. No new enforcement path is needed.

### 5.7 Removing the derived diff artifact (Requirement 12)

`diff-report.json` records the merge-base of HEAD and the base ref → working tree.
Every byte of that is recomputable from git, and the artifact is **committed**
(`gitignore.go:24-32` ignores only the binary and the pinggy files), so the repo
versions a description of what the repo records. `tdd-cycle:88-89`'s pairing rule
exists only because the cache drifts the moment any file changes.

There is no detached-consumer case that would justify the cache: `csdd export` is
`{kiro, codex}` — format conversion, not a static dashboard — so every consumer of
the Changes view already has the repo and can run git.

**Full removal** is chosen over keeping the view with a live git implementation. The
view duplicates `git diff` and the host's PR view; preserving it would retain ~290
LOC of diff parsing for a screen nobody opens with a PR open beside it. If that
judgement proves wrong, re-adding a live (unpersisted) view is a small, separable
change.

**Work split, to avoid two tasks editing one file.** The removal task owns the Go
code, the MCP tool, the dashboard component, and `pr-review:38` (the only template
not otherwise rewritten). The `tdd-cycle`, `implementer`, and `plan-dev` diff lines
are deleted inside their own doctrine rewrites, where those files are already open.

**Migration.** `loadSpecDiff` already tolerates a missing file, so nothing breaks
when the artifact disappears; orphaned files are removed with a documented `git rm`.
Deleting the MCP tool breaks anyone scripting against it — Requirement 12.5 requires
it in the release notes, with a minor version bump.

### 5.8 Evidence artifacts under concurrency

After Requirement 12, two contended artifacts remain.

`SpecReport` (`reports.go:23-31`) carries `Feature`, `UpdatedAt`, `Command`, `Tests`,
`Coverage` — **no task field, no history** — and `spec.go:888` rebuilds it from
scratch on every call. Thirteen implementers overwrite one file thirteen times; three
concurrent worktrees conflict on it and on every `[x]` edit to `tasks.md`.

The fix keeps one file per spec (the dashboard contract) but makes it accumulative: a
`tasks` map keyed by task ID, each entry carrying that task's counts and timestamp,
with the existing top-level fields retained as the latest-run rollup so the dashboard
keeps working unchanged (Requirement 6.3).

`tasks.md` contention moves to the orchestration layer: the implementer reports the
task green and the **orchestrator** checks the box after merging the batch, so only
one writer ever touches the file.

### 5.9 Orchestration

The runner verifies nothing today and spawns one subprocess; every gate named in
`brief.go:168-177` is run by the session itself. So Requirements 1–8 are template
changes only. Requirements 9–12 carry the Go, MCP, and web work.

### 5.10 File structure plan

```text
internal/templater/templates/
  rules/definition-of-done.md.tmpl        MODIFIED  tier split (load-bearing)
  skills/verify-change/SKILL.md.tmpl      MODIFIED  tiers + scoped Iron Law
  skills/tdd-cycle/SKILL.md.tmpl          MODIFIED  Tier 1/2 split, drop §4 + diff block
  skills/plan-dev/SKILL.md.tmpl           MODIFIED  no-reverify, batch gate, dep packing
  agents/implementer.md.tmpl              MODIFIED  single gate + single report
  agents/wf-development.md.tmpl           MODIFIED  align to tier contract
  skills/pr-review/SKILL.md.tmpl          MODIFIED  drop the diff refresh
  plan/plan.md.tmpl                       MODIFIED  fix the runner-gate contradiction
internal/plan/runner.go                   MODIFIED  verdict gate + per-feat bound
internal/plan/runner_exec.go              MODIFIED  capture duration/usage/cost
internal/plan/ledger.go                   MODIFIED  persist per-session records
internal/plan/runner_test.go              MODIFIED  verdict gate + bound coverage
internal/session/reports.go               MODIFIED  integrity, attribution, fast commands
internal/session/reports_test.go          MODIFIED  integrity + concurrency + parity
internal/session/session.go               MODIFIED  drop the Diff field
internal/cli/spec.go                      MODIFIED  drop the diff-report dispatch
internal/templater/templater_test.go      MODIFIED  assert tier language is consistent
internal/cli/diff_report.go               DELETED
internal/cli/diff_report_test.go          DELETED
internal/session/diff.go                  DELETED
internal/session/diff_test.go             DELETED
internal/web/frontend/src/components/DiffView.tsx   DELETED
mcp-server/src/tools/spec.ts              MODIFIED  drop csdd_spec_diff_report
mcp-server/test/tools.test.ts             MODIFIED  drop its cases
mcp-server/README.md                      MODIFIED  drop its row
```

### 5.11 Requirements Traceability

| Requirement | Design | Task |
|---|---|---|
| 1.1–1.4 | 5.4, 5.3 | 5 |
| 2.1–2.4 | 5.3, 5.4 | 6, 7 |
| 3.1–3.4 | 5.3 | 5 |
| 4.1–4.4 | 5.9 | 8 |
| 5.1–5.3 | 5.4, 5.9 | 9 |
| 6.1–6.3 | 5.8 | 10 |
| 7.1–7.3 | 5.9 | 11 |
| 8.1–8.3 | 5.3, 5.10 | 12, 13 |
| 9.1–9.3 | 1.5, 5.10 | 1 |
| 10.1–10.5 | 5.5 | 2 |
| 11.1–11.3 | 5.6 | 3 |
| 12.1–12.5 | 5.7 | 4, 6, 7, 8 |

---

## 6. Tasks (content of `tasks.md`) — TDD, boundaries, dependencies

### Phase 1: Baseline (M0)

- [ ] 1. Capture the session `result` event's duration, usage, and cost where the
      verdict is parsed, and persist one record per session attempt.
      _Requirements: 9.1, 9.2, 9.3_ _Boundary: Instrumentation_

### Phase 2: The evidence layer (M1) — add the net, delete the lie

- [ ] 2. (P) Gate the `done` verdict on the three on-disk checks, converting a failed
      gate to `continue` with a generated handoff, and bound per-feat attempts.
      _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_ _Boundary: VerdictGate_
- [ ] 3. (P) Detect test-exclusion and selection flags in a custom `--cmd` and record
      them as attentions, with the executed command stored verbatim.
      _Requirements: 11.1, 11.2, 11.3_ _Boundary: EvidenceIntegrity_
- [ ] 4. (P) Delete the diff artifact — CLI command, session model, dashboard view,
      MCP tool, and the `pr-review` instruction — tolerating orphaned files and
      recording the breaking change.
      _Requirements: 12.1, 12.2, 12.4, 12.5_ _Boundary: DiffArtifactRemoval_

### Phase 3: Doctrine (M2 — templates only)

- [ ] 5. Rewrite the tier contract across `definition-of-done` and `verify-change`,
      scoping the Iron Law so a recorded in-session run, or a sub-agent's reported
      result, satisfies the evidence obligation.
      _Requirements: 1.1, 1.2, 1.3, 1.4, 3.1, 3.2, 3.3, 3.4_ _Boundary: VerificationDoctrine_ _Depends: 2_
- [ ] 6. (P) Split `tdd-cycle` into the Tier-1 focused loop and one Tier-2 exit gate,
      deleting the separate "widen the net" run and the diff-report block.
      _Requirements: 2.1, 2.2, 2.4, 12.3_ _Boundary: TddCycleLoop_ _Depends: 4, 5_
- [ ] 7. (P) Collapse `implementer` steps 3–5 into a single gate plus a single report,
      dropping the task-level typecheck, build, and diff-report lines.
      _Requirements: 2.1, 2.3, 2.4, 12.3_ _Boundary: ImplementerGate_ _Depends: 4, 5_

### Phase 4: Orchestration (M3)

- [ ] 8. Forbid per-slice re-verification in `plan-dev`; results come from the
      implementer's return message, the plan Quality Gates run once at feat exit, and
      the diff-report mention is dropped.
      _Requirements: 4.1, 4.2, 4.3, 4.4, 12.3_ _Boundary: PlanDevOrchestration_ _Depends: 7_
- [ ] 9. Add the batch-merge integration gate with its fix-dispatch path, align
      `wf-development`, and correct the `plan.md.tmpl` runner-gate contradiction.
      _Requirements: 5.1, 5.2, 5.3, 1.4_ _Boundary: PlanDevOrchestration_ _Depends: 8_

### Phase 5: Concurrency (M4)

- [ ] 10. Make `SpecReport` accumulative and task-attributed, retaining the top-level
      fields as the latest-run rollup for the dashboard.
      _Requirements: 6.1, 6.2, 6.3_ _Boundary: EvidenceArtifacts_ _Depends: 3_
- [ ] 11. Dispatch by dependency depth rather than phase heading, with the
      orchestrator as the single writer of `tasks.md` checkboxes.
      _Requirements: 7.1, 7.2, 7.3_ _Boundary: PlanDevOrchestration_ _Depends: 9, 10_

### Phase 6: Command modes (M5)

- [ ] 12. (P) Add fast (coverage-free) test commands for all seven languages with an
      accessor mirroring `DefaultTestCommand`, leaving evidence defaults untouched.
      _Requirements: 8.1, 8.3_ _Boundary: TestCommandModes_
- [ ] 13. Surface the fast mode through `--run` and cite it from the Tier-2 templates.
      _Requirements: 8.2_ _Boundary: TestCommandModes_ _Depends: 12_

### Phase 7: Validation (M6)

- [ ] 14. Re-run the reference feat and compare every projection in §7 against the
      records captured by task 1.
      _Requirements: 9.3_ _Boundary: Instrumentation_ _Depends: 11, 13_

---

## 7. Milestones

| M | Content | Tasks | Projected wall clock |
|---|---|---|---|
| M0 | Baseline captured | 1 | 139 min (unchanged) |
| M1 | **Evidence layer** — verdict gate, command integrity, diff removal | 2–4 | ~139 min (correctness, not speed) |
| M2 | Verification collapsed | 5–7 | ~115 min |
| M3 | Orchestrator stops re-verifying; batch gate | 8–9 | ~108 min |
| M4 | Concurrency unblocked; packing by dependency depth | 10–11 | ~85 min |
| M5 | Fast command modes | 12–13 | ~80 min |
| M6 | Verified against baseline | 14 | measured, not projected |

With the workspace also moved off `/mnt/c` (§8, open decision 2): **~68–75 min** —
roughly **2× faster** than the 2h19 baseline, with three failure modes closed that
are open today.

**Where each saving comes from.** M2–M3 reach only the slowest task per batch
(~25–30 min). M4 is the larger term: packing the same 13 tasks by dependency depth
gives four levels summing to ~86 min against today's 108. **M1 buys no measurable
time and is still first** — the diff artifact costs 0–3s per task, so its removal is
a correctness and maintenance decision, not a performance one, and the verdict gate
is the net that makes every later cut safe.

**Estimate integrity.** Per-command timings and per-implementer durations are
observed. The batch model is validated (it reconciles to 139 min). The ~6–8 min
per-task verification figure is *inferred* from what the templates mandate, not
measured — implementers are opaque in the log. Task 1 exists to replace every
projection here with a measurement, which is why it is first.

---

## 8. Risks and decisions

| Risk | Mitigation |
|---|---|
| Scoping the Iron Law reopens "claimed green without running" | Requirement 3.4 keeps the floor; Tier 2 still runs the full suite; Requirement 10 now catches at the feat level what redundancy caught by accident. |
| One template is missed and silently restores the old doctrine | Exactly what `definition-of-done.md.tmpl` would have done. Requirement 1.4 plus the `templater_test.go` assertion in task 5 make the contract mechanically checkable. |
| The verdict gate turns a false `done` into an infinite `continue` | Requirements 10.4/10.5 — the per-feat bound. §5.5 explains why they cannot ship separately. |
| The verdict gate rejects legitimate `done` verdicts | All three checks read artifacts the session is already required to produce. A rejection means the contract was not met; the handoff names which check, so the next session self-heals. |
| Removing the Changes view loses a review surface | It duplicates `git diff` and the host's PR view, and every consumer has the repo. Re-adding a live, unpersisted view later is small and separable (§5.7). |
| Removing the MCP tool breaks external scripts | Requirement 12.5 — release-note breaking change with a minor bump. |
| A red integration gate after N tasks has no attribution | Requirement 5.1 gates every batch merge, not only feat exit; 5.2 defines the fix path. |
| Deferring `mypy` to Tier 3 lets a type error propagate | Accepted. mypy errors are local and cheap to fix late; ~35s × 13 outweighs it. Revisit if M6 shows cascading type failures. |
| The `SpecReport` change breaks the dashboard | Requirement 6.3 keeps the top-level fields as a rollup; the dashboard is not modified in the same release. |
| Flag detection produces false attentions | Attentions are advisory-with-teeth, not hard rejections; the command is recorded verbatim (11.3) so a reviewer can judge. |
| Templates only reach a workspace on re-init | Document that existing workspaces need `csdd init` / `csdd copy`. |

**Defects found during this analysis.**

1. **`plan/plan.md.tmpl:49-57` is wrong.** It tells plan authors *"The runner
   executes every gate after each implementation step; a red gate blocks advance"*,
   contradicting `commands/csdd-plan-run.md.tmpl:32-33` and the code
   (`runner.go:211-215`). Folded into task 9.
2. **No per-feat iteration bound** (`runner.go:181-186`). Independent of performance,
   but Requirement 10 makes it load-bearing — **folded into task 2**, per §5.5.
3. **`Feat.Parallel` is parsed but honored for ordering only** (`model.go:45`).
   Documented in §2 so nobody plans around concurrency that does not exist.

**Open decisions.**

1. **Tier-2 scope: full suite or affected suite?** Recommendation: full suite in fast
   mode (~1m15, trivial, attribution intact). The graph could map boundary → test
   files — elegant dogfooding, materially more work. Revisit after M6.
2. **Move the workspace off `/mnt/c`?** Prerequisite, not a task here. WSL2's 9p
   layer penalizes pytest collection, `uv`, and git; it multiplies every number
   above. Highest-leverage single action, zero code.
3. **Task sizing.** The 31m46 / 179k-token long poles are an authoring problem. After
   M6 the critical path is dominated by them; a sibling plan against
   `rules/tasks-generation.md.tmpl` should size leaves for an *agent's* context
   budget rather than the current "1–3 hours" human heuristic.
4. **Autonomous phase gates.** `plan-dev:17-31` removes the human, so `approve`
   becomes model-reviewing-model. Keep the mechanical validators; consider dropping
   the word "gate" for the autonomous path, or collapsing the three self-approvals
   into one validated checkpoint. Not costed here (~7% of wall clock).
5. **Should `test-report.json` be gitignored?** It survives the cache-versus-record
   test (§1.2) and must be committed for the dashboard and for review. Noted only so
   the question is closed deliberately rather than by omission.
6. **~~Revive `docs/plans/`?~~ Decided (2026-07-19): yes.** `13fb01b` had removed it;
   this plan is the first entry back. The consequence to settle next is whether the
   four plans that commit removed (`PLAN-knowledge-base`, `PLAN-plan-mode`,
   `PLAN-decision-records`, `PLAN-glossary`) are restored from `9455242` or left in
   history — a half-populated `docs/plans/` reads as if the others were never written.
