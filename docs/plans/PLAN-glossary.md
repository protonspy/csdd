# Master Plan — The Glossary (`docs/glossary.md` + the ubiquitous-language discipline)

> **Audience:** the LLM agent (and humans) implementing this feature end-to-end.
> **Status:** draft for review — revision 1 (2026-07-07). Not yet approved.
> **Companions:** `docs/plans/PLAN-decision-records.md` (the sibling discipline this
> plan mirrors — M1 machine contract / M2 authoring discipline; ADR 0003 there records
> the deferral this plan now resolves); `docs/plans/PLAN-plan-mode.md` (the substrate:
> validate/analyze seams, in flight on this branch); `docs/plans/PLAN-knowledge-base.md`
> (graph vocabulary); github.com/mattpocock/skills —
> `engineering/domain-modeling/CONTEXT-FORMAT.md` (the format this adapts, studied
> 2026-07-07).
> **Dogfood note:** csdd's own domain — plan, feat, spec, seed, brief, quality gate,
> steering, wiki page, stack row, ADR — becomes `docs/glossary.md` in task 6: the
> glossary's first entries define the tool that lints them, and its `_Avoid_` lists
> (feature, story, ticket, requirement-doc…) become the lint's first fixtures.

---

## 1. Context and vision

### 1.1 What we are building

csdd now has two normative project-level contracts: `docs/stack.md` (*with which
technology*, `stack:` refs, Decided-row lint) and `docs/adr/` (*why this way*, `adr:`
refs, triple-gate admission). Both answer questions; neither fixes **what things are
called**. Terminology drift is the quietest form of project rot: the plan says
"customer", a spec says "client", a wiki page says "account", feat slugs mint a fourth
synonym — and the graph, which links by name, sees four unrelated things.

This plan adds the third contract: **`docs/glossary.md`** — the project's ubiquitous
language, adapted from domain-modeling's `CONTEXT.md`:

```text
Term (canonical, one meaning)  →  entry in docs/glossary.md (+ _Avoid_ aliases)
    →  identifiers minted from canonical terms (feat slugs, spec dirs, wiki pages)
    →  lint: avoided term in an identifier is a finding
    →  graph: term nodes + references edges — "where is Customer used?" is a query
```

The enforcement insight (and the v1 boundary): **identifiers are where names
crystallize and where matching is deterministic.** A feat slug, a spec directory, a
wiki page name — token-matching those against canonical terms and `_Avoid_` aliases
has zero false positives worth arguing about. Prose is where terminology *drifts*, but
prose matching is fuzzy — so prose stays the skill's discipline (challenge, sharpen,
record), never the linter's.

### 1.2 The domain-modeling synthesis (what we take, what csdd upgrades)

| domain-modeling practice | csdd adaptation |
|---|---|
| `CONTEXT.md` glossary, "totally devoid of implementation details" | `docs/glossary.md` — same purity rule, named for what it is (the `stack.md` sibling), parseable `## Language` grammar |
| opinionated entries: canonical term + `_Avoid_` synonym list | adopted verbatim — and the `_Avoid_` list becomes **machine-enforceable** (identifier lint) instead of aspirational |
| challenge conflicting terms, sharpen fuzzy language, stress-test with scenarios | the standalone **glossary skill** — one owner of the discipline, invoked by prd/quick-prd/wiki (the stack-skill composition precedent) |
| cross-reference user claims against code | upgraded: `csdd graph query` first — the graph already indexes the workspace |
| update `CONTEXT.md` inline the moment a term resolves, never batched | adopted — the ADR inline-write rule, same muscle |
| lazy file creation | adopted — the skill creates `docs/glossary.md` on the first resolved term; `csdd init` scaffolds nothing |
| `CONTEXT-MAP.md` multi-context repos | **deferred post-v1** — csdd targets single-context repos; the map adds real complexity for a rare case |
| no term lifecycle (its known gap — meanings change silently) | **the tombstone rule:** a renamed canonical term moves to its successor's `_Avoid_` list — the lint then patrols the old name forever |

### 1.3 Value thesis (why this build order)

- **The contract pays before the discipline exists.** Once `docs/glossary.md` parses
  and identifiers lint (M1), a hand-written glossary already renames the future: every
  new feat slug, spec dir and wiki page is checked at authoring time.
- **The graph is why the glossary lives here and not in a skill repo.** Term nodes +
  `references` edges make language queryable (`csdd graph query customer`), and the
  canonical/alias table is the entity-resolution seed the knowledge-base extractor has
  been missing.
- **The discipline (M2) is template-only work** on a proven contract — the
  decision-records build order, repeated.

---

## 2. Storage (extends the project-wide convention, revision 3)

| Path | Role | Author |
|---|---|---|
| `docs/glossary.md` | The ubiquitous language: canonical terms, tight definitions, `_Avoid_` aliases. A contract, not documentation — zero implementation details. | Human/LLM prose (the glossary skill writes it); parsed and linted by the CLI, never written by it. |

Project-scoped, single file, created lazily by the skill (the `docs/stack.md`
precedent — no root template). One flat `## Language` section; `###` clusters allowed
when natural groups emerge.

---

## 3. Design principles (inviolable)

1. **The glossary is a contract, not documentation.** Like `stack.md`: normative,
   parseable, lint-backed. General programming concepts don't belong — only terms
   unique to this project's domain.
2. **The CLI never authors terms.** The skill writes entries; the CLI parses, lints,
   and graphs them (the stack/ADR precedent). No `csdd glossary` command group.
3. **Identifiers are the enforcement surface (v1).** Lint bites only where matching is
   deterministic — feat slugs, spec dirs, wiki page names. Prose terminology is the
   skill's job; a fuzzy lint that cries wolf trains users to ignore the whole linter.
4. **Renames leave tombstones.** A replaced canonical term is appended to its
   successor's `_Avoid_` list in the same edit — the old name stays banned forever,
   mechanically. This is the term lifecycle, and it needs no state machine.
5. **One owner of the discipline.** The glossary skill holds the moves (challenge,
   sharpen, scenario-test, record inline); prd/quick-prd/wiki *invoke* it — they never
   duplicate its rules (the drift-bug lesson).
6. **Aggressive reuse.** Parse/normalize/finding/extractor patterns come from the
   stack, wiki and plan machinery; the graph reuses `references` — one new node type,
   zero new relations.

---

## 4. Requirements (content of `requirements.md`)

EARS format. Requirement IDs in annotations are explicit comma-separated lists.

### Requirement 1: The glossary contract
**Objective:** As the toolchain, I want the language machine-readable, so lint and
graph work from explicit structure.
**Acceptance Criteria**
1. THE SYSTEM SHALL parse `docs/glossary.md` entries under `## Language`: a
   `**<Term>**:` line, definition prose until the next entry, and an optional
   `_Avoid_: <a>, <b>` line; `###` subheadings group entries into clusters without
   affecting parsing.
2. THE SYSTEM SHALL normalize terms and aliases for matching (lowercase, non-alnum
   collapse — the `normalizeTechName` discipline) and SHALL treat multi-word terms as
   contiguous token sequences in kebab-case identifiers.
3. THE SYSTEM SHALL report as well-formedness findings: duplicate canonical terms, an
   alias colliding with another entry's canonical term or alias, and an entry with no
   definition prose.

### Requirement 2: Identifier lint at authoring time (`csdd plan validate`)
**Objective:** As the plan author, I want avoided terms caught before approval, so
feat names are minted in canonical language.
**Acceptance Criteria**
1. WHERE `docs/glossary.md` exists THE SYSTEM SHALL report every feat slug (and the
   plan slug) containing an `_Avoid_` alias as whole token(s) — "avoided term
   '<alias>' in '<slug>' — canonical is '<term>'".
2. WHERE `docs/glossary.md` exists THE SYSTEM SHALL surface its well-formedness
   findings (R1.3) in the same pass (the ADR-dir precedent of
   PLAN-decision-records R3.2).
3. IF any violation is found THEN the system SHALL exit non-zero.

### Requirement 3: Identifier lint workspace-wide (`csdd graph analyze`)
**Objective:** As a knowledge-base user, I want the whole workspace held to the same
language, so drift is visible wherever names live.
**Acceptance Criteria**
1. WHEN `csdd graph analyze` runs and `docs/glossary.md` exists THEN the system SHALL
   report avoided-term matches in spec directory names and wiki page names,
   domain-tagged so `csdd wiki lint` renders the wiki subset (§5.10 — lint is analyze
   filtered).
2. THE SYSTEM SHALL report an informational "orphan term" (no inbound `references`
   edge) — terms may legitimately predate their first use; informational only.

### Requirement 4: Terms in the graph
**Objective:** As a knowledge-base user, I want language in the same graph as plans,
specs and code, so "where is this concept used" is a query.
**Acceptance Criteria**
1. WHEN `csdd graph build` runs THEN the system SHALL emit a `term` node per entry
   (id from the normalized canonical term; attrs: definition, avoid aliases, cluster).
2. THE SYSTEM SHALL emit a `references` edge from every feat, spec, and wiki page
   whose identifier tokens match a term or one of its aliases (deterministic
   whole-token match — alias matches carry an attr naming the matched alias, which is
   what R3.1 surfaces).

### Requirement 5: The glossary skill
**Objective:** As any authoring session, I want one owner of the language discipline,
so terminology is challenged and recorded the same way everywhere.
**Acceptance Criteria**
1. THE SYSTEM SHALL install a **glossary skill** with the domain-modeling moves:
   **challenge** a term conflicting with an existing entry the moment it appears;
   **sharpen** vague or overloaded terms by proposing a canonical one (with the
   recommended-answer form of the decision grill); **stress-test** boundaries with
   concrete edge-case scenarios; **cross-reference** claims via `csdd graph query`
   before asking the user.
2. WHEN a term resolves THEN the skill SHALL update `docs/glossary.md` immediately
   (never batched), creating the file lazily on the first entry.
3. WHEN a canonical term is renamed THEN the skill SHALL append the old term to the
   successor's `_Avoid_` list in the same edit (the tombstone rule).
4. THE SKILL SHALL keep the glossary a contract: definitions of one or two sentences,
   what it IS not what it does, project-specific terms only, zero implementation
   details.

### Requirement 6: Flow integration
**Objective:** As the plan/spec/wiki flows, I want the glossary consulted where names
are born, so canonical language is the path of least resistance.
**Acceptance Criteria**
1. THE prd SKILL's Draft SHALL route terminology conflicts and fuzzy terms surfaced by
   the interview through the glossary skill, and Decompose SHALL mint feat slugs from
   canonical terms (validate enforces per R2.1).
2. THE quick-prd SKILL SHALL carry the same hook (challenge + record, no ceremony).
3. THE wiki SKILL's Ingest SHALL consult the glossary when naming pages.
4. THE SYSTEM SHALL install a **`/glossary` slash command** routing to the skill
   (the `/prd` precedent for user-facing entry points).

### Requirement 7: Satellites (managed docs)
**Objective:** As a csdd workspace, I want the convention discoverable without reading
this plan.
**Acceptance Criteria**
1. THE root knowledge section SHALL gain the `docs/glossary.md` row of §2.
2. THE managed CLAUDE.md section SHALL gain the moment: when a domain term is
   contested or fuzzy, invoke the glossary skill; identifiers use canonical terms;
   never rename a term without its tombstone.

---

## 5. Design (content of `design.md`)

### 5.1 Overview
One new shared package `internal/glossary` (parse, normalize, match) consumed by both
`internal/plan` (validate findings) and `internal/graph` (extractor + analyze) — the
package exists because both sides need it and `plan` must not import `graph`. Plus one
graph extractor, one paths helper, one new skill + command template, and hook edits in
three existing skills. **Zero new CLI commands.**

### 5.2 Goals / Non-Goals
**Goals:** parseable glossary contract; well-formedness lint; avoided-term identifier
lint in `plan validate` (feat/plan slugs) and `graph analyze` (spec dirs, wiki pages,
wiki-lint filtered); `term` nodes + `references` edges; the glossary skill with the
tombstone rule; prd/quick-prd/wiki hooks; `/glossary` command; satellites.
**Non-Goals (v1):** multi-context (`CONTEXT-MAP.md`) — post-v1 if a real repo needs
it; prose linting (even as warnings — deterministic surface only); a `csdd glossary`
command group; alias-aware `[[wiki-ref]]` resolution (post-v1 — needs care to keep
ref resolution deterministic); glossary-driven entity dedup inside the extractor
(post-v1, builds on the same alias table); term pages in the web dashboard; migrating
existing prose to canonical terms.

### 5.3 Reuse map (verified against the current tree)
| Need | Exists in | Use |
|---|---|---|
| Human-contract parse precedent | `internal/plan/validate.go` `decidedRows`/`decidedTech` (~L305-320) | same shape: `glossary.Parse` → term table |
| Name normalization | `internal/plan/validate.go` `normalizeTechName`/`normalizeWikiSlug` + `reNonAlnum` (~L343-368) | `normalizeTerm` in `internal/glossary` (single home; plan/graph both import it) |
| Lint = analyze filtered | `internal/graph/analyze.go:12-13,83` (findings domain-tagged spec\|wiki\|tech) | avoided-term findings tagged so `csdd wiki lint` shows the wiki subset |
| Graph vocabulary constants | `internal/graph/model.go:34-77` (closed Type/Rel sets + meta mirror) | add `TypeTerm`; **reuse `RelReferences`** — no new relation |
| Extractor seam | `internal/graph` `Extractor` (`extract_plan.go` precedent) | `extract_glossary.go` |
| Paths | `internal/paths` (`StackSeg` L32, `Stack()` L116) | `GlossarySeg`, `Glossary()` |
| Skill shape + composition | `templates/skills/stack/SKILL.md.tmpl` (Propose/Refine, invoked by prd) | `templates/skills/glossary/SKILL.md.tmpl` |
| User-facing command | `templates/commands/prd.md.tmpl` | `templates/commands/glossary.md.tmpl` |
| Managed CLAUDE.md | `internal/cli/claudemd.go` | the language moment (R7.2) |
| Sibling plan mechanics | `docs/plans/PLAN-decision-records.md` (M1 contract / M2 discipline, well-formedness-in-validate precedent) | same two-milestone shape |

### 5.4 The glossary grammar (deterministic)
- Sections: everything outside `## Language` is free prose (parser ignores — the
  plan.md discipline). Inside it, an entry is:
  `**<Term>**:` → definition prose (≥1 line) → optional `_Avoid_: a, b, c`.
- Matching: identifiers are tokenized on `-`/`_`; a term or alias matches on equal
  normalized whole tokens; multi-word terms match contiguous token runs
  (`purchase order` ⇢ `purchase-order-import`). Substring matches never count —
  `client` does not match `clientele`.
- Collisions (R1.3) are findings, not silent precedence: the glossary is small and
  human-curated; ambiguity is fixed at the source.

### 5.5 The discipline (stated once, in the skill)
The glossary skill owns challenge/sharpen/scenario/record; the prd, quick-prd and wiki
templates gain only an *invocation hook* ("route terminology through the glossary
skill"), never the rules themselves — the single-owner answer to the format-file drift
bug. Sharpening reuses the decision-grill interaction form (one term, a recommended
canonical, the human decides) without the ADR gate: terms are cheap to record and the
glossary is its own registry. Where a naming choice *is* gate-positive (a bounded
concept split, a deliberate divergence from industry vocabulary), the skill hands it
to the decision grill and the ADR cites the term — the two contracts compose instead
of overlapping.

### 5.6 CLI surface
```text
(unchanged)
csdd plan validate <slug>   # gains: avoided-term in feat/plan slugs,
                            #        glossary well-formedness
csdd graph build            # gains: term nodes, references edges (alias-attributed)
csdd graph analyze          # gains: avoided-term in spec/wiki identifiers,
                            #        orphan-term (informational)
csdd wiki lint              # gains: the wiki subset of the above, for free (filtered)
```

### 5.7 File structure plan
```text
internal/glossary/
├── glossary.go        # Parse, Term, normalize, Match                [Glossary]
└── glossary_test.go   # table-driven; fixtures in testdata/
internal/graph/extract_glossary.go  # term nodes + references edges  [Extract]
internal/templater/templates/skills/glossary/SKILL.md.tmpl           [Templates]
internal/templater/templates/commands/glossary.md.tmpl               [Templates]
docs/glossary.md                    # dogfood: csdd's own language (task 6)
```
**Modified:** `internal/plan/validate.go` (findings; drop the local normalizer in
favor of `internal/glossary`'s if extraction is clean), `internal/paths/paths.go`,
`internal/graph` (vocab + analyze), `internal/cli/claudemd.go`, templates:
`skills/prd/SKILL.md.tmpl`, `skills/quick-prd/SKILL.md.tmpl`,
`skills/wiki/SKILL.md.tmpl` (hooks), `root/knowledge-section.md.tmpl`.

### 5.8 Requirements Traceability
| Requirement | Components | Interfaces | Flows |
|---|---|---|---|
| 1.1, 1.2, 1.3 | Glossary | Parse(), Match() | validate/analyze/build |
| 2.1, 2.2, 2.3 | Validate | ValidatePlan() | validate |
| 3.1, 3.2 | Extract | AnalyzeGaps() | analyze/wiki-lint |
| 4.1, 4.2 | Extract | Extract() | build |
| 5.1, 5.2, 5.3, 5.4 | Templates | glossary skill | authoring |
| 6.1, 6.2, 6.3, 6.4 | Templates | prd/quick-prd/wiki hooks, /glossary | authoring |
| 7.1, 7.2 | Templates, CLI | knowledge-section, claudemd | init/update |

---

## 6. Tasks (content of `tasks.md`) — TDD, boundaries, dependencies

### Phase 1: The language contract (M1)
- [ ] 1. Glossary package _Boundary: Glossary_
  - [ ] 1.1 RED — fixtures: entry grammar good/bad, clusters, duplicate terms, alias
    collisions, empty definitions, normalization, whole-token + multi-word matching
    (incl. the `clientele` non-match)
    - _Requirements: 1.1, 1.2, 1.3_
  - [ ] 1.2 GREEN — `internal/glossary` + `paths.Glossary`
    - _Requirements: 1.1, 1.2, 1.3_
- [ ] 2. Validate findings _Boundary: Validate_
  - [ ] 2.1 RED — avoided-term in feat/plan slugs (canonical named in the message),
    well-formedness surfaced, absent-glossary = silent, exit codes
    - _Requirements: 2.1, 2.2, 2.3_
    - _Depends: 1.2_
  - [ ] 2.2 GREEN — `validate.go` wiring
    - _Requirements: 2.1, 2.2, 2.3_
- [ ] 3. Graph (P) _Boundary: Extract_
  - [ ] 3.1 RED — term nodes with attrs; `references` edges from feat/spec/wiki
    identifiers (alias-attributed); avoided-term findings domain-tagged (wiki subset
    renders in `wiki lint`); orphan-term informational
    - _Requirements: 3.1, 3.2, 4.1, 4.2_
    - _Depends: 1.2_
  - [ ] 3.2 GREEN — `extract_glossary.go` + `TypeTerm` vocab + analyze rules
    - _Requirements: 3.1, 3.2, 4.1, 4.2_

### Phase 2: The discipline (M2 — template-only, rides on M1)
- [ ] 4. The glossary skill + `/glossary` _Boundary: Templates_
  - [ ] 4.1 RED — installed-skill assertions: the four moves; inline-write + lazy
    creation; the tombstone rule; graph-query-before-asking; contract purity rules;
    gate-positive naming hands off to the decision grill
    - _Requirements: 5.1, 5.2, 5.3, 5.4_
  - [ ] 4.2 GREEN — `skills/glossary/SKILL.md.tmpl` + `commands/glossary.md.tmpl`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 6.4_
- [ ] 5. Flow hooks + satellites (P) _Boundary: Templates_
  - [ ] 5.1 RED — prd Draft/Decompose hooks, quick-prd hook, wiki Ingest hook —
    invocation only, no duplicated rules; knowledge-section row; claudemd moment
    - _Requirements: 6.1, 6.2, 6.3, 7.1, 7.2_
  - [ ] 5.2 GREEN — hook edits + `knowledge-section.md.tmpl` + `claudemd.go`
    - _Requirements: 6.1, 6.2, 6.3, 7.1, 7.2_
- [ ] 6. Dogfood seed + e2e
  - [ ] 6.1 — author csdd's own `docs/glossary.md` (~10 entries: plan, feat, spec,
    seed, brief, quality gate, steering, wiki page, stack row, ADR — with tombstones
    like *feature/story* → feat, *PRD* → plan where the distinction bites); e2e on a
    fixture workspace: an avoided feat slug fails `plan validate`, an avoided wiki
    page name surfaces in `wiki lint`, term nodes answer `graph query`, and the
    orphan-term informational fires
    - _Requirements: 1.1, 2.1, 3.1, 3.2, 4.2_
    - _Depends: 2.2, 3.2, 4.2_

**Execution order:** 1 → {2, 3} → {4, 5} → 6.

---

## 7. Milestones

| Milestone | Delivery | Tasks |
|---|---|---|
| **M1 — The language contract** | `docs/glossary.md` parses and lints; avoided terms caught in `plan validate`/`graph analyze`/`wiki lint`; terms queryable in the graph — a hand-written glossary is first-class before any skill ships | 1–3 |
| **M2 — The discipline** | glossary skill + `/glossary`, hooks in prd/quick-prd/wiki, satellites; csdd's own glossary dogfooded | 4–6 |
| **Post-v1** | `CONTEXT-MAP.md` multi-context; alias-aware `[[wiki-ref]]` resolution; glossary-driven entity dedup in the extractor; prose-tier warnings if identifier lint proves too narrow; term pages in the web dashboard | — |

---

## 8. Risks and decisions

- **Generic `_Avoid_` words → false positives (bounded by design).** Whole-token
  matching only, never substrings; findings name the canonical term so triage is one
  glance; the glossary is human-curated — an alias too generic to ban is deleted with
  a one-line edit. If a real repo still hurts, a per-alias opt-out beats loosening the
  matcher.
- **Prose drift stays invisible to the machine (accepted, v1).** The deliberate
  boundary of principle 3: the skill challenges prose, the linter guards identifiers.
  Widening to prose warnings is a post-v1 call made on evidence, not speculation.
- **Two disciplines could blur (composed instead).** Terms are cheap (no gate,
  glossary is the registry); decisions are gated (ADR). The skill's hand-off rule
  (§5.5) keeps one boundary: *what we call it* → glossary; *why we chose it* → ADR
  citing the term.
- **In-flight substrate (sequenced, not risky).** This plan layers on the plan-mode
  branch and touches the same `validate.go` seam as PLAN-decision-records — land the
  three in order (plan-mode → decision-records → glossary), never parallel PRs on the
  same files.
- **Skill-invocation timing (the domain-modeling structural limit).** Challenge
  moments need the skill in context; mitigation is placement, not hope — hooks live in
  the three flows where names are born (prd, quick-prd, wiki) plus the claudemd
  moment for everything else.
- **`normalizeTechName` vs `normalizeTerm` (one home).** The normalizer moves to
  `internal/glossary` only if the extraction is mechanical; duplicating three lines of
  regex beats a tangled import graph — decided at task 1.2, noted here so the
  reviewer expects either.

---

## 9. Appendices

### 9.1 References
- `docs/plans/PLAN-decision-records.md` — the sibling contract (ADR 0003 records this
  plan's deferral) and the two-milestone shape this mirrors.
- `docs/plans/PLAN-plan-mode.md` / `docs/plans/PLAN-knowledge-base.md` — substrate and
  graph vocabulary.
- github.com/mattpocock/skills — `skills/engineering/domain-modeling/` (`SKILL.md`,
  `CONTEXT-FORMAT.md`), studied 2026-07-07.
- Eric Evans, *Domain-Driven Design* (2003) — the ubiquitous-language canon the
  CONTEXT.md format condenses.

### 9.2 Executor notes (this repo / workstation)
- **Verify:** `make check` (gofmt + vet + race). Distribution gate: `CGO_ENABLED=0`
  via `make dist`.
- **CLI pattern:** stdlib `flag` + subcommand switch in `internal/cli/cli.go`;
  `--json` via the `jsonout` helpers; table-driven tests beside each file.
- **Line endings:** CRLF working tree over LF index on this workstation — stage only
  the paths you changed; never `git add -A`. Glossary fixtures and templates must be
  written LF.
- **Flaky tests:** four update/clean `.old`-backup CLI tests fail non-deterministically
  on `/mnt/c` (WSL) — re-run before treating as regression.
- **Go toolchain:** `~/.local/go/bin` (export PATH + GOPATH/GOCACHE).
- **Before starting:** `git fetch origin` and check open PRs — parallel sessions land
  related work; this plan's seams are on an in-flight branch shared with two sibling
  plans.
- **Commits/PRs:** conventional style (`feat(glossary): …`); repo templates forbid AI
  attribution in commits/PRs.
