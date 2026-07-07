# Master Plan — csdd Knowledge Base (`csdd graph` + `csdd wiki`)

> **Audience:** the LLM agent that will implement this feature end-to-end.
> **Status:** approved plan, ready for implementation. Sections 4/5/6 are the pre-authored
> content of `requirements.md` / `design.md` / `tasks.md` for the csdd spec flow.
> **Companions:** `docs/graph/README.md` (schema doc), `docs/graph/graph.json` (hand-seeded fixture),
> `docs/graph/log.md`, `graphify/ANALISE-ARQUITETURA.md` (graphify internals study),
> `graphify/llm-wiki.md` (Karpathy's LLM-wiki pattern).
> **Supersedes:** `docs/plans/PLANO-graph-index.md` (Portuguese, graph-only scope).
> **Revision 2 (2026-07-06):** re-scoped after value review — spec lint (`analyze`) is the
> immediately-paying core; the Go code extractor is promoted into scope (M3); the full
> query engine and incremental builds are deferred to the scale milestone (M4), because
> they only pay off once source code makes the graph large.
> **Revision 3 (2026-07-06):** storage split by purpose — the compiled knowledge index
> moved to `docs/graph/` (the only generated subtree in `docs/`); `.csdd/` now holds
> operational state only (manifests, caches, incremental state); added R14 + task 16
> (update-manifest migration from `.claude/.csdd-manifest.json` with legacy fallback).
> **Revision 4 (2026-07-06):** `.csdd/` becomes the **workspace marker** (today it is
> `.claude/`), and `csdd init` scaffolds the **complete development layout** —
> `specs/`, `docs/plans|raw|wiki|graph/`, `.claude/`, `.csdd/` — added R15 + task 17.
> Agent integration added (R16): graph + wiki **skills** driving everything through
> `csdd`/`npx csdd`, **slash commands** (`/csdd-graph-*`, `/csdd-wiki-*`) for direct
> user control, and a managed **CLAUDE.md** section teaching the workflow moments.
> **Revision 5 (2026-07-06):** the **tech contract** (R17, task 18) — `docs/stack.md`
> pins stack/library decisions as a human-authored contract; manifests are indexed and
> diffed against it (undeclared/phantom/unrefined tech lints); a **stack skill**
> (Propose/Refine/Reconcile, human-decision gate) plus `/csdd-stack-*` commands; and
> CLAUDE.md gains the ask-first + refine-via-Context7/web protocol and the skill's
> invocation moments. Kills silent architectural decisions.

---

## 1. Context and vision

### 1.1 What we are building

csdd is a spec-driven-development CLI: a single static Go binary that scaffolds and
governs `specs/` (requirements → design → tasks), `.claude/` (steering, skills, agents),
`CLAUDE.md`, and `.mcp.json`. Its reason to exist is **traceability** — every task traces
to a requirement, every component to a criterion.

This plan adds csdd's **knowledge base**, made of two cooperating halves:

1. **`csdd graph`** — **a structured brain for fast, token-cheap, precise consultation
   of the project's source code.** That sentence is the mission — and its point is that
   **the LLM should not need to read the whole codebase to understand it**. Instead of
   consuming thousands of lines to build a mental map, the LLM queries the graph and
   gets the structure directly: what exists, what connects to what, where the hubs are,
   which patterns repeat. Understanding comes from traversing relationships (a few
   hundred tokens), not from re-reading source (hundreds of thousands). Concretely: a
   deterministic, persistent knowledge graph (`docs/graph/graph.json`, NetworkX node-link
   format) — retrieval in milliseconds, answers scoped to a token budget, edges that
   are facts (extracted, not guessed), patterns surfaced by structure (god nodes,
   communities, dependency chains). It indexes every artifact csdd governs, the
   documentation under `docs/`, and — from M3, the milestone where the mission is
   fulfilled — the project's **Go source code**. Along the way it answers the question
   csdd exists for: *"what connects this requirement to the code, and what breaks if I
   change this task?"* — queryable traceability + mechanical gap detection across the
   whole spec.

2. **`csdd wiki`** — a standardized instantiation of **Karpathy's LLM-wiki pattern**
   (`graphify/llm-wiki.md`): a pipeline that turns **raw sources** the user drops into
   `docs/raw/` (immutable — articles, notes, transcripts, clippings) into a **solid,
   structured, LLM-consumable knowledge base** under `docs/wiki/` — a persistent,
   interlinked markdown wiki. **Content is authored by LLM sessions** (via a skill csdd
   installs); **structure is scaffolded and linted by the CLI** (deterministic
   operations only). The wiki is NOT about the specs — it is a general knowledge-base
   layer over `docs/`. It is also just another corpus for the graph engine — one
   knowledge base, two faces.

### 1.2 The synthesis of three sources

| Source | What we take |
|--------|--------------|
| **graphify** | The *structural technique*: `{nodes,edges}` contract, single canonical ID, confidence tiers, query engine with hub-gating + token budget, incremental replace-per-source. |
| **Karpathy LLM-wiki** | The *lifecycle*: a persistent artifact "compiled once, kept current"; Ingest/Query/Lint operations; `index.md` + `log.md`; a co-evolved schema doc. |
| **csdd** | The *domain and discipline*: SDD traceability as a first-class citizen; **deterministic extraction** (no LLM in the binary — honest, reviewable, mechanically validated); CLI as the sole author of generated data. |

### 1.3 The discovery that anchors the approach

csdd artifacts **already carry explicit traceability annotations** (verified in
`internal/templater/templates/spec/*.tmpl`):

- `tasks.md`: `_Requirements: 1.1, 1.2_`, `_Boundary: X_`, `_Depends: 3.1_`, `(P)` marker, checkboxes.
- `design.md`: a **Requirements Traceability** table, `**Requirements**: 1.1, 1.2` per component, `**Dependencies**` (Inbound/Outbound/External).
- `requirements.md`: `### Requirement N` headings + numbered EARS criteria `N.M`.
- wiki pages (this plan): YAML frontmatter + `[[wikilinks]]` — both explicit.

**Consequence:** ~95% of edges are `EXTRACTED` (explicit links). The extractor is
deterministic annotation parsing — **pure Go, no tree-sitter, no LLM, no cgo**. The
distribution constraint (`CGO_ENABLED=0`, 6 release targets, offline single binary)
stays intact. Source code enters later through the same seam using `go/parser`
(stdlib, cgo-free).

### 1.4 Value thesis (why this build order)

The prime objective is the **code brain**: the LLM understands the codebase by querying
its structure, not by reading all of it. Fast (pre-built index, no re-derivation per
question), token-cheap (budgeted subgraph answers instead of file dumps), precise
(edges are extracted facts with confidence labels, not similarity guesses) — and,
above all, **comprehension without exhaustive reading**: architecture questions ("what
depends on X?", "what implements Y?", "where are the hubs?") answered by traversal, and
recurring patterns made visible by the graph's shape instead of discovered by scanning
files. The build order is how we get there without a leap of faith:

- **`csdd graph analyze` pays for itself immediately.** Traceability annotations are
  write-only today — the templates demand them, nothing consumes them. A mechanical
  lint of the SDD contract (untested criteria, orphan tasks, unimplemented components,
  pending references, dependency cycles) is csdd's core promise made checkable. It ships
  first (M1) and validates the whole substrate (model, IDs, extraction, assembly) on a
  small corpus.
- **The code graph fulfills the mission.** A knowledge graph of dozens of markdown
  artifacts is readable without a graph; the *brain* only exists when source code joins
  it (M3). The Go extractor is therefore in scope, not a vague future.
- **Scale machinery follows the corpus, it does not precede it.** The full query engine
  (IDF, trigrams, hub-gating, **token budgets** — the "cheap" in the mission) and
  incremental builds (hash-skip, replace-per-source — the "fast" as the codebase grows)
  were designed by graphify for thousands of nodes. At M1 corpus size a full rebuild is
  milliseconds and label matching answers queries. Both are specified now (§5.11, §5.9)
  and implemented in M4, immediately after the code corpus makes them matter.

---

## 2. Storage convention (decided, project-wide — revision 3: split by purpose)

| Directory | Role | Author |
|-----------|------|--------|
| **`docs/`** | The **knowledge base** — everything committed and shared: `docs/plans/` (plans), `docs/stack.md` (**the tech contract** — human-authored stack decisions, R17), `docs/raw/` (**immutable raw sources**, user-curated dropzone — read, never modified), `docs/wiki/` (**LLM-authored structured knowledge**), and **`docs/graph/`** — the compiled knowledge index (`graph.json`, `graph.html`, build `log.md`), the **only generated subtree in `docs/`**, marked read-only by its README. | Humans and LLM sessions author prose; the CLI writes **only** `docs/graph/` and never touches `raw/`. |
| **`.csdd/`** | csdd's **operational state** — the csdd analog of `.git/`: hash manifests (the `csdd update` manifest migrates here from `.claude/.csdd-manifest.json`, R14), incremental build state and caches (M4). No knowledge artifacts. **Its presence at the project root is the workspace marker** (R15): `csdd init` creates it; workspace-requiring commands validate it. | **The CLI only.** `cache/`/`state.json` are local (gitignored) and regenerable; manifests are committed. |

Ownership is assigned **by purpose**: knowledge (committed, shared, reviewed in PRs)
lives in `docs/`; disposable machine state lives in `.csdd/`. The git story is trivial —
commit all of `docs/`, ignore `.csdd/` except manifests.

---

## 3. Design principles (inviolable)

1. **100% pure Go, cgo-free.** No new heavy dependency. `norm.NFKC` comes from
   `golang.org/x/text` (already in `go.sum`); code parsing uses stdlib `go/parser`.
2. **The CLI never authors prose.** It owns `.csdd/` (operational state) and exactly
   one generated subtree of the knowledge base — `docs/graph/` — which humans and LLMs
   treat as read-only. Everything else under `docs/` is human/LLM-authored: the CLI may
   scaffold and lint it, never write content, and never touch `docs/raw/`. TUI/web are
   read-only views.
3. **Deterministic and idempotent.** Same input → byte-identical `docs/graph/graph.json`
   (clean git diffs). No LLM calls, no wall-clock in output (except the build log).
4. **Aggressive reuse.** `internal/manifest`, `internal/session/changedetect.go`,
   `internal/frontmatter`, `internal/paths`, `internal/textutil`, `internal/templater`,
   `internal/web` embed patterns.
5. **Honesty over recall.** Structural ambiguity → omission; `AMBIGUOUS` only where real
   uncertainty exists (for the human gate). Unresolved references are **reported, never
   silently dropped** — in csdd the gap *is* the product.
6. **The graph engine is corpus-agnostic.** All corpora enter through one `Extractor`
   contract (§5.4): spec artifacts, `.claude/`, the wiki (M1), Go source (M3). Nothing
   downstream of extraction may know which corpus a fragment came from.
7. **Scale machinery follows the corpus.** Features whose value depends on graph size
   (full query engine, incremental builds) land only when the corpus that justifies
   them does (M4, after code indexing). Until then, simpler mechanisms with identical
   external behavior.

---

## 4. Requirements (content of `requirements.md`)

EARS format (WHEN/IF/WHILE/WHERE/THE SYSTEM SHALL), as the csdd template requires.
Requirement IDs in annotations are always **explicit comma-separated lists** — range
shorthand (`3.1–3.5`) is forbidden because the parser does not resolve it.

### Requirement 1: Index workspace artifacts into a graph
**Objective:** As a maintainer of a csdd workspace, I want artifacts indexed into a
graph, so I can navigate relationships instead of re-reading files.
**Acceptance Criteria**
1. WHEN `csdd graph build` runs THEN the system SHALL write `docs/graph/graph.json`
   (node-link) covering all of `specs/`, `.claude/`, `CLAUDE.md`, `.mcp.json`, and
   `docs/` (when present).
2. THE SYSTEM SHALL emit one node per artifact/entity with
   `{id, label, file_type, source_file, source_location}`.
3. WHERE an artifact carries an explicit traceability annotation THE SYSTEM SHALL mark
   the edge `EXTRACTED` with `confidence_score` 1.0.
4. THE SYSTEM SHALL generate deterministic node IDs (NFKC → non-word→`_` → casefold),
   identical across rebuilds.

### Requirement 2: Query the graph (query / path / explain — v1 surface)
**Objective:** As an agent or human, I want to look up nodes and trace paths, to find
what connects to what without re-reading files.
**Acceptance Criteria**
1. WHEN `csdd graph query "<terms>"` runs THEN the system SHALL return the matching
   nodes and their immediate neighborhood, matched by label tiers
   (exact > prefix > substring), in deterministic order.
2. WHEN `csdd graph path A B` runs THEN the system SHALL return the shortest path with
   each edge's real direction, or an error if A resolves to the same node as B.
3. WHEN `csdd graph explain <label>` runs THEN the system SHALL list the node plus its
   connections ordered by neighbor degree.

### Requirement 3: Surface traceability gaps (the "Lint" operation for specs)
**Objective:** As a reviewer, I want the graph to point out contract violations across
the whole spec, so the human gate acts before merge.
**Acceptance Criteria**
1. WHEN `csdd graph analyze` runs THEN the system SHALL list every acceptance criterion
   with no task realizing it ("untested requirement").
2. THE SYSTEM SHALL list every task without a `_Requirements` annotation ("orphan task").
3. THE SYSTEM SHALL list every design component with no task pointing at it via
   `_Boundary` ("unimplemented component").
4. IF a `_Depends`/traceability annotation references a nonexistent ID THEN the system
   SHALL report it as a "pending reference".
5. IF `depends_on` edges form a cycle THEN the system SHALL report it.

### Requirement 4: Deterministic, logged rebuilds
**Objective:** As a user, I want every build to be reproducible and recorded, so the
graph is trustworthy and its history auditable.
**Acceptance Criteria**
1. WHEN `csdd graph build` runs THEN the system SHALL fully rebuild the graph from the
   corpus, producing byte-identical output for an unchanged corpus. (v1 has no
   incremental mode; at v1 corpus size a full rebuild is imperceptible — see R13.)
2. THE SYSTEM SHALL append one line to `docs/graph/log.md` on every build.

### Requirement 5: Pure Go / cgo-free
**Objective:** As the distribution maintainer, I want the feature to preserve the static
binary model, so all 6 targets keep compiling without a C toolchain.
**Acceptance Criteria**
1. THE SYSTEM SHALL compile with `CGO_ENABLED=0` for the 6 release targets (`make dist`).
2. THE SYSTEM SHALL NOT introduce a dependency requiring cgo or an external toolchain.

### Requirement 6: CLI + web surfaces
**Objective:** As a user, I want the graph via CLI and visualized in the dashboard, to
consume it however fits.
**Acceptance Criteria**
1. THE SYSTEM SHALL expose `csdd graph build|query|path|explain|analyze|export` and
   `csdd wiki init|lint`.
2. WHEN `csdd graph export` runs THEN the system SHALL write a self-contained
   `docs/graph/graph.html` (vis-network inlined, no external asset).
3. WHERE the web dashboard is active THE SYSTEM SHALL serve a "Graph" tab reading
   `docs/graph/graph.json`, through the same read-only hardening path as existing routes.

### Requirement 7: Storage layout — knowledge in `docs/`, state in `.csdd/`
**Objective:** As a user, I want generated knowledge in one predictable committed place
and machine state out of my way, so PRs review knowledge and state stays disposable.
**Acceptance Criteria**
1. THE SYSTEM SHALL write generated knowledge artifacts (`graph.json`, `graph.html`,
   the build `log.md`) under `docs/graph/` and SHALL NOT write generated files anywhere
   else in `docs/`.
2. THE SYSTEM SHALL keep `docs/graph/graph.json` byte-stable when the corpus is
   unchanged (total deterministic ordering of nodes and edges).
3. THE SYSTEM SHALL treat paths as workspace-root-relative (portable across machines).
4. THE SYSTEM SHALL keep operational state (caches, incremental state, hash manifests)
   under `.csdd/`, with everything except committed manifests regenerable from scratch.

### Requirement 8: Interoperability with the graphify format
**Objective:** As an ecosystem user, I want the graph loadable by NetworkX/graphify
tooling, to reuse visualizers and loaders.
**Acceptance Criteria**
1. THE SYSTEM SHALL emit node-link JSON with keys `{directed, multigraph, graph, nodes, links}`.
2. THE SYSTEM SHALL use confidence labels `EXTRACTED|INFERRED|AMBIGUOUS`.

### Requirement 9: Wiki scaffolding (`csdd wiki init`)
**Objective:** As a user adopting the LLM-wiki pattern, I want the CLI to stand up the
standard structure and teach my LLM how to maintain it, so every csdd workspace gets the
same disciplined wiki.
**Acceptance Criteria**
1. WHEN `csdd wiki init` runs THEN the system SHALL create `docs/raw/` (the immutable
   source dropzone) and `docs/wiki/` containing `index.md` (content catalog), `log.md`
   (append-only chronology with the `## [YYYY-MM-DD] <op> | <title>` entry format
   documented inline), and `pages/`.
2. THE SYSTEM SHALL install the wiki **schema doc** (conventions, the raw→wiki pipeline,
   and Ingest/Query/Maintain workflows for the LLM) through csdd's existing
   steering/skill mechanisms (`internal/templater`): a steering rule plus a `wiki`
   skill under `.claude/skills/`.
3. THE SYSTEM SHALL NOT author wiki content and SHALL NOT modify anything under
   `docs/raw/` — pages are written by LLM sessions following the installed skill; raw
   sources are read-only for everyone but the user.
4. WHERE `docs/wiki/` already exists THE SYSTEM SHALL leave existing files untouched and
   only add missing scaffold files (idempotent; `--force` required to overwrite).

### Requirement 10: Wiki lint (`csdd wiki lint`)
**Objective:** As a wiki owner, I want mechanical health checks, so the wiki stays
navigable as it grows (the deterministic half of Karpathy's "Lint").
**Acceptance Criteria**
1. WHEN `csdd wiki lint` runs THEN the system SHALL report every broken `[[wikilink]]`
   (target page absent).
2. THE SYSTEM SHALL report orphan pages (no inbound `links_to` edge and not listed in
   `index.md`).
3. THE SYSTEM SHALL report index desync in both directions: pages missing from
   `index.md`, and `index.md` entries pointing at missing files.
4. THE SYSTEM SHALL report `log.md` entries that do not match the documented
   `## [YYYY-MM-DD] <op> | <title>` format.
5. THE SYSTEM SHALL report every raw source under `docs/raw/` with no inbound
   `derived_from` edge ("unprocessed source" — dropped but never ingested).
6. IF any violation is found THEN the system SHALL exit non-zero (CI-gateable).

### Requirement 11: The wiki is a graph corpus
**Objective:** As a knowledge-base user, I want wiki pages inside the same graph, so
queries and analysis span specs and documentation as one knowledge base.
**Acceptance Criteria**
1. WHEN `csdd graph build` runs and `docs/` exists THEN the system SHALL index wiki
   pages as `wiki_page` nodes carrying frontmatter attributes (tags, dates) and every
   file under `docs/raw/` as an opaque `raw_source` node (path only, content unparsed).
2. THE SYSTEM SHALL emit `links_to` edges (`EXTRACTED`, 1.0) for `[[wikilinks]]`,
   `derived_from` edges (wiki_page → raw_source) for sources cited in page frontmatter
   (`sources:` list) or links to `raw/` paths, and `references`/`reuses` edges for other
   repo paths cited in pages.
3. THE SYSTEM SHALL compute `csdd wiki lint` findings from this same graph (single
   source of truth — lint is `analyze` filtered to the wiki corpus).

### Requirement 12: Index Go source code (M3 — the code graph)
**Objective:** As a developer, I want the project's Go source in the same graph, so
traceability reaches real code and the graph grows with the codebase.
**Acceptance Criteria**
1. WHEN `csdd graph build` runs in a workspace containing `.go` files THEN the system
   SHALL emit `code` nodes for packages, files, and top-level declarations (functions,
   methods, types), extracted with stdlib `go/parser` (syntax-only, cgo-free).
2. THE SYSTEM SHALL emit containment edges (package → file → declaration) and
   package-import edges, all `EXTRACTED` with score 1.0.
3. WHERE an artifact or wiki page cites a path that resolves to an indexed Go file THE
   SYSTEM SHALL link it to the `code` node (`reuses`), replacing the placeholder
   `code_ref`.
4. THE SYSTEM SHALL exclude `vendor/`, hidden directories, and `testdata/` fixtures
   from code indexing by default.

### Requirement 13: Scale engine (M4 — earned by the code corpus)
**Objective:** As a user of a now-large graph, I want retrieval-grade queries and cheap
rebuilds, because the code corpus made full scans and label matching insufficient.
**Acceptance Criteria**
1. WHEN `csdd graph query` runs THEN the system SHALL score nodes by tiers weighted by
   IDF, seed at least one node per query term, not expand traversal *through*
   super-hubs (degree > p99, configurable floor), and render within a `--budget` token
   budget (default 2000).
2. WHEN an artifact is unchanged (equal content hash) THEN the system SHALL skip its
   re-extraction; changed sources SHALL have their entire prior contribution replaced
   (replace-per-source); deleted sources SHALL be pruned.
3. WHEN `csdd graph build` runs THEN incremental SHALL be the default and `--full`
   SHALL force a complete rebuild, with output byte-identical to a full rebuild.

### Requirement 14: `.csdd/` state consolidation (independent of milestones)
**Objective:** As a csdd user, I want csdd's operational files consolidated in
`.csdd/`, so `.claude/` stays purely mine.
**Acceptance Criteria**
1. WHEN `csdd update` needs its hash manifest THEN the system SHALL read
   `.csdd/manifest.json`, falling back to the legacy `.claude/.csdd-manifest.json` when
   the new path is absent, and SHALL write to the new path on the next save
   (transparent migration).
2. THE SYSTEM SHALL behave identically for workspaces that only have the legacy
   manifest (no forced migration, no breakage).

### Requirement 15: `csdd init` scaffolds the complete workspace; `.csdd/` is the marker
**Objective:** As a user starting a project, I want one command to assemble the whole
development setup, and one unambiguous signal that a project is csdd-initialized.
**Acceptance Criteria**
1. WHEN `csdd init` runs THEN the system SHALL scaffold the complete development
   layout: the existing `.claude/` core, `specs/`, `docs/plans/`, `docs/raw/`,
   `docs/wiki/` (index/log/pages via the §5.13 wiki scaffold), `docs/graph/` (with its
   read-only README), and `.csdd/` (the state dir) — idempotent, adding only missing
   pieces (`--force` to overwrite).
2. THE SYSTEM SHALL treat the presence of `.csdd/` at the project root as **the
   workspace marker**: commands that require an initialized workspace (`graph`, `wiki`)
   SHALL fail with actionable guidance ("run `csdd init`") when it is absent.
3. WHERE a legacy workspace has `.claude/` but no `.csdd/` THE SYSTEM SHALL say so
   explicitly and instruct that re-running `csdd init` upgrades it in place (no data
   loss, nothing overwritten).

### Requirement 16: Agent integration — skills + CLAUDE.md workflow moments
**Objective:** As an agent working in a csdd workspace, I want installed skills that
know how to control the graph and the wiki through the CLI, and a CLAUDE.md that tells
me *when* to invoke them, so the knowledge base stays current inside the normal
workflow without ad-hoc prompting.
**Acceptance Criteria**
1. THE SYSTEM SHALL install a **graph skill** (`.claude/skills/graph/SKILL.md`) that
   controls the graph exclusively via the CLI (`csdd graph …`, or `npx csdd graph …`
   when not installed globally): when to `build` (after editing specs/tasks/code),
   `query`/`explain`/`path` (before grepping or reading code — the brain-first habit),
   and `analyze` (before review/commit), including how to consume `--json` output.
2. THE SYSTEM SHALL install the **wiki skill** (§5.13) that owns content
   creation/maintenance knowledge (Ingest/Query/Maintain) and uses
   `csdd wiki init|lint` for the deterministic parts.
3. WHEN `csdd init` runs THEN the system SHALL wire the managed **CLAUDE.md** section
   with the knowledge-base workflow moments: consult the graph before searching;
   rebuild after changing specs/tasks/code; run the wiki Ingest workflow after a file
   lands in `docs/raw/`; run `csdd graph analyze --strict` and `csdd wiki lint` before
   commit/PR.
4. Skills and LLM sessions SHALL NOT write `docs/graph/` or `.csdd/` directly — the CLI
   is the single mutation path for generated artifacts.
5. THE SYSTEM SHALL install **user-invocable slash commands** under `.claude/commands/`
   (reusing the existing mechanism, e.g. `/csdd-commit`) so the user can drive the
   knowledge base directly: `/csdd-graph-query`, `/csdd-graph-path`,
   `/csdd-graph-explain`, `/csdd-graph-analyze`, `/csdd-wiki-ingest`,
   `/csdd-wiki-query`, `/csdd-wiki-lint`, `/csdd-stack-propose`, `/csdd-stack-refine` —
   thin wrappers that route to the skills/CLI.

### Requirement 17: The tech contract (`docs/stack.md`) — stack decisions are the human's
**Objective:** As the project owner, I want stack/library/framework decisions written
as a contract the agent must follow, so architectural choices stay mine, libraries are
used current and correctly, and silent tech decisions (e.g. assuming Redis as broker
when the decision was RabbitMQ) stop happening.
**Acceptance Criteria**
1. WHEN `csdd init` runs THEN the system SHALL scaffold `docs/stack.md` with three
   sections: **Decided** (a parseable table `| Domain | Choice | Version | Why | Refs |`),
   **Rules** (the decision protocol, pre-filled — see 17.4), and **Open questions**.
   The scaffold SHALL NOT prefill any technology choice — the human authors the
   contract; the CLI provides structure only.
2. THE SYSTEM SHALL index every Decided row as a `tech` node, and every dependency
   declared in manifests (`go.mod`, `package.json`, `pyproject.toml`/`requirements.txt`)
   as a `tech` node with a `uses_tech` edge from the manifest (`EXTRACTED`), matched to
   contract entries by normalized name.
3. WHEN `csdd graph analyze` runs THEN the system SHALL report: dependencies present in
   manifests but absent from the contract ("**undeclared tech decision**" — the
   Celery+Redis catcher); contract entries with no detected usage ("phantom tech");
   and contract entries missing Version or Refs ("unrefined tech").
4. THE SYSTEM SHALL include the contract protocol in the managed CLAUDE.md section
   (R16.3): *the contract is law; any technology not listed is an **open decision** —
   propose options with trade-offs and ask before adopting; before the first use of a
   listed library in a feature, refine against current documentation (Context7 MCP when
   configured, else web search) and file the findings as a wiki page linked from the
   contract's Refs column.*
5. WHERE the workspace has no `docs/stack.md` THE SYSTEM SHALL treat this as a lint
   finding ("no tech contract"), not an error — adoption is incremental.
6. THE SYSTEM SHALL install a **stack skill** (`.claude/skills/stack/SKILL.md`) owning
   the contract workflows: **Propose** (a new tech need appears → present options with
   trade-offs → ask the human → amend the Decided table only after approval),
   **Refine** (run the currency check via Context7 MCP when configured, else web
   search → file findings as a wiki page → link it in the entry's Refs), and
   **Reconcile** (react to `analyze` tech findings: undeclared → Propose it or remove
   the dependency; phantom → confirm intent or drop the entry; unrefined → run Refine).
   The skill SHALL NOT edit manifests or adopt technology without an explicit human
   decision.
7. THE SYSTEM SHALL include the stack-skill invocation moments in the managed CLAUDE.md
   section: invoke it whenever a new dependency or technology is about to enter the
   project, whenever `analyze` reports tech findings, and whenever the user mentions
   adopting or changing technology.

---

## 5. Design (content of `design.md`)

### 5.1 Overview
- **Purpose:** a structured brain for fast, token-cheap, precise consultation of the
  project — a deterministic graph over csdd artifacts, documentation, and (M3) Go
  source code — plus a standardized LLM-wiki lifecycle turning `docs/raw/` into
  structured knowledge (CLI scaffolds/lints; LLM authors).
- **Users:** agents (query, wiki ingest), reviewers (analyze/lint/web), csdd itself (gates).
- **Impact:** new package `internal/graph`, new templates, two new subcommands, one web
  tab. Zero changes to existing artifact formats.

### 5.2 Goals / Non-Goals
**Goals:** mechanical spec lint (the core product); queryable traceability; one graph
across specs, docs, and code; pure Go; clean git diffs; wiki scaffold + lint; scale
machinery when — and only when — the corpus earns it.
**Non-Goals (all milestones):** LLM calls inside the binary (wiki *content* belongs to
LLM sessions via the skill); embeddings; MinHash dedup; hyperedges; Leiden communities
(per-spec grouping is natural at this scale). **Non-goals for M1–M2 specifically:**
code indexing (M3), IDF/hub-gating/budget query engine and incremental builds (M4).
Polyglot code indexing (tree-sitter-WASM over `wazero`) stays post-v1.

### 5.3 Existing architecture analysis (verified reuse map)
| Need | Already exists in | Use |
|---|---|---|
| Per-file content hash | `internal/manifest` (SHA256, relative paths; file lives at `.claude/.csdd-manifest.json` today, migrates to `.csdd/manifest.json` — task 16) | M4 incremental cache fingerprint |
| size+mtime change detection | `internal/session/changedetect.go` (Snapshot) | M4 incremental fast path |
| Frontmatter parsing | `internal/frontmatter` | skill/agent/steering/wiki-page nodes |
| Workspace paths / roots | `internal/paths`, `internal/session` (csddRoots) | corpus discovery |
| Text hash/normalization | `internal/textutil` | ID helpers |
| NFKC Unicode | `golang.org/x/text/unicode/norm` (already in go.sum) | ID normalization (no new dep) |
| Go syntax parsing | stdlib `go/parser`, `go/ast` | M3 code extractor (no new dep) |
| Asset embedding | `internal/web` (`//go:embed`) | `graph.html` template |
| Template installation | `internal/templater` (spec/skill/agents/rules templates) | `wiki init` scaffold + skill |
| CLI registration | `internal/cli/cli.go` `Run()` switch (stdlib `flag`) | `graph` / `wiki` subcommands |

### 5.4 The Extractor contract (first-class)

Everything enters the graph through one seam. Nothing downstream may branch on corpus.

```go
// A Source is one file of a corpus, already read and root-relative.
type Source struct {
    Path    string // workspace-root-relative, forward slashes
    Content []byte
}

// A Fragment is one extractor's partial contribution: nodes plus edges whose
// endpoints may still be symbolic (e.g. "requirement 1.1 of spec X") until
// Assemble reconciles them against the (spec, N, M) index.
type Fragment struct {
    Nodes []Node
    Edges []Edge
}

type Extractor interface {
    // Matches reports whether this extractor handles the given corpus file.
    Matches(path string) bool
    // Extract parses one source into fragments. Deterministic; no I/O.
    Extract(src Source) ([]Fragment, error)
}
```

**M1 extractors:** `specExtractor` (requirements/design/tasks/spec.json),
`claudeExtractor` (steering, skills, agents, `.mcp.json`, `CLAUDE.md`),
`wikiExtractor` (`docs/**/*.md`, wiki-aware for `docs/wiki/`).
**M3 extractor:** `goExtractor` (stdlib `go/parser`, syntax-only) for `code` nodes.
Adding it must not touch Assemble/Query/Export — that is the acceptance test of this
contract. **Post-v1:** tree-sitter-WASM over `wazero` for polyglot.

### 5.5 The graph model (canonical vocabulary — single source of truth)

`docs/graph/README.md` and the seed `graph.json` mirror this section; if this table changes,
they change in the same commit.

**Node types** (`file_type`): `spec` · `requirement` · `criterion` · `design`
(component) · `interface` · `flow` · `task` · `steering` · `skill` · `agent` · `mcp` ·
`code_ref` (a path cited by an artifact) · `wiki_page` · `raw_source` (an immutable file
under `docs/raw/`, indexed opaquely) · `tech` (a stack decision from `docs/stack.md` or
a dependency declared in a manifest) · `code` (a real source symbol, emitted once code
indexing lands in M3).

```go
type Node struct {
    ID, Label, FileType, SourceFile, SourceLocation string
    Attrs map[string]any // status(done|pending), parallel(bool), phase, tags, ...
}
type Edge struct {
    Source, Target, Relation, Confidence string
    ConfidenceScore float64
    SourceFile string
}
type Graph struct {
    Directed, Multigraph bool
    Meta  map[string]any `json:"graph"`
    Nodes []Node         `json:"nodes"`
    Links []Edge         `json:"links"`
}
```

**Edge relations** and their extraction source (EXTRACTED unless noted):

| Relation | From → To | Source |
|---|---|---|
| `owns` | spec → requirement/design/task | `specs/<name>/` structure |
| `has_criterion` | requirement → criterion | `### Requirement N` → item `N.M` |
| `realizes` | task → criterion | `_Requirements: 1.1_` in tasks.md |
| `traced_to` | criterion → design | Requirements Traceability table row |
| `implements` | design → interface | Interfaces column / component contract |
| `exercises` | criterion → flow | Flows column |
| `refined_by` | criterion → design | `**Requirements**: 1.1` in a component |
| `in_boundary` | task → design | `_Boundary: X_` |
| `depends_on` | task → task | `_Depends: 3.1_` |
| `component_dep` | design → design | `**Dependencies**` Inbound/Outbound |
| `references` | task/design/wiki_page → code_ref | cited path that does **not** exist on disk (planned) |
| `reuses` | task/design/wiki_page → code_ref/code | cited path that **does** exist on disk (deterministic check; targets `code` once M3 lands) |
| `governs` | steering → spec | steering scope (INFERRED) |
| `related_to` | skill/agent/mcp ↔ * | markdown links / frontmatter (INFERRED) |
| `links_to` | wiki_page → wiki_page | `[[wikilink]]` |
| `derived_from` | wiki_page → raw_source | frontmatter `sources:` list / links to `raw/` paths |
| `uses_tech` | code_ref (manifest) / design → tech | dependency manifests (EXTRACTED); component `**Dependencies**: External` (INFERRED) |
| `contains` | code → code | package → file → declaration (M3) |
| `imports` | code → code | package import (M3) |

**Confidence:** `EXTRACTED` (explicit annotation or exact cited path, score 1.0) ·
`INFERRED` (resolved by name/path matching, discrete 0.55–0.95) · `AMBIGUOUS`
(uncertain, 0.1–0.3, surfaced for the human gate). Structural ambiguity that is not an
annotation → **omission**, not a noisy edge.

### 5.6 Extraction rules — artifacts and wiki (the parser spec, deterministic)

- **spec.json** → `spec` node (attrs: phase, development_flow, approvals); `owns`
  everything under the spec folder.
- **requirements.md** → each `### Requirement N: Title` creates `requirement`; each item
  under `**Acceptance Criteria**` becomes `criterion` with ID `crit_<spec>_<N>_<M>` and a
  `has_criterion` edge.
- **design.md** →
  - `## Requirements Traceability` table: each row `| N.M | Comp | iface() | flow |` →
    `traced_to`, `implements`, `exercises`. One criterion per row; comma-separated lists
    within a cell are split; **range shorthand is a parse error** surfaced by `analyze`.
  - `### Comp` under `## Components and Interfaces` → `design`; `**Requirements**: …` →
    `refined_by`; `**Dependencies**` → `component_dep`.
  - `## File Structure Plan` (```text block) → `code_ref` per path (`reuses` if the path
    exists on disk, else `references`).
- **tasks.md** → each `- [ ] N[.M]. Title` → `task` (attr `status` from `[x]`,
  `parallel` from `(P)`); annotations `_Requirements:` → `realizes`, `_Boundary:` →
  `in_boundary`, `_Depends:` → `depends_on`. IDs are comma-separated `N.M` lists only.
- **.claude/** → `steering/rules`, `skills/**/SKILL.md`, `agents/*.md`, `.mcp.json` →
  respective nodes via `internal/frontmatter`; internal markdown links → `related_to`.
- **docs/** → every `*.md` outside `docs/raw/` becomes `wiki_page` (frontmatter →
  attrs). `[[wikilink]]` (regex `\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]`, target normalized
  with `NormalizeID`) → `links_to`. Frontmatter `sources:` entries and links to `raw/`
  paths → `derived_from`. Other markdown links to repo paths → `reuses`/`references`.
  Pages under `docs/wiki/pages/` additionally participate in wiki lint (index/log
  checks).
- **docs/raw/** → every file (any extension) becomes an opaque `raw_source` node —
  path only, content never parsed (raw is immutable input, not graph material). Its
  purpose in the graph is provenance (`derived_from` targets) and the "unprocessed
  source" lint.
- **docs/stack.md** → each row of the **Decided** table → `tech` node
  (attrs: domain, version, refs, `status=decided`); Rules/Open sections are prose, not
  parsed. **Dependency manifests** (`go.mod`, `package.json`,
  `pyproject.toml`/`requirements.txt`) → the manifest becomes a `code_ref` node and
  each declared dependency a `tech` node (`status=used`) with a `uses_tech` edge
  (`EXTRACTED`). Contract↔usage matching by `NormalizeID(name)`; the set differences
  feed the three tech lints (R17.3).

**Reference resolution:** numeric annotation IDs (`1.1`, `3.1`) resolve to canonical
criterion/task nodes **at build time** via a `(spec, N, M) → id` index. A reference that
does not resolve becomes a pending-reference finding in `analyze` — never silently
dropped (the inverse of graphify's behavior; here the gap is the product).

### 5.7 Extraction rules — Go source (M3)

`extract_go.go`, stdlib `go/parser` in `parser.ParseFile` mode (syntax-only — no
`go/types`, no module resolution, so no network and no toolchain dependency):

- One `code` node per **package** (dir-level), per **file**, and per top-level
  **declaration** (func, method with receiver in the label, type, exported var/const
  blocks). IDs: `MakeID("go", pkgPath, name)`. `source_location` = `file:line`.
- `contains` edges: package → file → declaration. `imports` edges: package → package
  (import specs; std-lib imports become nodes only if `--include-std`, default off).
- Excluded by default: `vendor/`, hidden dirs, `testdata/` (they are fixtures, not
  project truth); `_test.go` files are included but tagged `attrs.test=true`.
- **Resolution pass:** existing `code_ref` nodes whose path matches an indexed Go file
  are replaced by edges to the real `code` file node (upgrades artifact→code
  traceability from placeholder to real). This pass lives in Assemble, keyed by path —
  it must not special-case the Go extractor (principle 6).
- Call graphs and cross-package symbol resolution need `go/types`; explicitly out of
  scope for M3 (revisit post-v1 if a use case demands it).

### 5.8 Canonical IDs (port of `graphify/ids.py`)
```go
func NormalizeID(s string) string {
    s = norm.NFKC.String(s)                 // golang.org/x/text/unicode/norm
    s = reNonWord.ReplaceAllString(s, "_")  // [^\p{L}\p{N}]+ → _ (keeps CJK/accents)
    s = reUnderscores.ReplaceAllString(s, "_")
    return strings.ToLower(strings.Trim(s, "_")) // pragmatic casefold, v1
}
func MakeID(parts ...string) string // join with _, normalize
```
Idempotent. Single package, imported by extract and build — graphify's lesson #1.

### 5.9 Assemble / build
Fragments → `Graph` (DiGraph from the start). Node dedup by ID (last-writer-wins), edge
dedup by `(source, target, relation)`. Annotation IDs reconciled via the `(spec,N,M)`
index; cited paths reconciled against indexed code files (M3, §5.7). Total deterministic
ordering of nodes and edges before serialization (stable git diffs, graphify scar #1090).

**Build strategy:** M1–M3 builds are always **full rebuilds** — at this corpus size they
are milliseconds, and full rebuild is trivially deterministic (deleted files simply
vanish). M4 adds incremental (manifest hash-skip + replace-per-source + prune, reusing
`internal/manifest` and `changedetect`; per-source state in `.csdd/state.json`, local
and regenerable), becoming the default with `--full` as escape hatch; its output must
be byte-identical to a full rebuild (tested). Every build appends one line to
`docs/graph/log.md`.

### 5.10 Analyze
`GapReport`: criteria without `realizes` (untested), tasks without `realizes` (orphans),
components without `in_boundary` (unimplemented), pending references, `depends_on`
cycles — plus god nodes (degree) and per-spec communities. **Wiki lint findings**
(broken `links_to` targets, orphan `wiki_page` nodes, index/log violations) live in the
same report tagged with corpus `wiki`; `csdd wiki lint` renders exactly that subset.

### 5.11 Query — two stages
**M1 (ships first):** tiered label matching — exact > prefix > substring over
`NormalizeID`-normalized labels — returning matches plus their 1-hop neighborhood,
deterministic order. `Path(a, b, maxHops)`: shortest undirected path, real edge
direction in rendering. `Explain(label)`: node + neighbors by degree. This covers a
corpus of dozens–hundreds of nodes completely.
**M4 (the graphify `serve.py` engine, earned by the code corpus):** precomputed `Index`
(`label→[]id`, trigram index, IDF, adjacency); `Query(q, opts)`: stopword-filtered
terms → tier scoring × IDF → seeds (≥1 per term) → BFS with **hub-gating** (never
traverse *through* degree > p99, floor 50) → render within `token_budget` (default
2000). Same CLI surface; only the internals grow.

### 5.12 Export
`docs/graph/graph.json` (node-link, ordered — written by every build).
`docs/graph/graph.html` (M5): embedded
template (`//go:embed`) with inline data (escape `</` → `<\/`), vis-network inlined,
colors by community/`file_type`.

### 5.13 Agent integration — wiki scaffold, skills, slash commands, CLAUDE.md (templater work)

The wiki is the Karpathy three-layer architecture instantiated on `docs/`:
**`docs/raw/`** (immutable, user-curated sources) → **`docs/wiki/`** (LLM-authored,
structured, interlinked knowledge) → **the schema** (skill + steering installed by the
CLI). The goal: the user can drop *anything* raw and end up with a knowledge base that
is solid, structured, and cheap for an LLM to consume.

Templates added under `internal/templater/templates/wiki/`:
- `docs/raw/` — the dropzone. A short `README.md` explains: sources go here verbatim
  (articles, notes, transcripts, clippings); nothing here is ever edited — not by the
  CLI, not by the LLM.
- `docs/wiki/index.md` — catalog skeleton (by-category sections, entry format documented).
- `docs/wiki/log.md` — chronology skeleton; documents the
  `## [YYYY-MM-DD] <op> | <title>` format (`op ∈ ingest|query|lint|refactor`) and that
  `grep "^## \[" log.md | tail -5` must always work.
- `docs/wiki/pages/` — content home (LLM-authored).
- `.claude/skills/wiki/SKILL.md` — the **schema doc**: page conventions (frontmatter
  keys including `sources:` for provenance, `[[wikilinks]]`, one concept per page) and
  the three workflows —
  **Ingest** (user drops a source into `docs/raw/` → LLM reads it → writes/extends
  pages under `pages/` with `sources:` pointing back at the raw file → updates
  `index.md` → cross-references related pages → appends a log entry),
  **Query** (read index → drill into pages → synthesize with citations →
  optionally file the answer back as a new page — explorations compound),
  **Maintain** (run `csdd wiki lint`, fix findings — including unprocessed raw
  sources — and look for contradictions/stale claims between pages).
- **Boundary (stated verbatim in the skill):** the wiki is a knowledge base built from
  `docs/raw/` — it is not documentation *of the specs*. `specs/` holds feature
  contracts; the wiki holds knowledge. When a page needs to mention project internals,
  it links (path or `[[...]]`), never restates — duplicated text is drift waiting to
  happen and a Maintain finding.
- `.claude/skills/graph/SKILL.md` — the **graph-controller skill**: drives the graph
  exclusively through the CLI (`csdd graph …` / `npx csdd graph …`). It encodes the
  *moments*: `build` after editing specs/tasks/code; `query`/`explain`/`path` before
  grepping or reading code (the brain-first habit); `analyze --strict` before
  review/commit; and how to consume `--json` output. Neither skill ever writes
  `docs/graph/` or `.csdd/` directly — the CLI is the single mutation path (R16.4).
- `.claude/skills/stack/SKILL.md` — the **tech-contract skill** (R17.6): owns the
  three contract workflows — **Propose** (options + trade-offs → ask → amend Decided
  only after approval), **Refine** (Context7 MCP when configured, else web search →
  wiki page → Refs link), **Reconcile** (act on `analyze` tech findings). Never edits
  manifests or adopts technology without an explicit human decision.
- `.claude/commands/` — **user-facing slash commands** (same mechanism as
  `/csdd-commit`): `/csdd-graph-query`, `/csdd-graph-path`, `/csdd-graph-explain`,
  `/csdd-graph-analyze`, `/csdd-wiki-ingest`, `/csdd-wiki-query`, `/csdd-wiki-lint`,
  `/csdd-stack-propose`, `/csdd-stack-refine` — thin wrappers routing to the
  skills/CLI so the user can drive the knowledge base directly from the prompt.
- **CLAUDE.md wiring** (reuses the managed-section machinery in
  `internal/cli/claudemd.go`): `csdd init` writes the knowledge-base workflow moments
  into CLAUDE.md — consult the graph before searching; rebuild after changing
  specs/tasks/code; run wiki Ingest when a file lands in `docs/raw/`; run
  `csdd graph analyze --strict` + `csdd wiki lint` before commit/PR — **plus the tech
  contract protocol (R17.4)**: `docs/stack.md` is law; unlisted technology = open
  decision → propose options with trade-offs and ask first; before first use of a
  contracted lib, refine via Context7 MCP (when configured) or web search and file the
  findings as a wiki page linked from the contract's Refs — **and the stack-skill
  invocation moments (R17.7)**: invoke the stack skill when a new dependency/technology
  is about to enter the project, when `analyze` reports tech findings, and when the
  user mentions adopting or changing technology.
- A steering rule pointing agents at the knowledge base: *"consult `docs/graph/graph.json`
  (`csdd graph query`) and `docs/wiki/index.md` before grepping."*
Idempotent: existing files are never overwritten without `--force`; `docs/raw/` contents
are never touched.

### 5.14 CLI surface
```text
csdd graph build   [--root DIR] [--json]        # full rebuild (M4: incremental default, --full)
csdd graph query   "<terms>" [--json]           # M4 adds --budget N, --hops N
csdd graph path    <A> <B> [--max-hops N] [--json]
csdd graph explain <label> [--json]
csdd graph analyze [--json] [--strict]          # --strict: exit non-zero on findings
csdd graph export  [--out PATH]                 # M5: graph.html
csdd wiki  init    [--force] [--root DIR]
csdd wiki  lint    [--json]                     # exits non-zero on findings (R10.5)
```
Registered as `case "graph"` / `case "wiki"` in `internal/cli/cli.go` `Run()`, following
the existing stdlib-`flag` + subcommand-switch pattern and `jsonout` conventions.

**Workspace marker & init (R15).** Today the marker is `.claude/` (see the doc comment
in `internal/paths/paths.go`); this plan moves it to `.csdd/`. A shared
`RequireWorkspace(root)` helper (in `internal/paths` or `internal/workspace`) checks for
`.csdd/` and returns the actionable error; `graph` and `wiki` subcommands call it first.
`csdd init` (`internal/cli/init.go`) is extended to compose the full development setup:
existing `.claude/` scaffolding + `specs/` + `docs/plans|raw|wiki|graph/` (reusing the
§5.13 wiki templates and the `docs/graph/` README) + `.csdd/`. Idempotent — re-running
adds only what is missing, which is also the upgrade path for legacy workspaces that
predate `.csdd/`.

### 5.15 Web
A "Graph" tab (`internal/web/frontend/src/Graph.tsx` + route) rendering
`docs/graph/graph.json` with vis-network. The JSON is served through the dashboard's existing
read-only hardening path (host guard, redaction — see PR #43); no new write endpoint.

### 5.16 File structure plan
```text
internal/graph/
├── model.go          # Node, Edge, Graph; node-link (un)marshal              [Model]
├── ids.go            # NormalizeID, MakeID                                    [Model]
├── extract.go        # Extractor contract; corpus walk; dispatch             [Extract]
├── extract_spec.go   # requirements.md / design.md / tasks.md / spec.json    [Extract]
├── extract_claude.go # steering / skills / agents / mcp / CLAUDE.md          [Extract]
├── extract_wiki.go   # docs/**.md: frontmatter, [[wikilinks]], cited paths   [Extract]
├── extract_go.go     # M3: go/parser → code nodes, contains/imports          [Extract]
├── build.go          # fragments → Graph (merge, reconcile IDs, dedup, sort) [Assemble]
├── analyze.go        # gaps, wiki lint findings, god nodes, cycles           [Analyze]
├── query.go          # M1: label tiers + path + explain; M4: full engine     [Query]
├── export.go         # graph.json writer; M5: graph.html                     [Export]
├── incremental.go    # M4: manifest + changedetect → replace-per-source      [Incremental]
└── *_test.go         # table-driven; fixtures in internal/graph/testdata/
internal/cli/
├── graph.go          # `csdd graph …` subcommand                             [CLI]
└── wiki.go           # `csdd wiki …` subcommand                              [CLI]
internal/templater/templates/wiki/   # wiki scaffold + wiki/graph SKILL.md + slash commands + steering + CLAUDE.md section  [WikiScaffold]
internal/web/frontend/src/
└── Graph.tsx (+ route/tab)          # M5: vis-network view                   [Web]
```
**Modified files:** `internal/cli/cli.go` (register both subcommands), `internal/web`
(tab route), `internal/templater` (template registration).

### 5.17 Requirements Traceability
| Requirement | Components | Interfaces | Flows |
|---|---|---|---|
| 1.1 | Extract, Assemble | Build() | build |
| 1.2 | Model | — | build |
| 1.3 | Extract | Extract() | build |
| 1.4 | Model | NormalizeID(), MakeID() | build |
| 2.1 | Query | Query() | query |
| 2.2 | Query | Path() | query |
| 2.3 | Query | Explain() | query |
| 3.1 | Analyze | AnalyzeGaps() | analyze |
| 3.2 | Analyze | AnalyzeGaps() | analyze |
| 3.3 | Analyze | AnalyzeGaps() | analyze |
| 3.4 | Analyze | AnalyzeGaps() | analyze |
| 3.5 | Analyze | AnalyzeGaps() | analyze |
| 4.1 | Assemble, Export | Build() | build |
| 4.2 | Export | Build() | build |
| 5.1 | (all) | — | ci |
| 5.2 | (all) | — | ci |
| 6.1 | CLI | graph/wiki subcommands | build/query/analyze |
| 6.2 | Export | Export() | export |
| 6.3 | Web | Graph.tsx | web |
| 7.1 | Export | Build(), Export() | build |
| 7.2 | Assemble, Export | Build() | build |
| 7.3 | Model | — | build |
| 7.4 | Incremental | Build() | build |
| 8.1 | Model | MarshalNodeLink() | build |
| 8.2 | Model | — | build |
| 9.1 | WikiScaffold | WikiInit() | wiki-init |
| 9.2 | WikiScaffold | WikiInit() | wiki-init |
| 9.3 | WikiScaffold | — | wiki-init |
| 9.4 | WikiScaffold | WikiInit() | wiki-init |
| 10.1 | Analyze | WikiLint() | wiki-lint |
| 10.2 | Analyze | WikiLint() | wiki-lint |
| 10.3 | Analyze | WikiLint() | wiki-lint |
| 10.4 | Analyze | WikiLint() | wiki-lint |
| 10.5 | Analyze | WikiLint() | wiki-lint |
| 10.6 | CLI | wiki subcommand | wiki-lint |
| 11.1 | Extract | Extract() | build |
| 11.2 | Extract | Extract() | build |
| 11.3 | Analyze | WikiLint() | wiki-lint |
| 12.1 | Extract | Extract() | build |
| 12.2 | Extract | Extract() | build |
| 12.3 | Assemble | Build() | build |
| 12.4 | Extract | Extract() | build |
| 13.1 | Query | Query() | query |
| 13.2 | Incremental | Build() | build |
| 13.3 | Incremental, CLI | Build() | build |
| 14.1 | CLI | manifest Load()/Save() | update |
| 14.2 | CLI | manifest Load()/Save() | update |
| 15.1 | CLI, WikiScaffold | init | init |
| 15.2 | CLI | RequireWorkspace() | init |
| 15.3 | CLI | RequireWorkspace() | init |
| 16.1 | WikiScaffold | graph SKILL.md | init |
| 16.2 | WikiScaffold | wiki SKILL.md | wiki-init |
| 16.3 | CLI | claudemd section | init |
| 16.4 | WikiScaffold | skill templates | init |
| 16.5 | WikiScaffold | commands templates | init |
| 17.1 | CLI, WikiScaffold | init | init |
| 17.2 | Extract | Extract() | build |
| 17.3 | Analyze | AnalyzeGaps() | analyze |
| 17.4 | CLI | claudemd section | init |
| 17.5 | Analyze | AnalyzeGaps() | analyze |
| 17.6 | WikiScaffold | stack SKILL.md | init |
| 17.7 | CLI | claudemd section | init |

---

## 6. Tasks (content of `tasks.md`) — TDD, boundaries, dependencies

All `_Requirements:` lists are explicit and comma-separated (no ranges).

### Phase 1: Foundation (M1)
- [ ] 1. Model + IDs _Boundary: Model_
  - [ ] 1.1 RED — tests for `NormalizeID`/`MakeID` (idempotence, NFKC, unicode) and node-link round-trip
    - _Requirements: 1.4, 8.1_
  - [ ] 1.2 GREEN — minimal `model.go` + `ids.go`
    - _Requirements: 1.2, 1.4, 8.1, 8.2_

### Phase 2: The lint core (M1)
- [ ] 2. Artifact extractors (P) _Boundary: Extract_
  - [ ] 2.1 RED — fixtures in `testdata/` (requirements/design/tasks/spec.json/.claude) + per-parser tests, including a fixture that mirrors `internal/templater/templates/spec/*.tmpl` and fails if templates drift
    - _Requirements: 1.1, 1.3_
    - _Depends: 1.2_
  - [ ] 2.2 GREEN — `extract.go` (Extractor contract + walk) + `extract_spec.go` + `extract_claude.go`
    - _Requirements: 1.1, 1.3_
- [ ] 3. Wiki extractor (P) _Boundary: Extract_
  - [ ] 3.1 RED — fixtures for wiki pages: frontmatter (incl. `sources:` provenance), `[[wikilinks]]` (incl. `|alias` and `#anchor` forms), cited paths existing/missing, `docs/raw/` files → opaque `raw_source` nodes, `derived_from` edges, orphan/broken-link/unprocessed-source cases
    - _Requirements: 11.1, 11.2_
    - _Depends: 1.2_
  - [ ] 3.2 GREEN — `extract_wiki.go`
    - _Requirements: 11.1, 11.2_
- [ ] 4. Assemble + dedup _Boundary: Assemble_
  - [ ] 4.1 RED — merge, `(spec,N,M)` ID reconciliation, edge/node dedup, total deterministic ordering (byte-stable double-build test)
    - _Requirements: 1.1, 4.1, 7.2_
    - _Depends: 1.2, 2.2, 3.2_
  - [ ] 4.2 GREEN — `build.go` (full rebuild; log append via Export)
    - _Requirements: 1.1, 4.1, 4.2, 7.2_
- [ ] 5. Analyze: gaps + wiki lint core _Boundary: Analyze_
  - [ ] 5.1 RED — untested criterion, orphan task, unimplemented component, pending reference, `depends_on` cycle; broken wikilink, orphan page, index desync, log format, unprocessed raw source
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 10.1, 10.2, 10.3, 10.4, 10.5_
    - _Depends: 4.2_
  - [ ] 5.2 GREEN — `analyze.go` (GapReport with corpus tags)
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 10.1, 10.2, 10.3, 10.4, 10.5, 11.3_

### Phase 3: Surfaces (M1 + M2)
- [ ] 6. Query v1: label tiers + path + explain (P) _Boundary: Query_
  - [ ] 6.1 RED — exact/prefix/substring tiers, deterministic order, 1-hop neighborhood; shortest path with real direction; explain by neighbor degree
    - _Requirements: 2.1, 2.2, 2.3_
    - _Depends: 4.2_
  - [ ] 6.2 GREEN — `query.go` (v1 internals)
    - _Requirements: 2.1, 2.2, 2.3_
- [ ] 7. Export: graph.json writer (P) _Boundary: Export_
  - [ ] 7.1 RED — stable node-link output, root-relative paths, log.md append line
    - _Requirements: 4.2, 7.1, 7.3, 8.1_
    - _Depends: 4.2_
  - [ ] 7.2 GREEN — `export.go` (json + log; html comes in task 13)
    - _Requirements: 4.2, 7.1_
- [ ] 8. Agent-integration templates: wiki scaffold + skills + slash commands (P) _Boundary: WikiScaffold_
  - [ ] 8.1 RED — `wiki init` creates raw dropzone + index/log/pages + wiki skill + steering (with the raw→wiki pipeline and boundary text); graph-controller skill; stack skill (Propose/Refine/Reconcile, human-decision gate); slash commands under `.claude/commands/`; idempotent; no-clobber without `--force`; never touches `docs/raw/` contents; skills contain no direct writes to `docs/graph/`/`.csdd/`
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 16.1, 16.2, 16.4, 16.5, 17.6_
  - [ ] 8.2 GREEN — `internal/templater/templates/wiki/` (+ graph/stack skills + commands templates) + install wiring
    - _Requirements: 9.1, 9.2, 9.4, 16.1, 16.2, 16.5, 17.6_
- [ ] 9. CLI `csdd graph` + `csdd wiki` _Boundary: CLI_
  - [ ] 9.1 RED — command surface, flags, `--json` output, exit codes (`analyze --strict`, lint non-zero on findings)
    - _Requirements: 6.1, 10.6_
    - _Depends: 5.2, 6.2, 7.2, 8.2_
  - [ ] 9.2 GREEN — `internal/cli/graph.go` + `internal/cli/wiki.go` + registration in `cli.go`
    - _Requirements: 6.1, 10.6_

### Phase 4: The code graph (M3)
- [ ] 10. Go source extractor _Boundary: Extract_
  - [ ] 10.1 RED — fixture mini-module in `testdata/`: packages/files/decls/imports; `vendor/`+`testdata/` exclusion; `_test.go` tagging; cited-path resolution upgrading `code_ref`→`code`
    - _Requirements: 12.1, 12.2, 12.3, 12.4_
    - _Depends: 4.2_
  - [ ] 10.2 GREEN — `extract_go.go` + resolution pass in Assemble (no downstream special-casing — the Extractor-contract acceptance test)
    - _Requirements: 12.1, 12.2, 12.3, 12.4_

### Phase 5: The scale engine (M4)
- [ ] 11. Full query engine _Boundary: Query_
  - [ ] 11.1 RED — tier scoring × IDF, seeds per term, BFS hub-gating (p99 floor 50), token budget rendering; same CLI surface as v1
    - _Requirements: 13.1_
    - _Depends: 6.2, 10.2_
  - [ ] 11.2 GREEN — `query.go` (M4 internals swap; v1 tests keep passing)
    - _Requirements: 13.1_
- [ ] 12. Incremental builds _Boundary: Incremental_
  - [ ] 12.1 RED — skip-by-hash, replace-per-source, prune deleted, `--full`; output byte-identical to a full rebuild
    - _Requirements: 7.4, 13.2, 13.3_
    - _Depends: 4.2, 10.2_
  - [ ] 12.2 GREEN — `incremental.go` (reuses `internal/manifest` + `internal/session/changedetect.go`); incremental becomes the default
    - _Requirements: 7.4, 13.2, 13.3_

### Phase 6: Visualization + validation (M5)
- [ ] 13. HTML export (P) _Boundary: Export_
  - [ ] 13.1 RED — self-contained html (no external asset, `</` escaped)
    - _Requirements: 6.2_
    - _Depends: 7.2_
  - [ ] 13.2 GREEN — embedded vis-network template in `export.go`
    - _Requirements: 6.2_
- [ ] 14. Web Graph tab (P) _Boundary: Web_
  - [ ] 14.1 RED — render test reading `docs/graph/graph.json` through the hardened read-only route
    - _Requirements: 6.3_
    - _Depends: 7.2_
  - [ ] 14.2 GREEN — `Graph.tsx` + route
    - _Requirements: 6.3_
- [ ] 15. E2E golden path + CI gate
  - [ ] 15.1 e2e on a fixture workspace: `graph build` → `analyze` → `query`; `wiki init` → `lint`; double-build byte-stability; `CGO_ENABLED=0` for all `make dist` targets in CI
    - _Requirements: 5.1, 5.2_
    - _Depends: 9.2_

### Phase 7: Workspace & state consolidation (independent — any time after M1)
- [ ] 16. Migrate the update manifest to `.csdd/` _Boundary: CLI_
  - [ ] 16.1 RED — read-fallback to legacy `.claude/.csdd-manifest.json`, write to `.csdd/manifest.json` on save, identical behavior for legacy-only workspaces
    - _Requirements: 14.1, 14.2_
  - [ ] 16.2 GREEN — path change in `internal/manifest` callers + `internal/paths` constant
    - _Requirements: 14.1, 14.2_
- [ ] 17. Init: full layout + workspace marker _Boundary: CLI_
  - [ ] 17.1 RED — `init` creates `specs/`, `docs/plans|raw|wiki|graph/`, `.csdd/`; wires the CLAUDE.md knowledge-base section (workflow moments incl. stack protocol + stack-skill invocation moments); idempotence/no-clobber; `RequireWorkspace` errors for `graph`/`wiki` when `.csdd/` is absent; legacy-upgrade message when only `.claude/` exists
    - _Requirements: 15.1, 15.2, 15.3, 16.3, 17.7_
    - _Depends: 8.2_
  - [ ] 17.2 GREEN — extend `internal/cli/init.go` + `RequireWorkspace` helper + CLAUDE.md section (via `claudemd` machinery) + wiring in `graph`/`wiki` subcommands
    - _Requirements: 15.1, 15.2, 15.3, 16.3, 17.7_

### Phase 8: Tech contract (parallel after task 5; template with task 17)
- [ ] 18. `docs/stack.md` contract: extractor + lints + scaffold _Boundary: Extract_
  - [ ] 18.1 RED — fixtures: Decided-table parsing → `tech` nodes; `go.mod`/`package.json`/`pyproject.toml` deps → `tech` usage + `uses_tech`; lints (undeclared tech decision, phantom tech, unrefined tech, no-contract finding); scaffold template with Rules protocol prefilled and no choices prefilled; CLAUDE.md moments include the contract protocol
    - _Requirements: 17.1, 17.2, 17.3, 17.4, 17.5_
    - _Depends: 4.2_
  - [ ] 18.2 GREEN — `extract_stack.go` (contract + manifests) + analyze rules + init template + CLAUDE.md section text
    - _Requirements: 17.1, 17.2, 17.3, 17.4, 17.5_

**Execution order (deps):** 1 → {2, 3, 8} → 4 → {5, 6, 7} → 9 → 10 → {11, 12} → {13, 14, 15}.
`(P)` tasks are parallelizable across distinct boundaries. Tasks 13–15 need only task 9
(plus 7 for exports) — they may start before 10–12 if sequencing favors it. Task 16 is
independent of the graph pipeline and may ship any time after M1.

---

## 7. Milestones

| Milestone | Delivery | Tasks |
|---|---|---|
| **M1 — Spec lint (the immediately-paying core)** | `csdd graph build` writes `docs/graph/graph.json` (specs + `.claude/` + `docs/`); `analyze` reports every traceability gap; `query`/`path`/`explain` v1 | 1–7, 9 |
| **M2 — Wiki standard + agent integration + tech contract** | `wiki init` scaffolds the `docs/raw/` → `docs/wiki/` pipeline; wiki + graph skills, `/csdd-graph-*` and `/csdd-wiki-*` slash commands, CLAUDE.md workflow moments incl. the stack protocol; `wiki lint` gates health incl. unprocessed sources; `docs/stack.md` contract indexed + tech lints | 3, 8, 9, 18 |
| **M3 — Code graph (the original vision)** | Go source in the same graph via `go/parser`; artifact-cited paths resolve to real code nodes; the graph now grows with the codebase | 10 |
| **M4 — Scale engine (earned by M3)** | full query engine (IDF × tiers, hub-gating, budget); incremental builds by default with `--full` | 11, 12 |
| **M5 — Visualization + validation** | `graph.html` export; web dashboard tab; e2e golden path; `CGO_ENABLED=0` CI gate | 13–15 |
| **M6 — Workspace & state consolidation (independent; any time after M1)** | `csdd init` scaffolds the complete dev layout (`specs/`, `docs/*`, `.claude/`, `.csdd/`); `.csdd/` becomes the workspace marker with legacy-upgrade path; update manifest migrated to `.csdd/manifest.json` with read-fallback | 16, 17 |
| **Post-v1** | always-on hooks (post-command/commit rebuild); "consult the graph before grep" steering everywhere; polyglot code indexing (tree-sitter-WASM over `wazero` spike); `go/types` call graphs if a use case demands | — |

---

## 8. Risks and decisions

- **cgo-free (locked).** Annotation parsing needs no tree-sitter; Go code parsing uses
  stdlib `go/parser` (syntax-only). Polyglot indexing enters post-v1 through the
  Extractor seam via a tree-sitter-WASM/`wazero` spike.
- **No LLM in the binary (locked).** Wiki content is authored by LLM sessions following
  the installed skill; the CLI does only deterministic scaffold/lint. This is the
  csdd-shaped split of Karpathy's pattern.
- **Scale machinery deferred, not dropped (decided in revision 2).** Full query engine
  and incremental builds are specified (§5.11, §5.9) but implemented in M4, after the
  code corpus justifies them. Guard: the M4 swap must keep all M1 query tests passing
  and incremental output byte-identical to full rebuilds.
- **Wiki/specs knowledge duplication.** The skill states the boundary verbatim (§5.13):
  specs = per-feature contracts; wiki = cross-feature synthesis; wiki pages link to
  specs, never restate them. Contradiction with specs is a Maintain finding.
- **Git-diff stability.** Total deterministic ordering (§5.9); double-build
  byte-equality is an explicit test (tasks 4.1, 15.1).
- **Template drift.** Parsers depend on `internal/templater/templates/spec/*.tmpl`;
  fixtures mirror the templates and a dedicated test fails when templates change without
  a parser update (task 2.1).
- **Vocabulary drift.** §5.5 is the single source of truth; `docs/graph/README.md` and the
  seed must change in the same commit (enforced by review, optionally by a test reading
  both).
- **Range shorthand.** `_Requirements: 3.1–3.5_` style ranges are a parse error surfaced
  by `analyze`, not a supported syntax — one less ambiguity in the format.
- **Generated island inside `docs/` (decided in revision 3).** Moving the knowledge
  index to `docs/graph/` trades the simple "CLI never writes docs/" invariant for a
  purpose-based split (knowledge vs. state) with a much cleaner git story. Guard: the
  README in `docs/graph/` declares it read-only, and R7.1 forbids generated files
  anywhere else in `docs/`.
- **Manifest migration compat (task 16).** Read-fallback to
  `.claude/.csdd-manifest.json` keeps existing workspaces working; writes go to
  `.csdd/manifest.json` on the next save. Independent of the graph milestones.

---

## 9. Appendices

### 9.1 graphify → csdd map
| graphify (Python) | csdd (Go) |
|---|---|
| `ids.py` | `internal/graph/ids.go` |
| `extract.py` + extractors | `internal/graph/extract*.go` (annotation parsers + `go/parser`) |
| `build.py` | `internal/graph/build.go` |
| `analyze.py` | `internal/graph/analyze.go` (gaps + wiki lint = the focus) |
| `serve.py` (query engine) | `internal/graph/query.go` (v1 lite; M4 full port) |
| `export.py` | `internal/graph/export.go` |
| `cache.py` + manifest | `internal/manifest` + `internal/session/changedetect.go` (M4 reuse) |
| `graphify-out/` | `docs/graph/` (knowledge index) + `.csdd/` (operational state) |

### 9.2 Karpathy LLM-wiki → csdd map
| LLM-wiki | csdd |
|---|---|
| Raw sources (immutable) | `docs/raw/` — user-curated dropzone, read-only for CLI and LLM |
| The wiki (LLM-authored) | `docs/wiki/` (pages by LLM sessions via the skill, `sources:` provenance) |
| The schema (CLAUDE.md/AGENTS.md) | `.claude/skills/wiki/SKILL.md` + steering rule (installed by `wiki init`) |
| Ingest | LLM workflow in the skill (CLI never authors content) |
| Query | LLM workflow + `csdd graph query` over the same graph |
| Lint | `csdd wiki lint` (deterministic half) + skill's Maintain workflow (semantic half) |
| `index.md` / `log.md` | `docs/wiki/index.md` / `docs/wiki/log.md` (scaffolded, format-linted) |
| "compiled once, kept current" | `csdd graph build` on every change (M4: incremental) |

### 9.3 References
- `graphify/ANALISE-ARQUITETURA.md` — graphify internals; decisions ported here.
- `graphify/llm-wiki.md` — Karpathy's pattern (source of the wiki lifecycle).
- `docs/graph/README.md` — schema doc for the knowledge index. `docs/graph/graph.json` — hand-seeded fixture. `.csdd/README.md` — the state-dir contract.
- `internal/templater/templates/spec/*.tmpl` — ground truth of artifact formats.
- `docs/plans/PLANO-graph-index.md` — superseded Portuguese predecessor (graph-only).

### 9.4 Executor notes (this repo / workstation)
- **Verify:** `make check` (gofmt + vet + race tests). Distribution gate:
  `CGO_ENABLED=0` via `make dist VERSION=vX.Y.Z` (6 targets).
- **CLI pattern:** stdlib `flag` + subcommand switch in `internal/cli/cli.go` `Run()`;
  `--json` output goes through the `jsonout` helpers; tests are table-driven next to
  each file.
- **Line endings:** the working tree on this workstation is CRLF over an LF index —
  stage only the specific paths you changed; **never `git add -A`**.
- **Flaky tests:** four update/clean `.old`-backup CLI tests fail non-deterministically
  on `/mnt/c` (WSL) — re-run before treating as a regression.
- **Go toolchain (this workstation):** not on the default PATH; it lives at
  `~/.local/go/bin` (export PATH + GOPATH/GOCACHE).
- **Before starting:** `git fetch origin` and check open PRs — parallel sessions may
  have landed related work.
- **Commits/PRs:** conventional style (`feat(graph): …`); repo templates forbid
  AI/assistant attribution in generated commits and PRs (#42).
- **Dogfood:** once M1 lands, run `csdd graph build` on this repo and convert this
  plan's sections 4/5/6 into `specs/knowledge-base/` via the csdd spec flow; the seed
  `docs/graph/graph.json` then gets regenerated deterministically. M3's first real corpus is
  csdd's own `internal/` tree — the graph indexing the tool that builds it.
