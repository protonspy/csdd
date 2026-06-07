# csdd

**Claude Spec-Driven Development — as an executable contract.**

`csdd` is a single Go binary that turns the Spec-Driven Development (SDD) workflow for [Claude Code](https://claude.com/claude-code) from "good intentions in markdown" into a contract that is validated mechanically — for humans *and* AI agents.

| 5 | 1 | ~8.7k |
|---|---|-------|
| managed resources | binary, zero runtime deps | lines of Go |

---

## Quickstart — set up csdd in your project

```bash
npx @protonspy/csdd init --with-baseline   # bootstrap this repo into a csdd workspace (no install)
```

Then, **inside Claude Code**, adapt the workspace to *this* project's stack in one step:

```
/csdd-setup-init       # inspect the project → tailor steering, a specialized implementer agent, and skills
/csdd-setup-update     # later: re-inspect and apply targeted adjustments, preserving your edits
```

Take your first feature from idea to ready-to-implement:

```bash
npx @protonspy/csdd spec init my-feature
npx @protonspy/csdd spec generate my-feature --artifact requirements   # → validate → approve → design → tasks → implement
```

> Prefer a short command on your `PATH`? `npm install -g @protonspy/csdd`, then call `csdd …` directly.

### Commands at a glance

| Command | What it does |
|---|---|
| `npx @protonspy/csdd init [--with-baseline]` | bootstrap the workspace (steering · skills · agents · commands · hooks) |
| `/csdd-setup-init` · `/csdd-setup-update` | adapt / refresh the workflow for your stack — Claude Code slash commands |
| `npx @protonspy/csdd spec init · generate · validate · approve · status` | the requirements → design → tasks gated lifecycle |
| `npx @protonspy/csdd steering · skill · agent · mcp …` | author the 5 governed resources (`create · list · show · delete`, plus `validate` where applicable) |
| `npx @protonspy/csdd update [--dry-run]` | upgrade managed artifacts, preserving your edits (`.old` backups) |
| `npx @protonspy/csdd web` | read-only live dashboard — spec progress, task board, file viewer |
| `/csdd-commit` | Conventional-Commit the reviewed slice, written from the diff + active spec |

> New to the flow? Read on for the *why*. In a hurry? The three blocks above are the whole loop.
> Examples use `npx @protonspy/csdd` (no install). Installed globally? Drop the prefix — `csdd …`.

---

## The problem

AI agents write code fast. Too fast to ship without a contract.

- **Scope creep.** Without an explicit, testable requirement, the agent "interprets" — and every run interprets differently. There is no baseline to review against.
- **Zero traceability.** Code with no link to a requirement. Nobody knows *why* a function exists, or what breaks if it changes.
- **Human review too late.** The human only sees the result in the PR — when reverting is expensive. The decision should be visible *before* the code.

The answer is **SDD — Spec-Driven Development**: a contract layer (requirements → design → tasks) with human approval at every gate. **csdd is the tool that makes that contract impossible to break by accident.**

---

## What csdd is

A **CLI + TUI** in a single Go binary — the only sanctioned author of the workflow's artifacts. You don't hand-edit frontmatter, `spec.json`, or task annotations — you generate from a template, edit the body, and validate.

### 🖥️ CLI — for agents & automation

Flag-driven, headless. Exposes **100% of the functionality** so Claude Code, Cursor, Codex, or CI can drive the binary without reading the source.

```bash
npx @protonspy/csdd spec generate photo-albums --artifact requirements
```

### ⌨️ TUI — for humans

Interactive interface (Bubble Tea). Running `csdd` with no arguments opens wizards and an artifact browser. Same operations, same rules.

```bash
npx @protonspy/csdd   # no args → interactive TUI
```

> 🔑 **Core principle:** both surfaces call the **same operation helpers**. A single source of truth — what a human does in the TUI, an agent does identically via the CLI.

### 🌐 Web — live dashboard

`csdd web` serves a **read-only** dashboard in your browser: spec progress (phase,
approvals, % of tasks done, validation status), navigable requirements / design /
tasks (Markdown + Mermaid Boundary Maps), a **live task board** that updates as
files change on disk, and a VS Code-style file viewer (Monaco) for the whole
workspace — specs, steering, skills, agents, MCP, hooks and commands.

```bash
npx @protonspy/csdd web              # serve the dashboard and print its URL
npx @protonspy/csdd web --port 8080  # custom port
npx @protonspy/csdd web --tunnel     # expose it publicly via a tunnel (forces auth)
```

It is a *view*, not an author — the CLI stays the only thing that writes
artifacts. And it's still a single binary: the React/Vite/Monaco UI is built
ahead of time and embedded, so it runs **offline** with no extra runtime
dependency. Binds to `127.0.0.1` by default.

> The prebuilt binaries (npm / releases) embed the dashboard. Building from
> source (`go install …` or `go build`)? Run `make web-build` first to embed the
> UI — otherwise `csdd web` serves a placeholder page (the API still works).

---

## Install

**npx** (cross-platform — fetches the right prebuilt binary for your OS/arch, no install):

```bash
npx @protonspy/csdd --help         # run instantly, no install
npx @protonspy/csdd                # interactive TUI
```

Prefer the short `csdd` command on your `PATH`? Install it globally:

```bash
npm install -g @protonspy/csdd     # then: csdd
```

**Prebuilt binaries:** grab the archive for your platform from the
[releases page](https://github.com/protonspy/csdd/releases) and put `csdd` on
your `PATH`. **From source:** `go install github.com/protonspy/csdd@latest`.

> The examples below use `npx @protonspy/csdd` (no install needed). Installed
> globally or built from source? Drop the prefix and call `csdd` directly — or
> alias it: `alias csdd='npx @protonspy/csdd'`.

---

## Upgrading safely — `csdd update`

A new csdd version ships new and improved managed artifacts (rules, templates, the shipped skills/agents/commands/hooks, the guide). `csdd update` brings your workspace up to that version **without ever losing your setup**:

```bash
npx @protonspy/csdd update --dry-run   # preview exactly what would change
npx @protonspy/csdd update             # apply it
```

It tracks what it wrote in `.claude/.csdd-manifest.json` (a content-hash baseline) and, for each csdd-managed file, decides:

| On disk vs. shipped | What update does |
|---|---|
| missing | **adds** it (new in this version) |
| identical | leaves it (already current) |
| pristine but outdated | **refreshes** it in place — you never touched it |
| **edited by you** | writes the new version **and** keeps your copy as `<file>-1.old` (then `-2.old`, …) |

So nothing is silently overwritten: any file you customized is preserved as a numbered `.old` backup beside it for you (or an agent) to diff and fold in. `--force` overwrites in place without the backup.

> 🔒 **Never touched by update:** your `specs/`, your filled `.claude/steering/*.md`, custom (non-shipped) skills/agents, `.mcp.json`, `.claude/settings.json`, and `CLAUDE.md`. Update reconciles only the pure-csdd artifacts.

---

## The 5 resources csdd governs

| Resource | What it is | Location |
|----------|-----------|----------|
| 🧭 **steering** | Project memory loaded into every agent interaction. Standards and the *why* behind decisions. | `.claude/steering/*.md` |
| 📐 **spec** | Per-feature contract: `spec.json` + requirements + design + tasks (+ research/bugfix). | `specs/<feature>/` |
| 🛠️ **skill** | Executable workflow bundle: `SKILL.md` + references + assets + scripts. | `.claude/skills/<name>/` |
| 🤖 **agent** | Custom sub-agent with a **least-privilege** tool scope (reviewer, debugger…). | `.claude/agents/<name>.md` |
| 🔌 **mcp** | Model Context Protocol servers the agent can connect to. stdio or remote, never both. | `.mcp.json` |

**Verbs per resource.** Common base: `create/init · list · show · delete`. `spec` adds `generate · approve · validate · status`; `mcp` uses `add · remove · enable · disable · validate`; `skill` adds `add-reference/script/asset · validate`.

---

## Mental model — read this first

Two distinctions matter more than anything else.

### 📜 Specification — the contract
The requirements, the File Structure Plan in the design, and the `_Boundary:_` / `_Depends:_` annotations on the tasks. 👤 **Humans review and approve this.**

### 🧩 Design — the implementation space
Components, internals, sequencing *within* each task. How the contract is fulfilled. 🤖 **The agent is free here, after approval.**

The second distinction is the **phase gates**: no phase is generated before the previous one is approved by a human — and that is **enforced mechanically**, not by convention.

---

## Phase gates — the heart of the flow

Four phases, three human gates:

```
Discovery → [gate] Requirements → [gate] Design → [gate] Tasks → Implementation
```

State lives in `spec.json`. Generating `design` while `requirements` is not approved **fails** — it's not a warning, it's exit code **2**.

- `ready_for_implementation` only becomes `true` after all 3 approvals.
- `--force` breaks the gate — only with explicit human authorization (Quick Plan), and it shows up in history.

```bash
npx @protonspy/csdd spec generate albums --artifact design
✗ phase gate: 'requirements' must be
  approved before generating 'design'.

# the right path:
npx @protonspy/csdd spec approve albums --phase requirements
✓ requirements approved
```

---

## Feature lifecycle — from idea to ready-to-implement

```bash
# 1 · bootstrap (once per repo)
npx @protonspy/csdd init --with-baseline

# 2 · create the feature workspace
npx @protonspy/csdd spec init photo-albums

# 3 · requirements → edit in EARS → validate → approve
npx @protonspy/csdd spec generate photo-albums --artifact requirements
npx @protonspy/csdd spec validate photo-albums   # exit 2 = fix what it flags
npx @protonspy/csdd spec approve  photo-albums --phase requirements

# 4 · design (blocked until step 3 passes)  → 5 · tasks (same)
npx @protonspy/csdd spec generate photo-albums --artifact design   # ... validate, approve
npx @protonspy/csdd spec generate photo-albums --artifact tasks    # ... validate, approve

✓ spec.json: ready_for_implementation = true   # implementation can begin
```

> 💡 `npx @protonspy/csdd spec status <feature>` between any two steps: phase + approvals + validation issues on a single screen.

---

## Conventions the validator enforces

### 📝 Requirements in EARS

Fixed, testable syntax, one behavior per criterion. `SHALL` — never `should`. Unique `N.M` IDs.

```
### Requirement 1: Album Management
1. WHEN a user creates an album
   THEN the system SHALL persist it <500ms.
2. IF the name is empty
   THEN the system SHALL return 400.
3. WHILE deleting THE SYSTEM SHALL
   block new uploads.
```

### ✅ Annotated tasks (not a todo-list)

Each leaf traces requirements; parallelism is declared and verified.

```
- [ ] 2. AlbumService _Boundary: AlbumService_
  - [ ] 2.1 create / rename / delete
    _Requirements: 1.1, 1.2_
- [ ] 3. PhotoService _Boundary: PhotoService_ (P)
  - [ ] 3.1 upload S3
    _Requirements: 2.1_
    _Depends: 1.2_
```

- `_Requirements:_` on every leaf
- `_Boundary:_` on every `(P)`
- `_Depends:_` between boundaries

`(P)` = runs in parallel. **Two `(P)` tasks cannot share a boundary** — the validator rejects it, guaranteeing safe parallel execution by agents.

---

## Implementation phase — one task at a time, TDD

Once the gate is open, the agent implements in **TDD**:

```
RED (write the test) → GREEN (minimum to pass) → REFACTOR (clean under green) → widen the net (full suite + lint)
```

Drive it with the shipped **`implementer` agent** — a language-agnostic sub-agent that takes one task, runs the `tdd-cycle`, stays inside its `design.md` boundary, runs the gate, records evidence, **marks the task done**, and reports. Specialize it per stack (e.g. a `go-developer`) via steering and skills — `/csdd-setup-init` derives one for your project.

### 🔴 Skill `tdd-cycle`
- **One leaf task per invocation.** Takes the ID from `specs/<f>/tasks.md`; doesn't batch tasks "to save time".
- **RED fails for the right reason.** A compile error doesn't count — it cites the failure before moving on.
- **Never weakens a test** to make the suite pass. New behavior = new RED.
- **Marks the task done.** Once it's green, checks the task `[x]` in `tasks.md` — so progress (and the dashboard) reflect reality.

### ✅ `verify-change` + Definition of Done
Before reporting done: run the executable checks and produce **real evidence** — "compiles" and "looks right" are not *done*.

```
go test ./...   ✓
lint            ✓
typecheck       ✓
build           ✓
```

Each leaf traces `_Requirements:_` → the test proves the requirement. **Evidence beats assertion.**

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

## Workflows — upstream & downstream (BMAD-style)

Everything above is the *downstream* contract: requirements → design → tasks → code. But where do the requirements come from, and who decides *what* to build? `csdd init` ships two orchestrator workflows — modeled on the [BMAD Method](https://github.com/bmadcode/BMAD-METHOD)'s phase-gated lifecycle — that bracket the SDD core. Each is an **agent** (the lead) fronting a set of phase **skills**, and the seam between them is a normal, validated csdd spec.

```
wf:product/discovery  ──(handoff: steering + spec requirements)──▶  wf:development
        UPSTREAM                                                        DOWNSTREAM
   what & why, decision-ready                                  how, built & verified
```

### 🧭 `wf-product-discovery` — *what & why* (upstream)

Agent that drives a raw idea to a decision-ready PRD, one optional phase at a time, then lands it into csdd:

| Skill | Produces |
|-------|----------|
| `discovery-product-brief` | `docs/product/product-brief.md` — vision, user, value, scope |
| `discovery-research` | `docs/product/research/*.md` — evidence-graded market / domain / technical findings |
| `discovery-prfaq` | `docs/product/prfaq.md` — Working-Backwards press release + hardest FAQ |
| `discovery-prd` | `docs/product/prd.md` — features with numbered, testable `FR-N` (+ validation checklist) |
| `discovery-ux-spec` | `docs/product/ux/DESIGN.md` + `EXPERIENCE.md` — structure + behavior, traced to journeys |
| `discovery-handoff` | **bridge:** updates steering, runs `csdd spec init`, translates each `FR-N` into EARS requirements that pass `csdd spec validate` |

### 🏗️ `wf-development` — *how, built & verified* (downstream)

Agent that takes an approved spec and drives it to shipped code — **reusing** the existing `tdd-cycle`, `verify-change`, `code-reviewer`/`security-reviewer`, `pr-review`, and the `csdd spec` gates rather than duplicating them. The `dev-*` skills add only the planning layer BMAD covers that csdd did not:

| Skill | Produces | Feeds csdd gate |
|-------|----------|-----------------|
| `dev-architecture` | `architecture.md` + ADRs | fills & approves `design.md` |
| `dev-epics-stories` | `epics.md` + stories | fills & approves `tasks.md` |
| `dev-readiness-check` | PASS / CONCERNS / FAIL verdict | required before the first `tdd-cycle` |
| `dev-sprint` | `sprint-status.yaml` | live task tracking |
| `dev-retrospective` | `retrospective.md` | folds durable lessons into steering |

> 🔑 The workflows never bypass the contract. Discovery output becomes a real EARS spec; architecture and stories become a real `design.md`/`tasks.md`. The phase gates stay mechanical — the workflows just decide *which* gate to open next.

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
   workspace · paths · validator · templater · frontmatter · render
                             │
   artifacts on disk: .claude/ · specs/ · CLAUDE.md · .mcp.json
              (plain text, reviewable in a PR)
```

| Package | Responsibility | Why it matters |
|---------|---------------|----------------|
| `internal/cli` | **CLI surface.** Dispatches `resource action`, flag parsing, 1 file per resource. Includes `CLAUDE.md` and `.gitignore` wiring. | The public contract — 100% of functionality, headless. |
| `internal/tui` | **Interactive front-end** (Bubble Tea): menu, wizards, artifact browser. | Calls the same helpers as `cli`. No duplicated logic. |
| `internal/workspace` | Resolves the `.claude/` root by walking up the tree; validates kebab-case; enumerates phases and artifacts. | Defines what a workspace *is* and the valid names. |
| `internal/paths` | Centralizes the on-disk layout: `.claude/`, `CLAUDE.md`, `.mcp.json`, `specs/`. | The layout lives in *exactly one* place. |
| `internal/validator` | **The mechanical checks:** EARS, unique IDs, traceability, annotations, parallelism safety, skill structure. | The agent's "friend." Never asks for judgment — only true/false. Exit 2. |
| `internal/templater` | Renders templates **embedded at compile time** (`go:embed`). | A fully self-contained binary — zero runtime dependencies. |
| `internal/frontmatter` | Parser for a minimal subset of YAML (scalars, bool, inline arrays). | Does only what's needed — small, predictable surface. |
| `internal/render` | Terminal output helpers with color (respects `NO_COLOR`/TTY). | Consistent `✓ ✗ ! •` messages in the CLI. |

---

## Design principles — four deliberate choices

1. **CLI = TUI, always.** Both surfaces converge on the same helpers. There is no function only the TUI can do — which is why a headless agent has 100% of the power.
2. **Embedded templates.** `go:embed all:templates` compiles the templates into the binary. You download one file and it works offline, with nothing to install.
3. **Mechanical, not opinionated, validation.** The validator never asks for judgment: either the criterion starts with `WHEN` or it doesn't. Deterministic → an agent can trust the exit code.
4. **Artifacts are plain text.** Everything becomes versionable markdown/JSON in `.claude/` and `specs/`. Review happens in the PR, with the tools the team already uses.

> The result: **the CLI never stops you from doing the right thing** — it stops you from doing the wrong thing *without making the decision visible*. Breaking a gate requires an explicit `--force`, and that shows up in history.

---

## What the validator catches

| Gate | Checks |
|------|--------|
| **spec · requirements** | Every criterion starts with WHEN/WHILE/IF/WHERE/THE SYSTEM · none uses `should` · `### Requirement N:` headers unique |
| **spec · design** | Boundary Map and File Structure Plan sections present · every requirement ID appears in the traceability table · `design.md` ≤ 1000 lines (else split the spec) |
| **spec · tasks** | Every leaf has `_Requirements:_` with real IDs · every `(P)` has a `_Boundary:_` that matches the design · no `(P)` pair shares a boundary |
| **skill · mcp · steering** | `SKILL.md` ≤ 500 lines / ~5k tokens, refs cited · mcp: exactly 1 transport (stdio **or** url) · steering: valid `inclusion`, fileMatch has a pattern |

**Exit codes:** `0` ok · `1` usage error · `2` validation failure. Scriptable in CI.

---

## Integration — native to Claude Code, no conversion layer

The workspace `csdd` writes **is** the layout Claude Code expects. `csdd init` bootstraps it and handles the wiring:

```
CLAUDE.md             # entry point + steering imports
.claude/steering/*.md # @-referenced from CLAUDE.md
.claude/agents/*.md   # sub-agents (implementer, code-reviewer, …)
.claude/skills/<n>/   # skill bundles
.claude/commands/     # slash commands (/csdd-setup-init, /csdd-setup-update, /csdd-commit)
.claude/hooks/        # deterministic automation
specs/<feature>/      # SDD contracts
.mcp.json             # MCP servers
```

Creating a steering automatically inserts `@.claude/steering/<name>` into a managed block of `CLAUDE.md` — idempotent, never clobbering manual edits.

**What the team gains:**
- **Zero friction with Claude Code.** Artifacts are read natively — no exporting or converting.
- **Review where we already work.** Specs and steering are text in a PR — diff, comment, approve.
- **Least privilege by default.** Sub-agents are born with `Read, Grep`; MCP with a restricted scope.
- **CI validates the contract.** A `csdd spec validate` in the pipeline blocks a broken spec before merge.

---

## 🔌 MCP server — drive csdd as native tools

Prefer your agent to call **tools** over shelling out to a terminal?
[`@protonspy/csdd-mcp`](mcp-server/) is an MCP server (stdio) that exposes the csdd
**development flow** as tools — `csdd_spec_generate`, `csdd_steering_create`,
`csdd_spec_approve`, … **27 in total.** It wraps the same CLI, so the contract is
intact: phase gates still block, the validator still runs, and **exit 2 surfaces
as a distinct "validation failed" result** the agent can branch on. Typed
parameters (enums for `artifact`/`phase`/`inclusion`) mean the agent picks valid
inputs and the server builds the argv — more precise than hand-written commands.

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

`csdd` is Claude Code-native, but the SDD artifacts aren't locked in. `csdd export`
converts the workspace to other agentic toolchains — a one-way, **additive** export
that lives alongside `.claude/` (nothing is overwritten in place):

```bash
npx @protonspy/csdd export kiro     # → .kiro/steering/*.md + .kiro/specs/<feature>/{requirements,design,tasks}.md
npx @protonspy/csdd export codex    # → AGENTS.md (CLAUDE.md + steering inlined) + .codex/config.toml (MCP)
npx @protonspy/csdd export kiro --out ./build --force
```

- **Kiro** — steering frontmatter (`inclusion: always|fileMatch|manual|auto`, `fileMatchPattern`) is already Kiro-compatible, so steering copies verbatim; specs copy their SDD markdown (`spec.json` is dropped — Kiro tracks phase state in-IDE).
- **Codex** — Codex has no `@`-import, so the managed steering block in `CLAUDE.md` is replaced by the steering inlined into `AGENTS.md`; `.mcp.json` becomes `[mcp_servers.*]` tables in `.codex/config.toml`.

---

## Getting started

```bash
# bootstrap a repo with baseline steering
npx @protonspy/csdd init --with-baseline

# take your first feature through to ready_for_implementation
npx @protonspy/csdd spec init my-feature
npx @protonspy/csdd spec generate my-feature --artifact requirements
```

**Takeaways:** The validator is your friend. The gate makes the decision *visible*. Contract before code — requirements → design → tasks, each approved by a human before the next. Always generate from a template; never hand-write frontmatter or `spec.json`. Least privilege everywhere.
