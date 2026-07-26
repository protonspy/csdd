# Master Plan — Runner resilience, then the squad

> **Audience:** the LLM agent (and humans) implementing this end-to-end.
> **Status:** draft for review — **revision 4** (2026-07-26). Not yet approved.
> **Companions:** `docs/plans/PLAN-verification-tiers.md` — that plan added the
> verdict gate this one bounds correctly.
> **Evidence base:** one complete `csdd plan run` of `agency-telegram-platform` in
> the `violet` workspace — 31 feats, 10 days (2026-07-16 → 2026-07-26), 94 journal
> entries, 27 feats delivered autonomously. Full tally in §1.
> **Storage note:** hand-authored plan storage, alongside the CLI-written
> `docs/graph/`. Second entry after `PLAN-verification-tiers.md`.

## Revision history

**r3 → r4** M1 implemented, minus R8. The observer was going to be built as a
seam with no consumer — the Telegram notifier already reads the journal and is
better off decoupled, so wiring it through an observer would have replaced working
code to no end. It moves to its own slice with a concrete deliverable instead:
attributing every event line to the dispatch and agent it came from, which is
useful at a squad limit of 1 (sub-agents within one session are already
indistinguishable in a piped log) and mandatory above it.

**r2 → r3** closed R2.2's durable half: the resume line now reaches `log.md`
naming the feats whose attempts died mid-session, not just stdout. R5.2 retired as
already-satisfied — `validate.go` rejects range shorthand, self-deps and unknown
slugs, and approval requires a clean validate, so the scheduler can trust
`Depends`.

**r1 → r2** bounded `--squad-limit` to 1..6 and set its target default to 3
(R7), with the sequencing note that a default above 1 makes the shared account
limiter a hard prerequisite of M3 rather than a refinement of it (R-B).

**r1** — first revision. Written after auditing the `violet` run end-to-end
(`docs/plans/agency-telegram-platform/log.md`, `.csdd/plan/*/sessions.jsonl`,
`progress.json`) and the `smtg-ai/claude-squad` source as a comparison point.

The plan deliberately **defers** the piece it was originally conceived around
(worktree-isolated parallel instances). The evidence moved it from "build this" to
"decide this later, with data" — see §1.4 and §8.

---

## 1. Context and vision

### 1.1 The core finding: every failure was at the boundary, none was reasoning

The `violet` run is the first complete dataset for the autonomous loop. It works:

| Outcome | Count |
|---|---|
| `done` | 62 |
| `failed` | 20 |
| `progress` (handoff) | 8 |
| `blocked` | 2 |
| `waiting` (account limit) | 2 |

**27 of 31 feats delivered autonomously** — migrations, money, tenancy, HMAC
webhooks — each with an independent security review and code review that caught
real defects (prompt injection through un-collapsed newlines in a persona field;
`consume(-5)` manufacturing Credits; a `withdrawal.failed` path that never credited
the gross back). The remaining 4 feats are an untouched linear tail, not a failure.

All 20 failures carry one of exactly two signatures, and **neither is the model
being wrong**:

```
2026-07-24 (10×)  fork/exec claude.exe: O nome do arquivo ou a extensão é muito grande
2026-07-20 (10×)  claude session failed: exit status 1:        ← empty stderr
```

The first is the Windows 32,767-char command-line limit, already fixed by
`fd40f75` (brief moved to stdin — see the comment at `runner_exec.go:135`, which
describes this exact log). The second is undiagnosed.

The loop's *judgment* never failed. Its *edges* did: process spawn, in-memory
state, branch integration.

### 1.2 The bound counted crashes that were not attempts

This is a defect, not an environment problem.

```
2026-07-20  agency-balance-and-payout  failed ×7  →  blocked after 8 attempts
2026-07-24  billing-engine             failed ×8  →  blocked after 8 attempts
```

Three days later the session that finally spawned reported:

> *"The prior 2026-07-20 runs left the feat **code-complete and committed** but
> never pushed/PR'd (8 session crashes); this run closed it out."*

`FeatAttempts` (`runner.go:92`) exists to bound *a feat that cannot converge*. It
counted *a process that never started*. `executeFeat` calls `st.nextAttempt(key)`
at `runner.go:271` before `sessionOrWait`, so a spawn error consumes an attempt
identically to a genuine failed session. Two feats were surfaced as blocked while
their code was on disk, costing days of re-execution.

The runner already has the right precedent: `LimitError` (`runner_exec.go:188`) is
an infrastructure stop that is transparently absorbed and never counts. A spawn
failure is the same class of event.

### 1.3 In-flight sub-agent loss is routine, not exceptional

Three sessions ended with dispatched implementers unreported:

> *"IN FLIGHT WHEN I RAN OUT OF ROOM — three implementer sub-agents were dispatched
> and had NOT yet reported. Their results are UNKNOWN. Verify each worktree's state
> before redoing work."*

Recovery worked — but only because the handoff prose was exhaustive and the
successor spent real time re-verifying. `agency-panel-characters` needed 3 sessions
and 2 handoffs; both earlier ones ended with agents in flight.

Meanwhile `runState` (`runner.go:449`) — attempts, handoffs, failure history,
blocked set — is entirely in memory. A crash loses all of it. The data to rebuild
it is **already written** to `sessions.jsonl` (`ledger.go:110`) and never read back:
`LoadSessionRecords` (`ledger.go:173`) is used only for metrics reporting.

### 1.4 What the dependency graph says

`Feat.Depends` (`model.go:43`) is parsed, validated for cycles with Tarjan SCC
(`validate.go:185`), and emitted as `depends_on` edges by the graph extractor
(`graph/extract_plan.go:140`). The sequencer then discards it —
`seq.go:8` states plainly: *"There is no dependency graph — feats run in the order
the author wrote them."*

Topologically sorting the `violet` plan's own `Depends` column:

| | |
|---|---|
| Sequential sessions | **31** |
| DAG depth (critical path) | **11** |
| Widest wave | **6** |

A **2.8× reduction in wall-clock critical path** is available from data the plan
already carries. Measured session cost in that run: $32.42 and $33.85, 2h00 and
1h38 — so ~56h sequential collapses toward ~20h.

Two caveats that shape the build order:

1. `(P)` was set on only **3 of 31** feats — because today the column does nothing.
   The plan would need re-annotating before any of this is realized.
2. A session that actually attempted worktree isolation reported it does not hold:

   > *"this spec's `(P)` boundaries are **not truly disjoint**: `services/__init__.py`,
   > `schemas/__init__.py`, `models/__init__.py`, `routes/__init__.py` are shared
   > export surfaces that conflict on merge."*

   Its own remedy was to abandon worktrees and run concurrently in the main
   checkout with **the orchestrator as the single writer of export surfaces**.

That second point is why this plan stops before worktrees. See §8, D1.

### 1.5 Value thesis (why this build order)

Phase 1 fixes losses that **already happened**, costs ~100 lines, and has zero
architectural risk. Phase 2 is cheap and unlocks everything after it. Phase 3 is
where cost and risk concentrate, and the evidence raises a specific doubt about it
— so it sits behind a measurement gate rather than being assumed.

---

## 2. Scope

**In scope**

- Classifying spawn failures apart from session failures (R1).
- Rebuilding `runState` from the append-only session record (R2, R3).
- Diagnosing the undiagnosed `exit status 1` class (R4).
- A DAG-aware sequencer, with `(P)` given real meaning (R5).
- A third verdict status for a feat blocked on a peer (R6).
- `--squad-limit`, bounded 1..6, shipped with an effective value of 1 (R7).
- `SquadObserver`, wired to the existing Telegram notifier (R8).
- Journal noise reduction (R9).

**Out of scope (this revision)**

- Git worktree isolation per feat, and the integration-branch merge phase. Deferred
  behind the §7 M2 decision gate.
- Concurrency > 1. The flag ships; the capability does not.
- The squad TUI. Demoted to optional — see §8, D2.
- Any change to the verdict gate's three on-disk checks.

---

## 3. Design principles (inviolable)

1. **Re-derive from disk; do not checkpoint process state.** The precedent is
   `reconcileLedgerFromDisk` (`runner.go:422`). Anything git or the spec tree can
   answer is never recorded separately.
2. **Infrastructure stops are not work failures.** They do not consume attempts,
   burn the stall guard, or enter the failure log. `LimitError` is the pattern.
3. **The runner never imports a renderer.** Observation is an interface the `plan`
   package defines and others implement — the `Hooks` idiom (`runner.go:37`).
4. **Instrumentation must never end a run.** Existing contract at `ledger.go:150`.
   Extended here to the observer: a slow consumer drops events, exactly as
   `hub.broadcast()` does at `sse.go:91`.
5. **The approval hash is sacred.** Nothing the runner writes may alter `plan.md`
   or trip `CoreHash` drift (`model.go:117`). Discovered state goes to sidecars.
6. **`--squad-limit 1` is byte-identical to today.** Every phase must be mergeable
   with the feature invisible.

---

## 4. Requirements

### Requirement 1: Spawn failures are classified, not counted

**R1.1** When the `claude` subprocess fails before producing any stream event — a
`cmd.Start()` / `fork/exec` error — the runner SHALL classify it as a spawn failure
distinct from a session failure.

**R1.2** A spawn failure SHALL NOT increment `st.attempts`, SHALL NOT increment the
stall counter, and SHALL NOT enter the feat's `failureHistory`.

**R1.3** The runner SHALL retry the same feat after a bounded backoff, and SHALL
journal the event as `infra` with the underlying error verbatim.

**R1.4** After a configurable number of consecutive spawn failures across the whole
run (default 5), the runner SHALL abort with a distinct outcome code — a persistent
spawn failure is an operator problem, not a feat problem, and burning 100 iterations
on it helps nobody.

**R1.5** A session that starts and then fails is unaffected: it remains a session
failure with today's semantics.

### Requirement 2: Every attempt is recorded before it runs

**R2.1** The runner SHALL append a `SessionRecord` with `Status: "started"` before
spawning, carrying feat, iteration and attempt.

**R2.2** On resume, a `started` record with no matching settled record for the same
(feat, iteration, attempt) SHALL be treated as a crashed attempt: it counts against
`FeatAttempts`, and the feats it belongs to SHALL be named in the run journal — not
only on stdout, which a long unattended run has nobody reading.

**R2.3** Appends SHALL be serialized (mutex), since `O_APPEND` atomicity is not
guaranteed on Windows.

### Requirement 3: `runState` is rebuilt from the record

**R3.1** At run start, after `reconcileLedgerFromDisk`, the runner SHALL rebuild
`runState` from `LoadSessionRecords`: attempt counts, the last `continue` handoff
per feat, and the blocked set.

**R3.2** A feat marked done in the ledger SHALL have its trail cleared, matching
`clearFeat` semantics.

**R3.3** `st.logs` SHALL be repopulated by testing for the deterministic failure-log
path (`runner.go:608`), not by recording it.

**R3.4** A missing or unreadable `sessions.jsonl` SHALL degrade to today's behavior
(empty `runState`), never fail the run.

**R3.5** The run SHALL log what it recovered, in one line, so a resumed run is
visibly distinguishable from a fresh one.

### Requirement 4: The undiagnosed failure class is diagnosed

**R4.1** `execClaudeSession` SHALL capture and journal the child's exit code and the
tail of stderr even when stderr is empty, plus the argv length and whether any
stream event was received — enough to classify the `exit status 1` family on next
occurrence.

**R4.2** This is instrumentation only; no behavior change is required by R4.

### Requirement 5: The sequencer honors the dependency graph

**R5.1** `readyFeats` SHALL return every feat whose `Depends` entries are all in the
done set and which is neither running nor blocked.

**R5.2** ~~Range shorthand in `Depends` SHALL be resolved or rejected at validate
time.~~ **Already satisfied — no work.** `validate.go:71-77` rejects range
shorthand outright ("list feat slugs explicitly"), and the same loop rejects
self-dependencies and unknown slugs. `plan run` preflights on approval and
approval requires a clean validate, so by the time the scheduler sees a plan its
`Depends` cells are guaranteed to be resolvable feat slugs. Kept here as a pinned
assumption: if that validation is ever relaxed, `readyFeats` becomes unsound.

**R5.3** `(P)` SHALL gate concurrency permission, distinct from DAG readiness which
gates correctness. A ready feat without `(P)` SHALL run alone.

**R5.4** With `--squad-limit 1` the dispatch order SHALL remain a valid topological
order, so behavior is a strict refinement of table order, never a regression.

### Requirement 6: A feat may report itself blocked on a peer

**R6.1** The verdict schema SHALL accept a third status, `blocked`, with a
`blocked_on` array of feat slugs.

**R6.2** `blocked_on` SHALL name feats that exist in the plan and are not done;
otherwise the runner SHALL reject the verdict and treat it as `continue`.

**R6.3** A `blocked` verdict SHALL park the feat without consuming an attempt, and
re-dispatch it only once every named feat is done.

**R6.4** The discovered edge SHALL be written to
`.csdd/state/plan/<slug>/discovered-deps.json` — **never** to `plan.md` (principle 5).

**R6.5** The scheduler SHALL union plan-declared and discovered dependencies.

### Requirement 7: `--squad-limit` exists, bounded, and lands inert

**R7.1** `plan run` SHALL accept `--squad-limit N`, rejecting N < 1 and N > 6.

**R7.2** The **hard ceiling is 6**, anchored on the widest topological wave the
evidence plan admits (§1.4). A plan cannot use more than its own graph allows, so
a higher number buys nothing and only burns account quota faster.

**R7.3** The **target default is 3**. It does NOT take effect in M1: with
concurrency unimplemented, a default of 3 would either lie about what the run does
or force M3's work into M1. M1 therefore ships the flag with an effective value of
1, and M3 flips the default to 3 in the same change that makes concurrency real —
together with the shared account limiter (§8, R-B), which a default above 1 makes
mandatory rather than optional.

**R7.4** N > 1 in M1 SHALL be accepted but warn that concurrency is not yet
implemented, so the flag's contract is fixed before the capability lands.

**R7.5** The frontmatter key `squad_limit` SHALL be honored, with the CLI winning,
and SHALL be subject to the same 1..6 bound.

**Note on a default above 1.** It makes parallel execution the behavior a user gets
without asking for it, in a tool that spends money unattended for hours. Two things
make that acceptable: `(P)` is the real gate (R5.3), so a plan that has not
declared parallel-safe feats stays serial whatever the limit says; and the ceiling
is low enough that the worst case is bounded. The corollary is that the default
buys nothing until plans are re-annotated — see §8, R-C.

### Requirement 8: Observation is an interface

**R8.1** The `plan` package SHALL define `SquadObserver` with dispatch, event,
settle and scheduler-note methods.

**R8.2** `RunOptions.Observer` SHALL default to nil, preserving the existing
`liveReporter` / `logReporter` behavior exactly (`fleet_render.go:54`).

**R8.3** The Telegram notifier SHALL be driven by an observer implementation rather
than by ad-hoc calls, so one seam serves Telegram, a future TUI, and the web SSE hub.

**R8.4** A slow observer SHALL drop events, never block the runner.

### Requirement 9: The journal reports reconciliation once

**R9.1** `reconcileLedgerFromDisk` SHALL emit one aggregate journal line per run
(`reconciled N feats from disk`) instead of one per feat.

**Rationale:** 40 of 94 entries in the `violet` journal (43%) are per-feat
reconciliation lines from `runner.go:433`, fired repeatedly because the ledger lives
in the transient state dir and was lost roughly five times.

---

## 5. Design

### 5.1 What runs today

`Run` (`runner.go:129`) loads the plan, preflights approval + sandbox, then loops:
`nextFeat` picks the first feat the ledger does not mark done (`seq.go:22`),
`executeFeat` spawns one `claude -p` session (`runner_exec.go:133`), parses a
`done|continue` verdict, gates a `done` against three on-disk checks, and records
the outcome. All cross-iteration memory is `runState`, in RAM.

### 5.2 Goals / Non-Goals

**Goals** — a run survives a crash with its per-feat progress intact; an
environment fault never masquerades as a feat that cannot converge; the dependency
graph the plan already declares actually schedules; the observation seam exists
before there is anything to observe.

**Non-Goals** — concurrency, worktrees, merge automation, a TUI. Each is either
deferred behind a gate or demoted.

### 5.3 Reuse map (verified against the current tree)

| Need | Already exists | Gap |
|---|---|---|
| Infra-stop precedent | `LimitError` + `sessionOrWait` (`runner_exec.go:188`, `runner.go:344`) | generalize to spawn |
| Attempt/handoff record | `SessionRecord` (`ledger.go:110`), `LoadSessionRecords` (`ledger.go:173`) | never read back |
| Cycle detection | `featCycles`, Tarjan (`validate.go:185`) | not used by the sequencer |
| Dependency edges | `Feat.Depends` (`model.go:43`) | discarded by `seq.go:22` |
| Verdict parsing | `parseVerdict` / `normalizeVerdict` (`runner_exec.go:282`) | two statuses only |
| Renderer selection | `newReporter` (`fleet_render.go:54`) | writes bytes, not events |
| Drop-on-full policy | `hub.broadcast` (`sse.go:91`) | reuse verbatim |
| Notification channel | Telegram notifier (`cli/plan.go:64`) | wire through the observer |

### 5.4 Spawn classification (R1)

`execClaudeSession` already distinguishes the two moments: `supervised.run()` owns
the child, and `stream.verdictSource()` reports whether anything arrived. A
`SpawnError` is returned when `cmd.Start()` fails or when the process exits having
produced **zero** stream events and a non-empty exec-level error.

`sessionOrWait` grows a second `errors.As` branch beside `LimitError`: back off,
retry the same feat, do not return to `executeFeat`. The attempt counter lives in
`executeFeat` (`runner.go:271`), so never returning is exactly what makes the
attempt not count — no new bookkeeping.

The consecutive-spawn-failure abort (R1.4) is a counter on `runState`, reset by any
session that produces a stream event.

### 5.5 Resume (R2, R3)

`restoreRunState(root, slug, featAttempts)` folds the record oldest-first:
`attempts` increments on every row; `handoffs` takes the last `continue` detail;
`failed` rows seed `failureHistory`; `done` clears the feat. `blocked` is derived,
not stored — a feat is blocked when its attempts meet the bound and the ledger does
not mark it done.

Crash detection (R2.2) is a pairing pass: index settled rows by
(feat, iteration, attempt) and treat unpaired `started` rows as crashed.

Durability note: `sessions.jsonl` is transient state. If `csdd clean` removes it,
resume degrades to `reconcileLedgerFromDisk` — progress survives, handoffs do not.
`clean` should say so.

### 5.6 The DAG sequencer (R5)

`nextFeat` is replaced by `readyFeats(doc, done, running, blocked) []Feat`, ordered
by table position for determinism. With `--squad-limit 1` the caller takes the head,
which is a topological refinement of today's behavior.

`(P)` is read at dispatch: a non-`(P)` ready feat waits for the in-flight set to
drain. At limit 1 this is a no-op, which is what keeps Phase 2 inert.

### 5.7 The `blocked` verdict (R6)

`verdictSchema` (`runner_exec.go:66`) gains the status and the `blocked_on` array;
`model.go:144` gains the constant; `normalizeVerdict` (`runner_exec.go:311`) gains a
case. Validation of `blocked_on` is mechanical and on-disk, the same discipline as
the verdict gate — which is what stops `blocked` from becoming an escape hatch from
hard work.

`continue` is wrong for this case: it resets the stall guard **and** burns an
attempt against a bound the feat cannot possibly satisfy while its blocker runs.

### 5.8 The observer (R8)

```go
type SquadObserver interface {
    FeatDispatched(feat, worktree, branch string)
    FeatEvent(feat string, ev StreamEvent)
    FeatSettled(feat, status string, m SessionMetrics)
    SchedulerNote(kind, text string)
}
```

Defined in `plan`, implemented outside it — the dependency direction that keeps
`plan` free of `tui`, `web` and the Telegram client. Nil observer means today's
behavior. The Telegram notifier becomes the first implementation, which is what
makes Phase 2 deliver user-visible value with no renderer written.

### 5.9 File structure plan

| Path | Change |
|---|---|
| `internal/plan/runner_exec.go` | `SpawnError`, R4 instrumentation, verdict schema |
| `internal/plan/runner.go` | spawn branch in `sessionOrWait`, `blocked` handling, restore call, aggregate reconcile log |
| `internal/plan/ledger.go` | `started` records, append mutex |
| `internal/plan/resume.go` | **new** — `restoreRunState` |
| `internal/plan/seq.go` | `readyFeats` replaces `nextFeat` |
| `internal/plan/observer.go` | **new** — `SquadObserver` |
| `internal/plan/model.go` | `VerdictBlocked`, `blocked_on` |
| `internal/plan/validate.go` | range-shorthand resolution (R5.2) |
| `internal/cli/plan.go` | `--squad-limit`, observer wiring |

---

## 6. Tasks

### Phase 1 — Resilience (M0). No new concepts.

1. `SpawnError` + classification in `execClaudeSession`. _(R1.1, R1.5)_
2. Spawn branch in `sessionOrWait` with backoff + `infra` journal. _(R1.2, R1.3)_
3. Consecutive-spawn abort with its own outcome code. _(R1.4)_
4. `started` records + append mutex. _(R2)_
5. `restoreRunState` in a new `resume.go`, called after reconcile. _(R3)_
6. Crash-pairing pass and its journal line. _(R2.2, R3.5)_
7. R4 instrumentation on the child's exit path. _(R4)_
8. Aggregate reconciliation journal line. _(R9)_

Tests: synthetic `sessions.jsonl` fixtures driving `restoreRunState`; a `Session`
hook stub returning `SpawnError` asserting the attempt counter does not move.

### Phase 2 — Scheduling and observation (M1). Inert at limit 1.

9. `readyFeats` + table-order determinism. _(R5.1, R5.4)_
10. Range-shorthand resolution at validate. _(R5.2)_
11. `(P)` drain semantics. _(R5.3)_
12. `blocked` verdict: schema, constant, parse, validation, park/re-dispatch. _(R6.1–R6.3)_
13. `discovered-deps.json` sidecar + union in the scheduler. _(R6.4, R6.5)_
14. `--squad-limit` flag + frontmatter key. _(R7)_
15. `SquadObserver` interface + nil default. _(R8.1, R8.2)_
16. Telegram notifier reimplemented as an observer. _(R8.3, R8.4)_

### Phase 3 — Decision gate (M2). No code.

17. Run a real plan under Phases 1–2. Measure: spawn failures absorbed vs.
    attempts consumed; handoffs recovered on resume; whether `exit status 1`
    recurs and what R4 says about it.
18. Decide worktree-with-merge vs. single-checkout-with-declared-shared-surfaces
    (§8, D1). **Do not build either before this.**

### Phase 4 — Conditional (M3+). Only if M2 says so.

Concurrency > 1, the shared account limiter, ledger single-writer, the chosen
isolation strategy, and — optionally — the TUI.

---

## 7. Milestones

| ID | Milestone | Exit criteria |
|---|---|---|
| **M0** | Resilience | A killed run resumes with attempts, handoffs and blocked set intact. A spawn failure never consumes an attempt. `--squad-limit` absent. |
| **M1** | Scheduling + observation | Dispatch order is topological. `blocked` round-trips. Telegram runs through the observer. `--squad-limit` validates 1..6; its effective value is 1, behaviorally identical to M0. |
| **M2** | Decision gate | One real plan executed under M1, with the §6.17 measurements recorded in a revision of this document. |
| **M3** | Concurrency | Deferred; scoped only after M2. Flips `--squad-limit` to its default of 3, together with the shared account limiter (R-B) — never separately. |

M0 and M1 are independently mergeable and independently valuable. M0 alone repays
the two wrongly-blocked feats observed in `violet`.

---

## 8. Risks and decisions

**D1 — Isolation strategy is deferred, deliberately.** The `violet` evidence cuts
against worktrees: `(P)` boundaries were not disjoint, shared export surfaces
conflicted on merge, and the session that hit this abandoned worktrees for a single
checkout with the orchestrator as sole writer of `__init__` files. Two stale locked
worktrees were also left orphaned under `.claude/worktrees/`, polluting
`graph analyze` — the same orphan-cleanup problem `claude-squad` solves at
`session/instance.go:425`. Building the expensive option before measuring risks
paying for isolation that the merge phase gives back.

**D2 — The TUI is demoted to optional.** Measured sessions ran 2h00 and 1h38 across
a 10-day plan: this is unattended workload. A push notification through the
notifier that already exists (`cli/plan.go:64`) serves it better than a panel nobody
watches. The observer is essential; the renderer is not.

**R-A — `blocked` as an escape hatch.** A model that learns it can declare `blocked`
may reach for it instead of doing hard work. Mitigated mechanically by R6.2:
`blocked_on` must name a real, not-done feat, checked on disk. Monitor the `blocked`
rate in M2; if it climbs, tighten by requiring the blocker to be a *declared*
dependency.

**R-B — The account limit is a shared global.** Not a risk at limit 1 (only 2
`waiting` entries in 94), but the target default of 3 (R7.3) binds it three times
sooner, and at that rate the per-loop `waitForLimit` (`runner.go:361`) is actively
wrong: three agents would each sleep their own countdown, uncoordinated, and any
re-dispatch during the window burns more quota to rediscover that the quota is
gone. A default above 1 therefore makes the shared limiter a **hard prerequisite**
of M3 rather than a refinement of it — the two ship together or neither does.

**R-C — `plan.md` re-annotation.** Realizing the 2.8× requires `(P)` on far more
than 3 of 31 feats. That is authoring work on each plan, not runner work, and it
lands after M2 proves the scheduler.

**R-D — `sessions.jsonl` is transient.** Resume depends on a file `csdd clean`
removes. Accepted: the degradation is graceful (progress survives via
`reconcileLedgerFromDisk`, handoffs do not). `clean` must warn.
