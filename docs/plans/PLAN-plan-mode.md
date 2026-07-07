# Master Plan — csdd Plan Mode (`csdd plan` + `csdd sandbox`)

> **Audience:** the LLM agent (and humans) implementing this feature end-to-end.
> **Status:** draft for review — revision 1 (2026-07-06). Not yet approved.
> **Companions:** `docs/plans/PLAN-knowledge-base.md` (the archetype this formalizes —
> and the substrate: graph, wiki and stack are *consumed* by this feature);
> github.com/snarktank/ralph (PRD skill + autonomous loop mechanics, studied 2026-07-06);
> github.com/protonspy/devcontainer-claude-code (sandbox template base, rebased onto
> language-agnostic Ubuntu in this design); code.claude.com/docs/en/devcontainer
> (official sandbox reference and security model).
> **Dogfood note:** this document is written in the format §4/§5/§6 define — the feature
> planning itself. Once `csdd plan init` exists (task 4), this file migrates to
> `docs/plans/plan-mode/plan.md` and becomes the first plan ever validated, approved,
> and (M4) executed by `csdd plan run`.

---

## 1. Context and vision

### 1.1 What we are building

csdd governs the spec flow: `requirements → design → tasks`, gated by human approvals
and mechanical validation. This plan adds the layer **above** specs: the **plan** — a
structured, contract-bound document in `docs/plans/` that decomposes an initiative into
**feats**, where each feat becomes exactly one spec in `specs/`. On top of it,
**autonomous execution**: `csdd plan run` drives fresh `claude -p` sessions through the
spec flow and implementation, csdd controlling and validating every step, with the
human gate moved upstream to a single `csdd plan approve`.

The hierarchy:

```text
Plan (docs/plans/<slug>/)  →  Feats (rows of the plan)  →  Specs (specs/<feat>/)  →  Tasks
```

**The contract is the secret.** Autonomy is not trust in the model — it is an authority
chain of contracts, every link mechanically checkable: the **plan** (approved,
hash-bound) says *what* and *in which order*; **`docs/stack.md`** says *with which
technology*; the **spec validators** say *whether each artifact honors the format*;
the **quality gates** say *whether the code works*; `csdd graph analyze --strict` says
*whether traceability holds*. Anything not covered by a contract is an **open
decision** — the loop surfaces it and stops that feat; it never improvises.

### 1.2 The Ralph synthesis (what we take, what we already have better)

Ralph (snarktank/ralph) proved the loop shape: a PRD of right-sized stories, a fresh
AI session per story, deterministic sequencing outside the model, mechanical gates
before commit, learnings persisted between iterations. csdd adopts the shape and
upgrades every organ:

| Ralph | csdd plan mode |
|---|---|
| `prd.json` + `passes: false` flags | plan + **derived status** (computed from `specs/` — no stored flags to desync) |
| story picked by priority | `csdd plan next` (deps + milestones + `(P)`, returns the exact *step*) |
| `prompt.md` hand-rolled context | `csdd plan brief` — deterministic context pack (feat row, seeds, stack rows, wiki refs, executor notes) |
| `progress.txt` append-only learnings | the wiki (structured, interlinked, graph-indexed) |
| `AGENTS.md` conventions | steering + managed CLAUDE.md (already installed by csdd) |
| typecheck + tests | spec validators + plan quality gates + `graph analyze --strict` + `wiki lint` |
| `ralph.sh` bash orchestrator | `csdd plan run` — the runner in Go, inside the binary |
| trust the loop on the host | `csdd sandbox` — devcontainer with default-deny egress; bypass only inside |

### 1.3 Value thesis (why this build order)

- **The plan document pays before any automation exists.** `PLAN-knowledge-base.md`
  already proved the format's worth for a human-driven build. Formalizing it +
  `validate`/`status` (M1) makes every future initiative start structured and
  mechanically linted — and dogfoods the parser on this very file.
- **The sandbox is independent and unblocks safe experimentation** (M2). It is useful
  today for any Claude Code work, not just the loop.
- **The bridge (approve/next/brief/generate, M3) is already a usable workflow** —
  a human or a supervised session can follow `next` manually long before the runner
  exists.
- **The runner (M4) is the smallest possible machine** because everything hard —
  sequencing, context assembly, validation, status — already shipped as CLI commands
  it merely calls in a loop.

---

## 2. Storage (extends the project-wide convention, revision 3)

| Path | Role | Author |
|---|---|---|
| `docs/plans/<slug>/plan.md` | The plan document — vision, decisions, **feat table**, quality gates, executor notes. | Human/LLM prose; scaffolded and linted by the CLI, never written by it. |
| `docs/plans/<slug>/seeds/<feat>/*.md` | Optional pre-authored spec artifacts (requirements/design/tasks seeds) consumed by `plan generate`. | Human/LLM. |
| `docs/plans/<slug>/plan.json` | Approval state: `{schema_version, name, approvals: {approved, content_hash, approved_at}}`. | **CLI only** (`plan approve`) — the `spec.json` precedent. |
| `docs/plans/<slug>/log.md` | Append-only run journal (one mechanical entry per runner event — the `docs/graph/log.md` precedent). | **CLI only** (`plan run`). |
| `.csdd/plan/<slug>/` | Transient runner state (session ids, last verdicts, block markers). Regenerable, gitignored. | CLI only. |

Feat **status is never stored** — it is derived from `specs/<feat>/` reality on every
read (R5). The committed knowledge is the plan and its journal; the machine state is
disposable.

---

## 3. Design principles (inviolable)

1. **The CLI never authors prose.** It scaffolds, parses, lints, derives, sequences,
   and appends mechanical log/state entries (`plan.json`, `log.md`) — plan content is
   human/LLM-authored via the installed skill.
2. **No LLM inference in the binary.** Refined for this feature: `csdd plan run` may
   **orchestrate the local `claude` CLI as a subprocess** — it never calls a model API
   itself, and every other command works with no `claude` installed.
3. **The LLM authors; the runner approves.** Sessions produce artifacts and verdicts.
   `csdd spec approve`, commits, and loop advancement are executed by the runner, and
   only after the mechanical gates pass. A session that tries to self-approve or to
   write `plan.md`, `plan.json`, `docs/graph/` or `.csdd/` is a hard failure (R9.6).
4. **Derived status, single source of truth.** `specs/` is reality; the plan never
   duplicates it.
5. **Deviation is an event, not an improvisation.** Unforeseen problems produce a
   structured blocked-verdict logged to `log.md`; folding it into the plan is a human
   (re-approval) act. Hash drift always pauses autonomy.
6. **Human gates: exactly two.** `csdd plan approve` before, the pull request after.
   Everything between is contract-checked machine work.
7. **Sandbox before bypass.** `--dangerously-skip-permissions` is only ever passed when
   `csdd sandbox doctor` proves the session is inside the hardened container.
8. **Aggressive reuse.** Spec lifecycle (`internal/cli/spec.go`), validator
   (`internal/validator`), graph (`internal/graph` Extractor seam), templater, paths,
   web hardening (PR #43) — nothing here invents a second mechanism for a solved
   problem.

---

## 4. Requirements (content of `requirements.md`)

EARS format. Requirement IDs in annotations are explicit comma-separated lists.

### Requirement 1: Plan scaffolding (`csdd plan init`)
**Objective:** As a project owner, I want one command to stand up a structured plan,
so every initiative starts in the standard format.
**Acceptance Criteria**
1. WHEN `csdd plan init <slug>` runs THEN the system SHALL create
   `docs/plans/<slug>/{plan.md, seeds/}` from the plan template, with all required
   sections present and no content prefilled beyond structure and inline format docs.
2. WHERE the plan directory already exists THE SYSTEM SHALL add only missing files
   (idempotent; `--force` required to overwrite).
3. IF the workspace marker (`.csdd/`) is absent THEN the system SHALL fail with the
   actionable `csdd init` guidance (R15 of the knowledge-base plan).

### Requirement 2: The plan document contract (parseable core)
**Objective:** As the toolchain, I want the plan machine-readable where it matters,
so sequencing, validation and the graph work from explicit structure.
**Acceptance Criteria**
1. THE SYSTEM SHALL parse a `## Feats` table with columns
   `| # | Feat | Objective | Depends | Milestone | (P) | Refs |` where `Feat` is a
   slug valid as a spec directory name, `Depends` is an explicit comma-separated list
   of feat slugs (range shorthand is a parse error), and `Refs` holds zero or more
   `[[wiki-page]]` and `stack:<name>` tokens.
2. THE SYSTEM SHALL parse a `## Quality Gates` section of `- <label>: <command>` lines
   (e.g. `- verify: make check`); the gate list is the plan's own verification
   contract, executed by the runner after every implementation step.
3. THE SYSTEM SHALL treat `## Executor Notes` as an opaque block passed verbatim into
   every brief (R7), and frontmatter keys `name`, `status (draft|approved|superseded)`.
4. WHERE `seeds/<feat>/` contains `requirements.md`/`design.md`/`tasks.md` THE SYSTEM
   SHALL associate them with that feat for `plan generate` (R8).

### Requirement 3: Plan lint (`csdd plan validate`)
**Objective:** As a reviewer (human or runner), I want structural violations surfaced
mechanically, so the approve gate acts on a sound document.
**Acceptance Criteria**
1. WHEN `csdd plan validate <slug>` runs THEN the system SHALL report: feat-table
   grammar violations, duplicate/invalid slugs, `Depends` references to nonexistent
   feats, and dependency cycles.
2. THE SYSTEM SHALL report every `stack:<name>` ref with no matching row in the
   `docs/stack.md` Decided table ("undeclared tech in plan") and every `[[wiki-page]]`
   ref with no matching page ("broken plan ref").
3. THE SYSTEM SHALL report an empty or missing `## Quality Gates` section.
4. THE SYSTEM SHALL run the EARS lint over any `seeds/*/requirements.md`.
5. IF any violation is found THEN the system SHALL exit non-zero (CI-gateable).

### Requirement 4: The human gate (`csdd plan approve`)
**Objective:** As the project owner, I want my approval bound to exact content, so
autonomy is authorized against what I read, not what the file later became.
**Acceptance Criteria**
1. WHEN `csdd plan approve <slug>` runs and `validate` passes THEN the system SHALL
   write `plan.json` with `approved: true` and a content hash covering `plan.md` and
   `seeds/**`.
2. WHEN plan content changes after approval THEN `status`, `next` and `run` SHALL
   detect the hash mismatch ("drift") and `run` SHALL refuse autonomous execution
   until re-approval.
3. IF `validate` reports findings THEN `approve` SHALL fail listing them.

### Requirement 5: Derived status (`csdd plan status`)
**Objective:** As anyone observing the plan, I want per-feat progress computed from
reality, so there is no second bookkeeping to desync.
**Acceptance Criteria**
1. WHEN `csdd plan status <slug>` runs THEN the system SHALL derive each feat's state
   from `specs/<feat>/`: `pending` (no spec), `requirements|design|tasks` phase
   progress (from `spec.json` approvals), `ready` (ready_for_implementation),
   `implementing` (tasks partially checked), `done` (all tasks checked), plus
   `blocked` when `.csdd/plan/<slug>/` holds a block marker for the feat.
2. THE SYSTEM SHALL surface plan-level flags: approval state, drift, and unprocessed
   deviation entries in `log.md`.
3. THE SYSTEM SHALL support `--json` for consumption by the runner, the web tab, and
   scripts.

### Requirement 6: The sequencer (`csdd plan next`)
**Objective:** As the runner (or a human following the plan by hand), I want csdd to
name the exact next step, so the loop stays dumb and the intelligence stays in the CLI.
**Acceptance Criteria**
1. WHEN `csdd plan next <slug>` runs THEN the system SHALL select the first feat, in
   table order within milestone order, whose `Depends` are all `done` and which is not
   `done`/`blocked`, and return the next **step** within it: `spec-requirements`,
   `spec-design`, `spec-tasks`, or `task <N.M>` (honoring tasks.md order, `_Depends:`
   and `(P)` semantics).
2. THE SYSTEM SHALL emit the step as `--json` `{feat, step, reason}` and as text.
3. THE SYSTEM SHALL exit with distinct codes: step available (0), plan complete (3),
   nothing unblocked (4), drift/not-approved (5).

### Requirement 7: The context pack (`csdd plan brief`)
**Objective:** As a fresh `claude -p` session, I want a surgical, deterministic
context pack, so each iteration starts precise without inheriting noise.
**Acceptance Criteria**
1. WHEN `csdd plan brief <slug> --step` runs THEN the system SHALL assemble, from
   explicit content only: the step contract (artifact to produce, gates that will run,
   forbidden actions per R9.6), the feat row and objective, the feat's seeds, every
   `stack:` ref resolved to its full Decided row, every `[[wiki-page]]` ref resolved
   to path + frontmatter description, the Executor Notes verbatim, and the graph-first
   consultation instructions (`csdd graph query` before reading code).
2. THE SYSTEM SHALL NOT inline wiki page bodies (the session reads what it needs —
   token discipline); stack rows are inlined in full (they are one-line contracts).
3. THE SYSTEM SHALL be deterministic: same plan + same step → byte-identical brief.

### Requirement 8: Plan → spec bridge (`csdd plan generate`)
**Objective:** As the loop (or a human), I want a feat's spec born from the plan,
so specs inherit the plan's intent and provenance.
**Acceptance Criteria**
1. WHEN `csdd plan generate <slug> <feat>` runs THEN the system SHALL scaffold
   `specs/<feat>/` through the existing spec-init machinery, pre-seeding artifacts
   from `seeds/<feat>/` when present.
2. THE SYSTEM SHALL record provenance in `spec.json` (`plan: <slug>`), which the graph
   extractor turns into a `plans` edge.
3. IF the plan is not approved THEN the system SHALL warn (human use) and, WHEN invoked
   by `plan run`, SHALL fail.

### Requirement 9: The runner (`csdd plan run`) — a layer above the spec flow
**Objective:** As the project owner, I want csdd itself to drive the loop — spawning
fresh sessions, validating everything between steps, approving only on green — so
autonomous development is csdd-controlled end to end.
**Acceptance Criteria**
1. WHEN `csdd plan run <slug>` starts THEN the system SHALL verify: plan approved and
   drift-free, `claude` CLI available, and the execution mode — `--autonomous`
   requires `csdd sandbox doctor` to pass and adds
   `--dangerously-skip-permissions`; otherwise the runner operates supervised with
   `--permission-mode acceptEdits`.
2. THE SYSTEM SHALL iterate: `next` → `brief` → spawn
   `claude -p <brief> --output-format json --json-schema <verdict schema>` (with
   `--max-budget-usd` from `--session-budget` only when set; the default is no
   per-session cap — sessions run under the Claude account's own limits) → parse the verdict →
   run the gates for the step (spec phase: `csdd spec validate`; implementation task:
   the plan's Quality Gates plus `csdd graph analyze --strict`; wiki lint when wiki
   files changed) → on green, the RUNNER SHALL execute `csdd spec approve` (spec
   phases) or `git commit` scoped to the session's changed paths (implementation),
   then append a `log.md` entry.
3. IF a gate fails THEN the system SHALL retry the step with the failure output
   appended to the brief, up to `--max-retries` (default 2), then mark the feat
   blocked and continue with other unblocked feats.
4. IF the session verdict is `blocked` THEN the system SHALL append the structured
   deviation (reason + revision proposal) to `log.md`, mark the feat blocked, and
   continue; WHEN no unblocked feat remains THE SYSTEM SHALL exit with the
   nothing-unblocked code.
5. THE SYSTEM SHALL re-check plan drift every iteration and SHALL stop autonomous
   execution immediately on mismatch.
6. THE SYSTEM SHALL treat as a hard step failure any session that modified `plan.md`,
   `plan.json`, `docs/graph/`, or `.csdd/` (verified mechanically from the git status
   of the workspace), and SHALL never let a session execute approvals.
7. THE SYSTEM SHALL honor `--max-iterations` (default 25) and report totals
   (steps, retries, blocks, spend when available) on exit.

### Requirement 10: Sandbox scaffolding (`csdd sandbox init`)
**Objective:** As a user, I want one command to install the hardened devcontainer at
the project root, so bypass-mode Claude runs isolated and language-agnostic.
**Acceptance Criteria**
1. WHEN `csdd sandbox init` runs THEN the system SHALL scaffold `.devcontainer/` with
   the four-file template: `devcontainer.json`, `Dockerfile`
   (`mcr.microsoft.com/devcontainers/base:ubuntu-24.04`, non-root `vscode` user, no
   language runtime baked in), `init-firewall.sh` (default-deny egress with
   self-verification), and `allowed-domains.txt` (Anthropic/GitHub/npm/VS Code plus
   Ubuntu apt domains; language-registry presets commented).
2. THE SYSTEM SHALL support `--feature <name>` (adds the devcontainer feature block and
   uncomments its registry domains), `--allow-domain <d>` (appends), and `--hardened`
   (sets the build arg restricting sudo to the firewall script).
3. THE SYSTEM SHALL write all four files LF-normalized (the firewall script must
   survive CRLF working trees) and be idempotent (`--force` to overwrite).

### Requirement 11: Sandbox verification (`csdd sandbox doctor`)
**Objective:** As the runner, I want a mechanical proof of isolation, so bypass is
gated on evidence, not configuration hope.
**Acceptance Criteria**
1. WHEN `csdd sandbox doctor` runs THEN the system SHALL verify: `DEVCONTAINER=true`,
   a non-root user, and the firewall active (a control domain unreachable AND
   `api.anthropic.com` reachable), exiting 0 only when all hold.
2. THE SYSTEM SHALL report each check individually (`--json` included) so failures are
   actionable.

### Requirement 12: Plans in the graph
**Objective:** As a knowledge-base user, I want plans and feats in the same graph, so
traceability spans plan → spec → requirement → task → code.
**Acceptance Criteria**
1. WHEN `csdd graph build` runs THEN the system SHALL emit a `plan` node per plan and
   a `feat` node per feat row, with edges: plan `owns` feat, feat `plans` spec (or a
   pending reference when the spec does not exist yet), feat `depends_on` feat, feat
   `references` wiki pages, feat `uses_tech` stack entries.
2. WHEN `csdd graph analyze` runs THEN the system SHALL report: spec with no inbound
   `plans` edge ("unplanned spec" — informational, incremental adoption), feat
   dependency cycles, and broken plan refs, alongside the existing lints.

### Requirement 13: Plans in the web dashboard
**Objective:** As an observer, I want plans visible in `csdd web`, so progress and
health are readable without the terminal.
**Acceptance Criteria**
1. WHERE the dashboard is active THE SYSTEM SHALL serve a "Plans" tab listing plans
   with approval/drift badges, and a per-plan view rendering the feat table with
   derived status, milestone progress, and blocked flags — all read-only through the
   existing hardened route path (host guard, redaction; PR #43).
2. THE SYSTEM SHALL render the run journal (`log.md`) in the per-plan view.

### Requirement 14: Agent integration — the PRD skill, `/prd`, CLAUDE.md moments
**Objective:** As an agent in a csdd workspace, I want a PRD skill that generates and
executes the whole plan flow through the CLI, so plan mode is the default way big work
starts — one command from idea to validated plan.
**Acceptance Criteria**
1. THE SYSTEM SHALL install a **PRD skill** (`.claude/skills/prd/SKILL.md`) that owns
   the authoring pipeline and drives every mechanical step through the CLI
   (`csdd plan …`, or `npx csdd plan …` when not installed globally): **Draft**
   (clarifying-questions interview with lettered options → goals/non-goals/user
   stories), **Research** (docs via Context7 MCP when configured, else web → findings
   filed as wiki pages, cited in Refs), **Stack consult** (invoke the stack skill's
   Propose for any new tech — human decides), **Decompose** (right-sizing rule: one
   feat = one spec, implementable in a short session chain; explicit deps;
   milestones; seeds per feat), and **Revise** (fold `log.md` deviations into the
   plan for re-approval). The skill scaffolds with `csdd plan init`, lints with
   `csdd plan validate` after every authoring pass, and ends by presenting the
   validated plan for the human `csdd plan approve`.
2. THE SYSTEM SHALL install the **`/prd` slash command** as the primary entry point
   (routes to the PRD skill), plus thin wrappers `/csdd-plan-validate`,
   `/csdd-plan-status`, `/csdd-plan-run`.
3. WHEN `csdd init` runs THEN the managed CLAUDE.md section SHALL gain the moments:
   run `/prd` before multi-feature work; never edit an approved plan or execute
   approvals during a run; deviations go through the Revise workflow.
4. THE SKILL SHALL position the existing `quick-prd` skill as the lightweight
   single-feature on-ramp: `/prd` is for initiatives that decompose into multiple
   feats; `quick-prd` output (`docs/product/`) is an accepted *input* to Draft.

---

## 5. Design (content of `design.md`)

### 5.1 Overview
New package `internal/plan` (parse, validate, derive, sequence, brief, run) +
`internal/cli/plan.go` and `internal/cli/sandbox.go` + template subtrees `plan/` and
`sandbox/` + one graph extractor + one web tab. Zero changes to spec artifact formats;
one additive field in `spec.json` (`plan`).

### 5.2 Goals / Non-Goals
**Goals:** formalized plan contract; mechanical plan lint; derived status; step
sequencer; deterministic briefs; plan-seeded specs; the Go runner over `claude -p`;
hardened sandbox scaffold + doctor; graph/web/skill integration.
**Non-Goals (v1):** parallel feat execution in worktrees (`(P)` feats run sequentially
in v1; the annotation is honored for ordering only); multi-plan orchestration;
runner support for non-Claude agents; editing/merging plan revisions mechanically
(prose is human/LLM work); csdd-published devcontainer *feature* (post-v1 idea);
**container lifecycle management** — `csdd sandbox` only scaffolds the setup; starting,
rebuilding and stopping the environment is the developer's act (IDE "Reopen in
Container" / devcontainer tooling), and the runner never launches containers — it runs
*inside* whatever environment the developer started, proving isolation via `doctor`.

### 5.3 Reuse map (verified against the current tree)
| Need | Exists in | Use |
|---|---|---|
| CLI dispatch pattern | `internal/cli/cli.go` switch + `parseFlags` | `plan`/`sandbox` cases |
| Spec lifecycle + approvals | `internal/cli/spec.go` (`SpecJSON`, phase gates, content-hash drift) | runner drives it; `plan.json` mirrors the pattern |
| Artifact validation | `internal/validator` | gates; EARS lint reused for seeds |
| Frontmatter | `internal/frontmatter` | plan.md frontmatter |
| Paths | `internal/paths` (`DocsPlans` already exists) | plan/sandbox layout |
| Graph extractor seam | `internal/graph` `Extractor` | `extract_plan.go` |
| Templates | `internal/templater` (embedded, LF-normalized) | `templates/plan/`, `templates/sandbox/` |
| Web hardening | `internal/web` read-only routes (PR #43) | Plans tab |
| Managed CLAUDE.md | `internal/cli/claudemd.go` | plan-mode moments |

### 5.4 The plan grammar (parser spec, deterministic)
- Frontmatter: `name`, `status`. Prose sections are free except the three parseable
  ones: `## Feats` (table per R2.1 — one feat per row, cells trimmed, `Refs` tokenized
  by whitespace), `## Quality Gates` (`- label: command` lines), `## Executor Notes`
  (opaque). Unknown sections are preserved and ignored.
- `Depends` cells resolve against feat slugs only (never spec dirs) at parse time;
  unresolved → validate finding, never dropped.
- Hashing (R4): SHA-256 over `plan.md` bytes + sorted `seeds/**` (path, bytes) pairs —
  same discipline as `phaseContentHash`.

### 5.5 Derivation (status) and sequencing (next)
`status.go` reads `spec.json` + `tasks.md` checkboxes through the same parsing the
graph's `extract_spec.go` uses (shared helpers, not duplicated regexes). `seq.go` is a
pure function of (parsed plan, derived statuses, block markers) → `Step`; total
deterministic ordering (milestone → table order → phase order → tasks.md order) so the
same workspace always yields the same next step.

### 5.6 The runner
```go
type Verdict struct {
    Status   string // done | blocked
    Summary  string
    Revision string // populated when Status == blocked
}
```
`runner.go`: loop { drift check → `next` → `brief` → `exec.Command("claude", "-p",
brief, "--output-format", "json", "--json-schema", verdictSchema, budgetFlags...,
modeFlags...)` → parse → gates → advance/retry/block → journal }. Gates and approvals
run in the runner's process (never delegated to the session). Changed-path audit
(R9.6) via `git status --porcelain` diffed against the step's allowed roots. Journal
entries are single mechanical lines: `## [YYYY-MM-DD] <step> | <feat> | <outcome>`.
Mode selection: `--autonomous` → `sandbox doctor` must pass → bypass flag; default
supervised → `--permission-mode acceptEdits`. All `claude` flags used are pinned in
one table in the code with a doctor-style preflight (`claude --version` logged).

### 5.7 Sandbox
Templates are the four files delivered with this design (Ubuntu 24.04 devcontainers
base; Claude Code + gh as devcontainer features; toolchains via features/apt;
`allowed-domains.txt` externalized; `HARDENED` build arg for the sudo policy).
`doctor.go` implements R11 with no network beyond the two probe requests.
**Scope boundary:** `sandbox init` produces the setup and stops there. The workflow is:
developer runs `csdd sandbox init` once → opens the environment through their IDE
(Reopen in Container) or devcontainer tooling → inside it, runs
`npx csdd plan run <slug> --autonomous`. csdd never invokes docker/devcontainer
commands; `doctor` is its only relationship with the running environment.

### 5.8 CLI surface
```text
csdd plan     init <slug> | validate <slug> | approve <slug> | status <slug> [--json]
              next <slug> [--json] | brief <slug> --step [--json]
              generate <slug> <feat> | run <slug> [--autonomous] [--session-budget N]
                                       [--max-iterations N] [--max-retries N]
csdd sandbox  init [--feature F]... [--allow-domain D]... [--hardened] [--force]
              doctor [--json]
```

### 5.9 File structure plan
```text
internal/plan/
├── model.go        # PlanDoc, Feat, Step, Verdict, plan.json          [Model]
├── parse.go        # plan.md grammar (feats/gates/notes/frontmatter)  [Parse]
├── validate.go     # R3 findings                                     [Validate]
├── status.go       # R5 derivation                                   [Status]
├── seq.go          # R6 next                                         [Sequencer]
├── brief.go        # R7 assembly                                     [Brief]
├── runner.go       # R9 loop (exec claude, gates, journal)           [Runner]
├── sandbox.go      # R10 scaffold ops + R11 doctor                   [Sandbox]
└── *_test.go       # table-driven; fixtures in internal/plan/testdata/
internal/cli/
├── plan.go         # `csdd plan …`                                   [CLI]
└── sandbox.go      # `csdd sandbox …`                                [CLI]
internal/graph/extract_plan.go                                        [Extract]
internal/templater/templates/plan/     # plan.md.tmpl + skill + commands + claudemd
internal/templater/templates/sandbox/  # devcontainer.json, Dockerfile,
                                       # init-firewall.sh, allowed-domains.txt
internal/web/frontend/src/Plans.tsx (+ route/tab)                     [Web]
```
**Modified:** `internal/cli/cli.go` (register), `internal/cli/spec.go` (`plan` field),
`internal/graph` (vocab: `plan`/`feat` nodes, `plans` edge), `internal/web` (tab),
`internal/templater` (registration), `claudemd` section.

### 5.10 Requirements Traceability
| Requirement | Components | Interfaces | Flows |
|---|---|---|---|
| 1.1, 1.2, 1.3 | CLI, Templates | PlanInit() | init |
| 2.1, 2.2, 2.3, 2.4 | Parse, Model | ParsePlan() | all |
| 3.1, 3.2, 3.3, 3.4, 3.5 | Validate | ValidatePlan() | validate |
| 4.1, 4.2, 4.3 | CLI, Model | PlanApprove(), Hash() | approve |
| 5.1, 5.2, 5.3 | Status | DeriveStatus() | status |
| 6.1, 6.2, 6.3 | Sequencer | Next() | next |
| 7.1, 7.2, 7.3 | Brief | Brief() | brief |
| 8.1, 8.2, 8.3 | CLI, Templates | PlanGenerate() | generate |
| 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7 | Runner | Run() | run |
| 10.1, 10.2, 10.3 | Sandbox, Templates | SandboxInit() | sandbox-init |
| 11.1, 11.2 | Sandbox | Doctor() | doctor |
| 12.1, 12.2 | Extract | Extract(), AnalyzeGaps() | build/analyze |
| 13.1, 13.2 | Web | Plans.tsx | web |
| 14.1, 14.2, 14.3, 14.4 | Templates, CLI | prd skill / `/prd` / claudemd | init |

---

## 6. Tasks (content of `tasks.md`) — TDD, boundaries, dependencies

### Phase 1: The plan contract (M1)
- [ ] 1. Model + parser _Boundary: Parse_
  - [ ] 1.1 RED — fixtures: feat table (good/bad grammar, dep lists, Refs tokens),
    quality gates, executor notes, frontmatter, hash determinism
    - _Requirements: 2.1, 2.2, 2.3, 2.4_
  - [ ] 1.2 GREEN — `model.go` + `parse.go`
    - _Requirements: 2.1, 2.2, 2.3, 2.4_
- [ ] 2. Validate _Boundary: Validate_
  - [ ] 2.1 RED — each R3 finding class incl. stack/wiki ref resolution and dep cycles
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_
    - _Depends: 1.2_
  - [ ] 2.2 GREEN — `validate.go`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_
- [ ] 3. Derived status (P) _Boundary: Status_
  - [ ] 3.1 RED — fixture workspace: feats in every state; block markers; drift flag
    - _Requirements: 5.1, 5.2, 5.3_
    - _Depends: 1.2_
  - [ ] 3.2 GREEN — `status.go` (shared spec-parsing helpers with `internal/graph`)
    - _Requirements: 5.1, 5.2, 5.3_
- [ ] 4. CLI init/validate/status + plan template (P) _Boundary: CLI_
  - [ ] 4.1 RED — command surface, flags, `--json`, exit codes, idempotent scaffold
    - _Requirements: 1.1, 1.2, 1.3_
    - _Depends: 2.2, 3.2_
  - [ ] 4.2 GREEN — `internal/cli/plan.go` (partial) + `templates/plan/plan.md.tmpl`
    - _Requirements: 1.1, 1.2, 1.3_
- [ ] 5. Graph extractor + analyze lints (P) _Boundary: Extract_
  - [ ] 5.1 RED — plan/feat nodes, plans/depends_on/references/uses_tech edges,
    pending refs, unplanned-spec + cycle findings
    - _Requirements: 12.1, 12.2_
    - _Depends: 1.2_
  - [ ] 5.2 GREEN — `extract_plan.go` + vocab + analyze rules
    - _Requirements: 12.1, 12.2_

### Phase 2: Sandbox (M2 — independent; may ship first)
- [ ] 6. Sandbox templates + init _Boundary: Sandbox_
  - [ ] 6.1 RED — four-file scaffold, LF enforcement, `--feature`/`--allow-domain`
    edits, `--hardened` arg, idempotence
    - _Requirements: 10.1, 10.2, 10.3_
  - [ ] 6.2 GREEN — `templates/sandbox/` + `sandbox.go` init + `cli/sandbox.go`
    - _Requirements: 10.1, 10.2, 10.3_
- [ ] 7. Doctor _Boundary: Sandbox_
  - [ ] 7.1 RED — each check individually reportable; probe stubs; exit codes
    - _Requirements: 11.1, 11.2_
    - _Depends: 6.2_
  - [ ] 7.2 GREEN — `Doctor()`
    - _Requirements: 11.1, 11.2_

### Phase 3: The bridge (M3)
- [ ] 8. Approve + drift _Boundary: Model_
  - [ ] 8.1 RED — hash over plan.md+seeds, refuse on findings, drift detection paths
    - _Requirements: 4.1, 4.2, 4.3_
    - _Depends: 2.2_
  - [ ] 8.2 GREEN — `PlanApprove()` + plan.json IO
    - _Requirements: 4.1, 4.2, 4.3_
- [ ] 9. Sequencer (P) _Boundary: Sequencer_
  - [ ] 9.1 RED — dep/milestone/order matrix, step progression incl. task-level,
    exit codes 0/3/4/5, determinism (double-run equality)
    - _Requirements: 6.1, 6.2, 6.3_
    - _Depends: 3.2, 8.2_
  - [ ] 9.2 GREEN — `seq.go`
    - _Requirements: 6.1, 6.2, 6.3_
- [ ] 10. Brief (P) _Boundary: Brief_
  - [ ] 10.1 RED — byte-determinism; stack rows inlined; wiki refs as path+description;
    forbidden-actions text present; seeds included
    - _Requirements: 7.1, 7.2, 7.3_
    - _Depends: 9.2_
  - [ ] 10.2 GREEN — `brief.go`
    - _Requirements: 7.1, 7.2, 7.3_
- [ ] 11. Generate (P) _Boundary: CLI_
  - [ ] 11.1 RED — spec scaffold with seeds, `plan` provenance in spec.json,
    approved-plan enforcement
    - _Requirements: 8.1, 8.2, 8.3_
    - _Depends: 8.2_
  - [ ] 11.2 GREEN — `PlanGenerate()` + spec.json field
    - _Requirements: 8.1, 8.2, 8.3_
- [ ] 12. PRD skill + `/prd` + CLAUDE.md moments (P) _Boundary: Templates_
  - [ ] 12.1 RED — skill workflows present (Draft/Research/Stack/Decompose/Revise),
    CLI-driven via `csdd plan`/`npx csdd plan` (no direct approve/graph writes),
    `/prd` + wrapper commands, claudemd section, quick-prd positioning
    - _Requirements: 14.1, 14.2, 14.3, 14.4_
  - [ ] 12.2 GREEN — `templates/plan/` prd skill + commands + claudemd wiring
    - _Requirements: 14.1, 14.2, 14.3, 14.4_

### Phase 4: The runner (M4)
- [ ] 13. Runner core _Boundary: Runner_
  - [ ] 13.1 RED — with a stub `claude` binary in testdata: verdict parsing, gate
    pipeline, retry-with-failure-appended, block marker + continue, path audit
    (R9.6), journal format
    - _Requirements: 9.2, 9.3, 9.4, 9.6_
    - _Depends: 9.2, 10.2, 11.2_
  - [ ] 13.2 GREEN — `runner.go`
    - _Requirements: 9.2, 9.3, 9.4, 9.6_
- [ ] 14. Run orchestration _Boundary: Runner_
  - [ ] 14.1 RED — preflight (approval/drift/claude present), mode selection via
    doctor, budgets, iteration cap, exit summary, drift-stop mid-run
    - _Requirements: 9.1, 9.5, 9.7_
    - _Depends: 13.2, 7.2_
  - [ ] 14.2 GREEN — `Run()` + `csdd plan run` wiring
    - _Requirements: 9.1, 9.5, 9.7_

### Phase 5: Surfaces + validation (M5)
- [ ] 15. Web Plans tab (P) _Boundary: Web_
  - [ ] 15.1 RED — hardened read-only routes for plan list/detail/journal
    - _Requirements: 13.1, 13.2_
    - _Depends: 3.2_
  - [ ] 15.2 GREEN — `Plans.tsx` + routes
    - _Requirements: 13.1, 13.2_
- [ ] 16. E2E golden path
  - [ ] 16.1 e2e on a fixture workspace: init → author → validate → approve →
    generate → (stub runner) run one feat to done → status/graph/web agree;
    `CGO_ENABLED=0` for all dist targets
    - _Requirements: 9.1, 9.2, 5.1_
    - _Depends: 14.2, 15.2_

**Execution order:** 1 → {2, 3, 5} → 4 → 8 → {9, 11, 12} → 10 → 13 → 14 → {15, 16}.
Phase 2 (6–7) is independent and may ship any time, including first.

---

## 7. Milestones

| Milestone | Delivery | Tasks |
|---|---|---|
| **M1 — The plan contract** | template + `init/validate/status`; plans in the graph; dogfood: this file passes `validate` | 1–5 |
| **M2 — Sandbox (independent)** | `.devcontainer/` scaffold + `doctor`; safe bypass exists before the loop does | 6–7 |
| **M3 — The bridge** | `approve/next/brief/generate` + plan skill; the workflow is followable by hand or supervised session | 8–12 |
| **M4 — The runner** | `csdd plan run` (supervised + autonomous); deviation protocol live | 13–14 |
| **M5 — Surfaces** | web Plans tab + journal view; e2e golden path | 15–16 |
| **Post-v1** | `(P)` feats in parallel worktrees (`claude -w`); csdd-published devcontainer feature; stack.md-driven sandbox suggestions; live run streaming in the dashboard | — |

---

## 8. Risks and decisions

- **`claude` CLI coupling (accepted, contained).** The runner shells out to `claude`;
  flags may drift across versions. Guard: all used flags live in one table, a preflight
  logs `claude --version`, and every other csdd command works without `claude`
  installed (principle 2).
- **Gate gaming (designed out).** Sessions cannot approve, cannot touch
  `plan.md`/`plan.json`/`docs/graph/`/`.csdd/` (mechanical path audit, R9.6), and the
  runner owns commits. The contract chain is the only path to advancement.
- **Runaway spend (bounded).** `--max-iterations` and retry caps bound the loop;
  totals land in the exit summary and journal. Per-session `--max-budget-usd` is
  **off by default** (sessions run under the Claude account's own limits) and set
  only via an explicit `--session-budget` when a tighter ceiling is wanted.
- **Firewall is best-effort under full sudo (documented trade-off).** `HARDENED=true`
  is the recommended sandbox build for unattended runs; doctor reports the sudo
  policy so `run --autonomous` can warn.
- **Prose/machine boundary (kept).** The CLI writes only `plan.json` and `log.md`
  (mechanical formats with precedents: `spec.json`, `docs/graph/log.md`). Revisions
  remain human/LLM acts followed by re-approval.
- **Right-sizing is a convention, not a lint (v1).** The skill teaches it; a
  seeds-size heuristic lint may come later if runs show oversized feats.
- **This document predates its own tooling.** It follows the §4/§5/§6 convention so
  the M1 parser can be pointed at it; migration to `docs/plans/plan-mode/` happens
  when task 4 lands.

---

## 9. Appendices

### 9.1 References
- `docs/plans/PLAN-knowledge-base.md` — the archetype and the substrate (graph, wiki,
  stack are prerequisites this feature consumes).
- github.com/snarktank/ralph — PRD skill (`skills/prd/SKILL.md`) and loop mechanics.
- github.com/protonspy/devcontainer-claude-code — sandbox base (this design rebases it
  onto language-agnostic Ubuntu; draft files delivered 2026-07-06).
- code.claude.com/docs/en/devcontainer — official reference: firewall, non-root
  requirement for bypass, credential caveats (never mount host secrets; trusted repos
  only).

### 9.2 Executor notes (this repo / workstation)
- **Verify:** `make check` (gofmt + vet + race). Distribution gate: `CGO_ENABLED=0`
  via `make dist` (6 targets).
- **CLI pattern:** stdlib `flag` + subcommand switch in `internal/cli/cli.go`;
  `--json` via the `jsonout` helpers; table-driven tests beside each file.
- **Line endings:** CRLF working tree over LF index on this workstation — stage only
  the paths you changed; never `git add -A`. Sandbox files must be written LF.
- **Flaky tests:** four update/clean `.old`-backup CLI tests fail non-deterministically
  on `/mnt/c` (WSL) — re-run before treating as regression.
- **Go toolchain:** `~/.local/go/bin` (export PATH + GOPATH/GOCACHE).
- **Before starting:** `git fetch origin` and check open PRs — parallel sessions land
  related work.
- **Commits/PRs:** conventional style (`feat(plan): …`); repo templates forbid
  AI attribution in commits/PRs.
