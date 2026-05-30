# csdd

**Claude Spec-Driven Development — as an executable contract.**

`csdd` is a single Go binary that turns the Spec-Driven Development (SDD) workflow for [Claude Code](https://claude.com/claude-code) from "good intentions in markdown" into a contract that is validated mechanically — for humans *and* AI agents.

| 5 | 1 | ~8.7k |
|---|---|-------|
| managed resources | binary, zero runtime deps | lines of Go |

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
csdd spec generate photo-albums --artifact requirements
```

### ⌨️ TUI — for humans

Interactive interface (Bubble Tea). Running `csdd` with no arguments opens wizards and an artifact browser. Same operations, same rules.

```bash
csdd          # no args → interactive TUI
```

> 🔑 **Core principle:** both surfaces call the **same operation helpers**. A single source of truth — what a human does in the TUI, an agent does identically via the CLI.

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
csdd spec generate albums --artifact design
✗ phase gate: 'requirements' must be
  approved before generating 'design'.

# the right path:
csdd spec approve albums --phase requirements
✓ requirements approved
```

---

## Feature lifecycle — from idea to ready-to-implement

```bash
# 1 · bootstrap (once per repo)
csdd init --with-baseline

# 2 · create the feature workspace
csdd spec init photo-albums

# 3 · requirements → edit in EARS → validate → approve
csdd spec generate photo-albums --artifact requirements
csdd spec validate photo-albums          # exit 2 = fix what it flags
csdd spec approve  photo-albums --phase requirements

# 4 · design (blocked until step 3 passes)  → 5 · tasks (same)
csdd spec generate photo-albums --artifact design   # ... validate, approve
csdd spec generate photo-albums --artifact tasks    # ... validate, approve

✓ spec.json: ready_for_implementation = true   # implementation can begin
```

> 💡 `csdd spec status <feature>` between any two steps: phase + approvals + validation issues on a single screen.

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

### 🔴 Skill `tdd-cycle`
- **One leaf task per invocation.** Takes the ID from `specs/<f>/tasks.md`; doesn't batch tasks "to save time".
- **RED fails for the right reason.** A compile error doesn't count — it cites the failure before moving on.
- **Never weakens a test** to make the suite pass. New behavior = new RED.

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

## Architecture — two surfaces, one core

```
                          main.go
            (no args → TUI · with args → CLI)
                             │
        ┌────────────────────┴────────────────────┐
     cmd/ · CLI                                tui/ · TUI
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
| `cmd/` | **CLI surface.** Dispatches `resource action`, flag parsing, 1 file per resource. Includes `CLAUDE.md` and `.gitignore` wiring. | The public contract — 100% of functionality, headless. |
| `tui/` | **Interactive front-end** (Bubble Tea): menu, wizards, artifact browser. | Calls the same helpers as `cmd`. No duplicated logic. |
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
.claude/agents/*.md   # sub-agents (Read, Grep…)
.claude/skills/<n>/   # skill bundles
.claude/commands/     # slash commands (/csdd-commit)
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

## Getting started

```bash
# build the binary
go build -o csdd .

# bootstrap a repo with baseline steering
csdd init --with-baseline

# take your first feature through to ready_for_implementation
csdd spec init my-feature
csdd spec generate my-feature --artifact requirements
```

**Takeaways:** The validator is your friend. The gate makes the decision *visible*. Contract before code — requirements → design → tasks, each approved by a human before the next. Always generate from a template; never hand-write frontmatter or `spec.json`. Least privilege everywhere.
