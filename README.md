# csdd

**Claude Spec-Driven Development — as an executable contract.**

`csdd` is a single Go binary that makes **Spec-Driven Development (SDD) + Test-Driven Development (TDD)** the native, *enforced* workflow inside a [Claude Code](https://claude.com/claude-code) repository. It turns the SDD lifecycle — `requirements → design → tasks → implementation` — from "good intentions in markdown" into a contract that is **validated mechanically and gated by human approval at every phase**, so neither a human nor an AI agent can skip a step, ship a requirement without a testable criterion, or mark a parallel task without declaring its boundary.

Around that spec core it adds two more layers: **plan mode** (`docs/plans/`), which decomposes a multi-feature initiative into feats that a runner drives to shipped code, and a **knowledge base** (`docs/`) — a graph "brain", an LLM-authored wiki, decision records, a glossary, and a tech contract — that keeps the *why* consultable instead of re-derived. There's no server and no database: the plain Markdown/JSON files under `.claude/`, `specs/`, and `docs/` *are* the API, reviewable in any pull request.

> **Convention in this README:** commands are written `csdd …`. Zero install? The exact equivalent is `npx @protonspy/csdd …` — or alias it: `alias csdd='npx @protonspy/csdd'`.

## Install & run

**npx — zero install, always the latest.** Fetches the right prebuilt binary for your OS/arch:

```bash
npx @protonspy/csdd --help    # run instantly
npx @protonspy/csdd           # no args → interactive TUI
```

**Global install** — a short `csdd` on your `PATH`:

```bash
npm install -g @protonspy/csdd
csdd --help
```

**Other ways in:** grab a prebuilt archive from the [releases page](https://github.com/protonspy/csdd/releases) and put `csdd` on your `PATH`, or build from source with `go install github.com/protonspy/csdd@latest`.

---

## Quickstart

```bash
# 1 · bootstrap this repo into a csdd workspace (no install)
npx @protonspy/csdd init --with-baseline
```

Then, **inside Claude Code**, adapt the workspace to *this* project's stack in one step:

```
/csdd-setup-init       # inspect the project → tailor steering, a specialized implementer agent, and skills
/csdd-setup-update     # later: re-inspect and apply targeted adjustments, preserving your edits
```

Take your first feature from idea to ready-to-implement:

```bash
csdd spec init my-feature
csdd spec generate my-feature --artifact requirements   # → validate → approve → design → tasks → implement
```

### Commands at a glance

| Command | What it does |
|---|---|
| `csdd init [--with-baseline] [--hooks] [--prepush]` | bootstrap the workspace (steering · skills · agents · commands · docs); the Claude Code hooks and the pre-push gate are opt-in via `--hooks`/`--prepush` (or `--include hooks,pre-push`) |
| `csdd update [--dry-run]` · `clean` · `destroy` | upgrade managed artifacts (keeping your edits as `.old`) · sweep `.old` backups · tear the workspace back down |
| `csdd copy <kind>/<name>` | cherry-pick one shipped artifact (skill · agent · rule · command · hook · template · steering) — the complement of `init --exclude` |
| `/csdd-setup-init` · `/csdd-setup-update` | adapt / refresh the workflow for your stack — Claude Code slash commands |
| `csdd spec init · generate · validate · approve · status · test-report` | the requirements → design → tasks gated lifecycle |
| `csdd plan init · validate · approve · status · next · brief · generate · run` | decompose an initiative into feats; drive each feat → spec → shipped code |
| `csdd sandbox init · doctor` | scaffold a default-deny-egress devcontainer and prove its isolation |
| `csdd graph build · query · path · explain · analyze · export` | the knowledge-base **brain** — a structured index over the whole workspace |
| `csdd wiki init · lint` | the LLM-authored knowledge base under `docs/` |
| `csdd codewiki lint` | the repo-derived document under `docs/raw/` — structure, slugs, and every citation resolved against the checkout |
| `csdd steering · skill · agent · mcp …` | author the governed resources (`create · list · show · delete`, plus `validate`) |
| `csdd web` | read-only live dashboard — spec progress, task board, file viewer |
| `/csdd-commit` | Conventional-Commit the reviewed slice, written from the diff + active spec |

> New to the flow? Read on for the *why*. In a hurry? The three blocks above are the whole loop.

---

## The problem

AI agents write code fast. Too fast to ship without a contract.

- **Scope creep.** Without an explicit, testable requirement, the agent "interprets" — and every run interprets differently. There is no baseline to review against.
- **Zero traceability.** Code with no link to a requirement. Nobody knows *why* a function exists, or what breaks if it changes.
- **Human review too late.** The human only sees the result in the PR — when reverting is expensive. The decision should be visible *before* the code.

The answer is **SDD — Spec-Driven Development**: a contract layer (requirements → design → tasks) with human approval at every gate. **csdd is the tool that makes that contract impossible to break by accident.**

---

## What csdd is

A **CLI + TUI + Web** in a single Go binary — the only sanctioned author of the workflow's artifacts. You don't hand-edit frontmatter, `spec.json`, or task annotations — you generate from a template, edit the body, and validate.

### 🖥️ CLI — for agents & automation

Flag-driven, headless. Exposes **100% of the functionality** so Claude Code, Cursor, Codex, or CI can drive the binary without reading the source.

```bash
csdd spec generate photo-albums --artifact requirements
```

### ⌨️ TUI — for humans

Interactive interface (Bubble Tea). Running `csdd` with no arguments opens wizards and an artifact browser. Same operations, same rules.

```bash
csdd   # no args → interactive TUI
```

### 🌐 Web — live dashboard

`csdd web` serves a **read-only** dashboard in your browser: spec progress (phase, approvals, % of tasks done, validation status), navigable requirements / design / tasks (Markdown + Mermaid Boundary Maps), a **live task board** that updates as files change on disk, and a VS Code-style file viewer (Monaco) for the whole workspace.

```bash
csdd web              # serve the dashboard and print its URL
csdd web --port 8080  # custom port
csdd web --tunnel     # expose it publicly via a tunnel (forces auth)
```

It's a *view*, not an author — the CLI stays the only thing that writes artifacts. And it's still a single binary: the React/Vite/Monaco UI is built ahead of time and embedded, so it runs **offline** with no extra runtime dependency. Binds to `127.0.0.1` by default.

> 🔑 **Core principle:** every surface calls the **same operation helpers**. A single source of truth — what a human does in the TUI, an agent does identically via the CLI.
>
> The prebuilt binaries (npm / releases) embed the dashboard. Building from source? Run `make web-build` first to embed the UI — otherwise `csdd web` serves a placeholder page (the API still works).

---

## Phase gates — the heart of the flow

Four phases, three human gates:

```
Discovery → [gate] Requirements → [gate] Design → [gate] Tasks → Implementation
```

State lives in `spec.json`. Generating `design` while `requirements` is not approved **fails** — it's not a warning, it's exit code **2**.

- `ready_for_implementation` only becomes `true` after all 3 approvals.
- `--force` breaks the gate — only with explicit human authorization, and it shows up in history.

```bash
csdd spec generate albums --artifact design
✗ phase gate: 'requirements' must be
  approved before generating 'design'.

# the right path:
csdd spec approve albums --phase requirements
✓ requirements approved
```

### 📜 Specification vs. 🧩 Design — the distinction that matters most

- **📜 Specification — the contract.** The requirements, the File Structure Plan in the design, and the `_Boundary:_` / `_Depends:_` annotations on the tasks. 👤 **Humans review and approve this.**
- **🧩 Design — the implementation space.** Components, internals, sequencing *within* each task. How the contract is fulfilled. 🤖 **The agent is free here, after approval.**

No phase is generated before the previous one is approved by a human — **enforced mechanically**, not by convention.

---

## Feature lifecycle — from idea to ready-to-implement

```bash
# 1 · create the feature workspace
csdd spec init photo-albums

# 2 · requirements → edit in EARS → validate → approve
csdd spec generate photo-albums --artifact requirements
csdd spec validate photo-albums   # exit 2 = fix what it flags
csdd spec approve  photo-albums --phase requirements

# 3 · design (blocked until step 2 passes)  → 4 · tasks (same)
csdd spec generate photo-albums --artifact design   # ... validate, approve
csdd spec generate photo-albums --artifact tasks    # ... validate, approve

✓ spec.json: ready_for_implementation = true   # implementation can begin
```

> 💡 `csdd spec status <feature>` between any two steps: phase + approvals + validation issues on a single screen.

### Conventions the validator enforces

**📝 Requirements in EARS.** Fixed, testable syntax, one behavior per criterion. `SHALL` — never `should`. Unique `N.M` IDs.

```
### Requirement 1: Album Management
1. WHEN a user creates an album
   THEN the system SHALL persist it <500ms.
2. IF the name is empty
   THEN the system SHALL return 400.
```

**✅ Annotated tasks (not a todo-list).** Each leaf traces requirements; parallelism is declared and verified.

```
- [ ] 2. AlbumService _Boundary: AlbumService_
  - [ ] 2.1 create / rename / delete
    _Requirements: 1.1, 1.2_
- [ ] 3. PhotoService _Boundary: PhotoService_ (P)
  - [ ] 3.1 upload S3
    _Requirements: 2.1_
    _Depends: 1.2_
```

- `_Requirements:_` on every leaf · `_Boundary:_` on every `(P)` · `_Depends:_` between boundaries.
- `(P)` = runs in parallel. **Two `(P)` tasks cannot share a boundary** — the validator rejects it, guaranteeing safe parallel execution by agents.

---

## Implementation phase — one task at a time, TDD

Once the gate is open, the agent implements in **TDD**:

```
RED (write the test) → GREEN (minimum to pass) → REFACTOR (clean under green) → widen the net (full suite + lint)
```

Drive it with the shipped **`implementer` agent** — a language-agnostic sub-agent that takes one task, drives it through the cycle the spec's `development_flow` declares, stays inside its `design.md` boundary, runs the gate, records evidence, **marks the task done**, and reports. Specialize it per stack (e.g. a `go-developer`) via steering and skills — `/csdd-setup-init` derives one for your project.

### The flow decides the cycle — `development_flow`

Each spec declares how its behaviors are delivered, and `csdd spec validate` **enforces the shape** it implies (exit 2, naming the task and line). Set it at creation: `csdd spec init <feature> --flow unit|tdd|tdd-e2e`, or workspace-wide via `default_development_flow` in steering.

| Flow | Cycle | Use it for |
|---|---|---|
| **`tdd`** (default) | `tdd-cycle` — RED → GREEN → REFACTOR | money, auth, tenancy, anything irreversible |
| **`unit`** | `unit-cycle` — implement → cover → gate | surfaces whose behaviors are render/CRUD states; halves the verification round-trips per behavior |
| **`tdd-e2e`** | `tdd-cycle` + an end-to-end pass | flows that must be exercised whole |

### 🔴 Skill `tdd-cycle` (under `tdd` / `tdd-e2e`)
- **One leaf task per invocation.** Takes the ID from `specs/<f>/tasks.md`; doesn't batch tasks "to save time".
- **RED fails for the right reason.** A compile error doesn't count — it cites the failure before moving on.
- **Never weakens a test** to make the suite pass. New behavior = new RED.
- **Marks the task done.** Once it's green, checks the task `[x]` in `tasks.md` — so progress (and the dashboard) reflect reality.

### 🟢 Skill `unit-cycle` (under `unit`)
`unit` drops the RED-first ritual, **not the test**. It buys one fewer process invocation and one fewer round-trip per behavior; it costs the proof that the test could ever fail. So the cycle charges the cheap substitute: **write the assertion from the acceptance criterion the task cites**, never from the code you just wrote — a test written after the implementation drifts toward describing what the code *does*. It also names when to stop and escalate back to `tdd`.

### ✅ `verify-change` + Definition of Done
Before reporting done: run the executable checks and produce **real evidence** — "compiles" and "looks right" are not *done*.

```
go test ./...   ✓
lint            ✓
typecheck       ✓
build           ✓
```

Each leaf traces `_Requirements:_` → the test proves the requirement. **Evidence beats assertion.** Capture it into `specs/<f>/test-report.json` with `csdd spec test-report` (parses JUnit/coverage, or auto-discovers reports for go/python/typescript/java/rust).

---

## From done code to PR — fixed order, with evidence

```
code-reviewer → /csdd-commit → git push (pre-push gate) → Pull Request
```

### 🔎 Adversarial review — skill `pr-review`
- `code-reviewer` runs on the diff; resolve every **Blocker** before moving on.
- `security-reviewer` if it touches auth / secrets / input — resolve Critical/High.
- Reviewers **don't write** — you apply the fix and re-review until clean.

### ✍️ `/csdd-commit` + pre-push gate

```
# Conventional Commits, generated from the diff + spec
feat(photo-albums): add album rename

Implements photo-albums; tasks 2.1, 2.2.

# git push → hook runs the suite; red BLOCKS
git push
✗ pre-push: test gate failed — push blocked
```

**Never** commit with an open Blocker; **never** `git push --no-verify`. The PR carries **evidence**: spec links · completed tasks · real check output · risks.

---

## Plan mode — orchestrate many specs

One spec is one feature. A real initiative spans several, with dependencies and a why. **Plan mode is the layer *above* specs:** a plan at `docs/plans/<slug>/` decomposes an initiative into **feats**, where **each feat becomes exactly one spec**.

```
initiative ──▶ plan (docs/plans/<slug>/) ──▶ feats ──▶ one spec each ──▶ shipped code
```

A `plan.md` carries three machine-parsed sections that `csdd plan validate` checks:

| Section | What it declares |
|---------|------------------|
| `## Feats` | one row per feat: `Feat` (kebab slug → spec dir) · `Objective` · `Depends` (feat slugs, no cycles) · `Milestone` · `(P)` · `Refs` |
| `## Quality Gates` | `- <label>: <command>` lines injected into each feat's brief as checks the session runs itself before declaring done; ≥1 required |
| `## Executor Notes` | opaque guidance inlined verbatim into every session brief |

The `Refs` column is how a feat cites its *why*: `[[wiki-page]]`, `stack:<name>`, and `adr:<slug>` tokens — each must resolve, or validation breaks. Cited decision records are inlined **in full** into that feat's run brief.

### Two human gates, machine work between

```
csdd plan approve  ──▶  csdd plan run  ──▶  Pull Request
   (human gate)         (csdd-controlled)     (human gate)
```

`csdd plan run <slug>` is a **deliberately dumb, Ralph-style loop** where **one iteration = one Claude session** (default cap 100), and each session is handed **one whole feat as its mission**. The session — Claude driving csdd — owns the entire flow: it authors and approves the spec (`csdd spec generate/validate/approve`), implements every task, runs the dev-cycle, owns git (branch, `/csdd-commit`, the pre-push gate, the PR), and records any open decision it makes (a `docs/stack.md` Decided row, an ADR when the why needs more than a line). **The loop trusts the session and does not verify** — there is no runner-side gate. It hands out feats in plan order, spawns the session, and records what it declares in a per-feat ledger (`.csdd/plan/<slug>/progress.json`). Nothing parks mid-run — **every failure becomes the next session's context** (the failure trail, the predecessor's handoff), which is what makes the loop self-correcting.

The session's verdict declares one of two intents: `done` (the whole feat is delivered and the session's own checks — `csdd spec validate`, `csdd graph analyze --strict`, the plan's Quality Gates — pass) or `continue` (honest partial work; the summary is the handoff to the next session on the same feat).

The run ends five ways: the plan **completes** (every feat is in the ledger), the **iteration cap** is hit (the wallet guard), the **stall guard** trips (default: 10 consecutive *failed* sessions — a session error, not a large feat mid-flight), the session **cannot be started at all** (5 consecutive spawn failures — a broken environment, not a broken plan), or a preflight refusal (unapproved or drifted plan).

A run is **resumable**: re-running `csdd plan run <slug>` after a crash or a Ctrl-C picks up where it stopped. Delivered feats come from the ledger and the spec tree; how many attempts each unfinished feat has spent, the handoff its last session left, and which feats already exhausted their bound are rebuilt from the append-only session record (`.csdd/plan/<slug>/sessions.jsonl`) — so an interruption neither resets a feat's attempt budget nor throws away the handoff.

```bash
csdd plan status <slug>                    # feats, milestones, what's next
csdd plan run    <slug>                     # bypass-mode loop — alerts + asks if the sandbox isn't verified
csdd plan run    <slug> --yes               # pre-accept the unverified-sandbox alert (non-interactive)
                        --session-budget 5   # per-session USD cap · --max-iterations (100) · --stall (10)
```

#### When a run ends without completing

There are no block markers to clear. Just `csdd plan run <slug>` again — it resumes from the ledger, re-handing whatever feat is not yet marked done, and the first session on a previously-failed feat is pointed at its failure log under `.csdd/plan/<slug>/failures/<feat>.log` (every attempt's untruncated output). `docs/plans/<slug>/log.md` journals every feat outcome for the audit trail.

### 🔒 `sandbox` — isolation before autonomy

`plan run` always drives Claude in bypass mode (`--dangerously-skip-permissions`), so it wants a **proven** sandbox: when `sandbox doctor` fails, the runner shows the failing checks and asks before continuing — declining closes the run.

```bash
csdd sandbox init --hardened   # scaffold a default-deny-egress devcontainer (--feature, --allow-domain)
csdd sandbox doctor            # prove the isolation holds before bypass mode is allowed
```

> Plans are authored by the **`/prd`** skill (interview → research → stack → decompose into feats → `csdd plan validate`). A single feature stays on the lighter **`quick-prd`** on-ramp.

---

## Knowledge base — the *why*, consultable

`csdd init` seeds a knowledge base under `docs/`. It exists so understanding comes from **traversing relationships**, not re-reading source; and so decisions and vocabulary are recorded once, not re-litigated.

### 🧠 Graph — a structured brain over the workspace

`csdd graph` builds a deterministic index (`docs/graph/graph.json.gz`, a byte-stable gzip blob — commit it with the change; conflicts are resolved by rebuilding, not hand-merging) linking specs, tasks, `.claude/` artifacts, `docs/`, and source. **Consult it before you grep:**

```bash
csdd graph build              # rebuild the index (+ appends docs/graph/log.md)
csdd graph query "<terms>"    # find nodes by label tier + their neighborhood
csdd graph path "<A>" "<B>"   # shortest path between two nodes
csdd graph explain "<label>"  # a node plus its connections, by neighbor degree
csdd graph analyze --strict   # traceability gaps + tech/wiki lints (non-zero exit)
csdd graph export             # a self-contained docs/graph/graph.html visualization
```

### 📚 Wiki — LLM-authored, provenance-tracked

`csdd wiki init` scaffolds `docs/raw/` (a dropzone the CLI never edits) and `docs/wiki/` (index, log, pages). When a source lands in `docs/raw/`, the **`wiki`** skill's *Ingest* workflow reads it and writes cross-linked pages under `docs/wiki/pages/` with `sources:` provenance. `csdd wiki lint` reports broken wikilinks, orphan pages, index/log desync, and unprocessed sources.

### 🧭 Codewiki — a repository read into one cited document

Source code is a different kind of drop: a repository is not something to ingest page by page, it is something to **read**. Drop the checkout into `docs/raw/<dir>/` (or a `.zip` unpacked beside itself) and run **`/csdd-codewiki`** — the **`codewiki`** skill compiles it into a single cited document, `docs/raw/<owner>-<repo>.md`: a `Structure` tree, `<<< SECTION: >>>`-delimited pages, mermaid for the moving parts, tables for every enum and flag, and a glossary. *That* document is the source you then ingest with `/csdd-wiki-ingest`.

The format is an interchange shape, not a private one — a document you compile locally and one produced by an external repo-wiki generator are the same artifact, linted by the same gate:

```bash
csdd codewiki lint                       # every repo-derived doc in docs/raw/
csdd codewiki lint <file> --repo <dir>   # resolve citations against a checkout
```

`csdd codewiki lint` is the deterministic half of the deal — the LLM writes the prose, the CLI proves the citations. It holds the `Structure` tree and the section delimiters to each other, checks slugs are unique and derived from their headings, fails a section that cites nothing, and resolves **every** `[path:start-end]()` against the checkout: a path that is not in the tree, a range past the end of the file, a citation climbing out of the repo with `..`. Run against externally generated documents it finds exactly that class of defect: a camelCase filename cited in snake_case, a name shortened to what it "should" be called, a 43-line manifest cited through line 60.

A checkout (a directory carrying `.git/` or a dependency manifest) and an archive unpacked beside itself are **material**, not sources: the graph skips them, so a 200-file repo does not become 200 `raw_source` nodes and 200 unprocessed-source findings. The obligation moves to the one document derived from it — an archive nobody unpacked stays flagged.

### 🧾 Decision records · 📖 glossary · 🧱 tech contract

| Artifact | Location | The rule |
|----------|----------|----------|
| **Decision records (ADR)** | `docs/adr/NNNN-<slug>.md` | A decision that passes the **triple gate** — hard to reverse · surprising without context · a real trade-off — becomes an append-only ADR. Feats cite it as `adr:<slug>`; a broken/superseded citation breaks `csdd plan validate`. Never edit a superseded record — mark it and write its successor. |
| **Glossary** | `docs/glossary.md` | Each domain concept has one **canonical term** + an `_Avoid_` list of banned synonyms. Slugs and page names are minted from canonical terms; using an avoided term as a whole token is a lint. Renaming a term appends the old name to the successor's `_Avoid_` (a tombstone). |
| **Tech contract** | `docs/stack.md` | Any technology **not** listed is an **open decision** — propose options and ask the human, never adopt a dependency silently. The **`stack`** skill refines each library against current docs before first use. |

> Skills own the authoring: **`graph`**, **`wiki`**, **`codewiki`**, **`glossary`**, **`stack`** (plus slash commands like `/csdd-graph-query`, `/csdd-wiki-ingest`, `/csdd-codewiki`, `/csdd-stack-refine`). The CLI is the only author of `docs/graph/` and `.csdd/`; nothing ever edits what is already under `docs/raw/` — `codewiki` only ever adds one new document.

---

## On-ramps — from an idea to an approved spec

The contract above starts at `requirements.md`. Three skills bracket it upstream, all
landing in the same place: a normal, validated csdd spec or plan.

| Skill | For | Produces |
|---|---|---|
| **`prd`** (`/prd`) | a whole initiative — several features, with dependencies and a why | a `docs/plans/<slug>/` plan whose feats each become one spec |
| **`quick-prd`** | a single feature | `docs/product/prd-<feature>.md` — stories + EARS-shaped `FR-N`, decision-ready in one pass |
| **`spec-brainstorm`** | an idea that needs requirements now | question-driven EARS requirements, straight into `csdd spec` |

> 🔑 An on-ramp never bypasses the contract. Whatever it produces still enters through
> `csdd spec generate` and still passes the same mechanical gates.

---

## The resources csdd governs

csdd is the only sanctioned author of these artifacts, so their structure stays machine-checkable instead of drifting into free-form prose.

| Resource | What it is | Location |
|----------|-----------|----------|
| 🧭 **steering** | Project memory loaded into every agent interaction. Standards and the *why* behind decisions. | `.claude/steering/*.md` |
| 📐 **spec** | Per-feature contract: `spec.json` + requirements + design + tasks (+ research/bugfix). | `specs/<feature>/` |
| 🗺️ **plan** | Multi-feature initiative decomposed into feats, each feat → one spec. | `docs/plans/<slug>/` |
| 🛠️ **skill** | Executable workflow bundle: `SKILL.md` + references + assets + scripts. | `.claude/skills/<name>/` |
| 🤖 **agent** | Custom sub-agent with a **least-privilege** tool scope (reviewer, debugger…). | `.claude/agents/<name>.md` |
| 🔌 **mcp** | Model Context Protocol servers the agent can connect to. stdio or remote, never both. | `.mcp.json` |
| 📚 **knowledge base** | Graph, wiki, decision records, glossary, tech contract. | `docs/` |

**Verbs per resource.** Common base: `create/init · list · show · delete`. `spec` adds `generate · approve · validate · status · test-report`; `plan` adds `validate · approve · status · next · brief · generate · run`; `mcp` uses `add · install · presets · remove · enable · disable · validate`; `skill` adds `add-reference/script/asset · validate`; `graph`, `wiki`, and `codewiki` are CLI-only builders/linters.

---

## Upgrading safely — `update`, `clean`, `destroy`

A new csdd version ships new and improved managed artifacts. `csdd update` brings your workspace up to that version **without ever losing your setup**:

```bash
csdd update --dry-run   # preview exactly what would change
csdd update             # apply it
```

It tracks what it wrote in `.csdd/manifest.json` (a content-hash baseline) and, for each csdd-managed file, decides:

| On disk vs. shipped | What update does |
|---|---|
| missing | **adds** it (new in this version) |
| identical | leaves it (already current) |
| pristine but outdated | **refreshes** it in place — you never touched it |
| **edited by you** | writes the new version **and** keeps your copy as `<file>-1.old` (then `-2.old`, …) |

So nothing is silently overwritten: any file you customized is preserved as a numbered `.old` backup beside it to diff and fold in. `--force` overwrites in place without the backup. When you're done reconciling, `csdd clean` sweeps the `*-N.old` backups; `csdd destroy` tears the workspace back down (`.claude/`, `CLAUDE.md`, `.mcp.json`, the pre-push hook) for a fresh re-install — **keeping your `specs/`**.

> 🔒 **Never touched by update:** your `specs/`, your filled `.claude/steering/*.md`, custom (non-shipped) skills/agents, `.mcp.json`, `.claude/settings.json`, `docs/` you authored, and `CLAUDE.md`. Update reconciles only the pure-csdd artifacts.

---

## What the validator catches

| Gate | Checks |
|------|--------|
| **spec · requirements** | Every criterion starts with WHEN/WHILE/IF/WHERE/THE SYSTEM · none uses `should` · `### Requirement N:` headers unique |
| **spec · design** | Boundary Map and File Structure Plan sections present · every requirement ID appears in the traceability table · `design.md` ≤ 1000 lines (else split the spec) |
| **spec · tasks** | Every leaf has `_Requirements:_` with real IDs · every `(P)` has a `_Boundary:_` that matches the design · no `(P)` pair shares a boundary · task shape matches the spec's `development_flow` (under `tdd`/`tdd-e2e` a GREEN step follows a RED and a RED is answered by a GREEN; under `unit` the RED/GREEN shape is a flow mismatch) |
| **plan** | Feats table well-formed · `Depends` resolve with no cycles · every `Refs` token (`[[wiki]]` · `stack:` · `adr:`) resolves · ≥1 quality gate · no avoided glossary terms |
| **graph · analyze** | Traceability gaps + tech lints (`undeclared`/`phantom`/`unrefined`) + wiki lints · `--strict` exits non-zero |
| **wiki · lint** | Broken wikilinks · orphan pages · index/log desync · unprocessed `docs/raw/` sources |
| **codewiki · lint** | Provenance header · `Structure` ↔ sections in sync · unique, heading-derived slugs · every section cites something · every `[path:start-end]()` resolves in the checkout |
| **skill · mcp · steering** | `SKILL.md` ≤ 500 lines / ~5k tokens, refs cited · mcp: exactly 1 transport (stdio **or** url) · steering: valid `inclusion`, fileMatch has a pattern |

**Exit codes:** `0` ok · `1` usage error · `2` validation failure. Scriptable in CI.

---

## Architecture — two surfaces, one core

```
                    cmd/csdd/main.go
            (no args → TUI · with args → CLI)
                             │
        ┌────────────────────┴────────────────────┐
        internal/cli · CLI       internal/tui · TUI
  dispatcher, 1 file/resource           Bubble Tea · wizards + browser
        └──── both call the SAME operation helpers ────┘
                             │
   workspace · paths · validator · templater · plan · graph · frontmatter · render
                             │
   artifacts on disk: .claude/ · specs/ · docs/ · CLAUDE.md · .mcp.json
              (plain text, reviewable in a PR)
```

| Package | Responsibility | Why it matters |
|---------|---------------|----------------|
| `internal/cli` | **CLI surface.** Dispatches `resource action`, flag parsing, 1 file per resource. Includes `CLAUDE.md` and `.gitignore` wiring. | The public contract — 100% of functionality, headless. |
| `internal/tui` | **Interactive front-end** (Bubble Tea): menu, wizards, artifact browser. | Calls the same helpers as `cli`. No duplicated logic. |
| `internal/workspace` | Resolves the workspace root (the `.csdd/` marker) by walking up the tree; validates kebab-case; enumerates phases and artifacts. | Defines what a workspace *is* and the valid names. |
| `internal/paths` | Centralizes the on-disk layout: `.claude/`, `.csdd/`, `docs/`, `specs/`, `CLAUDE.md`, `.mcp.json`. | The layout lives in *exactly one* place. |
| `internal/validator` | **The mechanical checks:** EARS, unique IDs, traceability, annotations, parallelism safety, skill structure. | The agent's "friend." Never asks for judgment — only true/false. Exit 2. |
| `internal/plan` | Plans, feats, the run loop, ADR parsing, and the sandbox scaffold. | Turns a multi-feature initiative into gated, runnable work. |
| `internal/graph` | Builds the deterministic knowledge graph and runs its analyses/lints. | The structured brain — consult over re-reading. |
| `internal/templater` | Renders templates **embedded at compile time** (`go:embed`). | A fully self-contained binary — zero runtime dependencies. |
| `internal/frontmatter` | Parser for a minimal subset of YAML (scalars, bool, inline arrays). | Does only what's needed — small, predictable surface. |
| `internal/render` | Terminal output helpers with color (respects `NO_COLOR`/TTY). | Consistent `✓ ✗ ! •` messages in the CLI. |

### Design principles — four deliberate choices

1. **CLI = TUI, always.** Both surfaces converge on the same helpers. There is no function only the TUI can do — which is why a headless agent has 100% of the power.
2. **Embedded templates.** `go:embed all:templates` compiles the templates into the binary. You download one file and it works offline, with nothing to install.
3. **Mechanical, not opinionated, validation.** The validator never asks for judgment: either the criterion starts with `WHEN` or it doesn't. Deterministic → an agent can trust the exit code.
4. **Artifacts are plain text.** Everything becomes versionable markdown/JSON in `.claude/`, `specs/`, and `docs/`. Review happens in the PR, with the tools the team already uses.

> The result: **the CLI never stops you from doing the right thing** — it stops you from doing the wrong thing *without making the decision visible*. Breaking a gate requires an explicit `--force`, and that shows up in history.

---

## Integration — native to Claude Code, no conversion layer

The workspace `csdd` writes **is** the layout Claude Code expects. `csdd init` bootstraps it and handles the wiring:

```
CLAUDE.md             # entry point + steering imports + knowledge-base workflow
.claude/steering/*.md # @-referenced from CLAUDE.md
.claude/agents/*.md   # sub-agents (implementer, code-reviewer, …)
.claude/skills/<n>/   # skill bundles
.claude/commands/     # slash commands (/csdd-setup-init, /csdd-commit, /prd, /csdd-graph-query, …)
.claude/hooks/        # deterministic automation (format-after-edit, …) — opt-in via `init --hooks`
.csdd/                # csdd's operational state (the workspace marker + manifest.json) — the csdd analog of .git/
specs/<feature>/      # SDD contracts
docs/                 # knowledge base: plans/ · adr/ · wiki/ · graph/ · raw/ · glossary.md · stack.md · product/
.mcp.json             # MCP servers
```

Creating a steering automatically inserts `@.claude/steering/<name>` into a managed block of `CLAUDE.md` — idempotent, never clobbering manual edits.

**What the team gains:**
- **Zero friction with Claude Code.** Artifacts are read natively — no exporting or converting.
- **Review where we already work.** Specs, steering, and docs are text in a PR — diff, comment, approve.
- **Least privilege by default.** Sub-agents are born with `Read, Grep`; MCP with a restricted scope.
- **CI validates the contract.** `csdd spec validate` (and `plan validate` / `graph analyze --strict` / `wiki lint`) in the pipeline blocks a broken contract before merge.

---

## 🔌 MCP server — drive csdd as native tools

Prefer your agent to call **tools** over shelling out to a terminal? [`@protonspy/csdd-mcp`](mcp-server/) is an MCP server (stdio) that exposes the csdd **development flow** as tools — `csdd_spec_generate`, `csdd_steering_create`, `csdd_spec_approve`, … **27 in total.** It wraps the same CLI, so the contract is intact: phase gates still block, the validator still runs, and **exit 2 surfaces as a distinct "validation failed" result** the agent can branch on. Typed parameters (enums for `artifact`/`phase`/`inclusion`) mean the agent picks valid inputs and the server builds the argv — more precise than hand-written commands.

`csdd init` registers the server in `.mcp.json` for you (pass `--no-mcp` to skip):

```bash
# already wired by `csdd init`; to add it to an existing workspace:
claude mcp add csdd -- npx -y @protonspy/csdd-mcp
```

- **Dev-flow only**, grouped by resource (steering · spec · skill · agent), plus `csdd_version`. **Setup and config management stay on the CLI** — `init`, `mcp`, and `export` are one-time human operations, not agent-loop tools.
- **Same binary, same rules.** The server just builds the argv and runs `csdd` headlessly (`NO_COLOR`, no TTY) — no logic of its own, so the CLI stays the single source of truth.
- **Zero-config binary** via `npx` (the matching prebuilt `csdd` is an `optionalDependency`); override with `CSDD_BIN`.

Full tool reference and configuration: [`mcp-server/README.md`](mcp-server/README.md).

---

## Interop — export to Kiro / Codex

`csdd` is Claude Code-native, but the SDD artifacts aren't locked in. `csdd export` converts the workspace to other agentic toolchains — a one-way, **additive** export that lives alongside `.claude/` (nothing is overwritten in place):

```bash
csdd export kiro     # → .kiro/steering/*.md + .kiro/specs/<feature>/{requirements,design,tasks}.md
csdd export codex    # → AGENTS.md (CLAUDE.md + steering inlined) + .codex/config.toml (MCP)
csdd export kiro --out ./build --force
```

- **Kiro** — steering frontmatter (`inclusion: always|fileMatch|manual|auto`, `fileMatchPattern`) is already Kiro-compatible, so steering copies verbatim; specs copy their SDD markdown (`spec.json` is dropped — Kiro tracks phase state in-IDE).
- **Codex** — Codex has no `@`-import, so the managed steering block in `CLAUDE.md` is replaced by the steering inlined into `AGENTS.md`; `.mcp.json` becomes `[mcp_servers.*]` tables in `.codex/config.toml`.

---

**Takeaways:** The validator is your friend. The gate makes the decision *visible*. Contract before code — requirements → design → tasks, each approved by a human before the next. Plan mode scales that from one feature to a whole initiative; the knowledge base keeps the *why* consultable. Always generate from a template; never hand-write frontmatter or `spec.json`. Least privilege everywhere.
