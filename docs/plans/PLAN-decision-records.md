# Master Plan — Decision Records (`docs/adr/` + the PRD decision grill)

> **Audience:** the LLM agent (and humans) implementing this feature end-to-end.
> **Status:** draft for review — revision 1 (2026-07-07). Not yet approved.
> **Companions:** `docs/plans/PLAN-plan-mode.md` (the substrate — this feature extends
> its plan grammar, validate, brief and graph seams; its code is in flight on this
> branch); `docs/plans/PLAN-knowledge-base.md` (graph vocabulary this feature adds to);
> github.com/mattpocock/skills — `productivity/grilling` and
> `engineering/domain-modeling` (the two skills this design synthesizes, studied
> 2026-07-07).
> **Dogfood note:** the three decisions locked in this feature's own design session
> (2026-07-07) — ADR storage + `adr:` token, the two-tier interview, glossary deferred —
> each pass the triple gate below. Task 8 records them as `docs/adr/0001..0003`: the
> first ADRs ever written, and the fixtures the lint is proven against.

---

## 1. Context and vision

### 1.1 What we are building

csdd's plan mode already treats one class of decision as a first-class object:
**technology**. The Stack consult pass elicits options, a human decides, the decision
lands as a `docs/stack.md` Decided row, feats cite it as `stack:<name>`, and
`csdd plan validate` breaks on an uncited adoption. Elicitation → record → lintable
reference — the full loop.

Every **other** load-bearing decision — domain shape, boundaries between feats,
integration patterns, deliberate deviations from the obvious path, scope trade-offs —
has three destinations today, all of which rot the project long-term:

1. **"Context and vision" prose** — the parser ignores it; when the plan becomes specs
   and runner sessions execute, the *why* does not travel with the *what*.
2. **`[ASSUMPTION]` markers** — nothing forces resolution before Present; an assumption
   that survives approval is a decision made by nobody.
3. **Nowhere** — the Draft interview's batched multiple-choice ("1B, 2A, 3C") is
   optimized for speed; decisions with dependencies between them pass bundled with
   scope facts, undiscussed.

This plan generalizes the stack pattern to **all gate-positive decisions**:

```text
Decision (passes the triple gate)  →  ADR (docs/adr/NNNN-<slug>.md)
    →  citation (adr:<slug> in a feat's Refs)  →  brief (inlined for every session)
```

**The triple gate is the admission contract.** A decision becomes an ADR only when all
three hold: **hard to reverse** (the cost of changing your mind later is meaningful),
**surprising without context** (a future reader will wonder "why did they do it this
way?"), and **the result of a real trade-off** (there were genuine alternatives). If
any is missing, it is prose or a cheap assumption — never an ADR. This is what designs
out ADR spam.

### 1.2 The skills synthesis (what we take, what csdd already has better)

The design merges two studied skills. csdd adopts each practice and upgrades it with
mechanics the skills lack:

| Skill practice | csdd adaptation |
|---|---|
| grilling: one question at a time, each with a recommended answer | the **Decision grill** tier of Draft — sequential only for gate-positive decisions; scope facts stay batched (csdd's batch already beats grilling for facts) |
| grilling: facts from the repo, decisions from the user | already prd law ("source facts; do not re-ask what is recorded"); now enforced on the decisions side too |
| grilling: do not enact until confirmed | already exists and stronger: `csdd plan approve` is hash-bound |
| grilling: shared understanding lives only in the conversation (its known gap) | closed: every resolved decision is an ADR file, immune to context loss |
| domain-modeling: triple-gate ADR admission | adopted verbatim as the admission contract |
| domain-modeling: minimal ADR format (`NNNN-slug.md`, 1–3 sentences) | adopted, plus a **lintable citation token** (`adr:<slug>`) the format alone lacks |
| domain-modeling: write the record the moment it crystallises, never batched | the inline-write rule of the grill |
| domain-modeling: CONTEXT.md glossary / ubiquitous language | **deferred** — own phase, designed with the graph (canonical entity aliases) |

### 1.3 Value thesis (why this build order)

- **The contract pays before the discipline exists.** Once `adr:` resolves in
  `validate` and `docs/adr/` lints (M1), hand-written ADRs are already first-class —
  citable, checkable, brief-inlined — with no skill change shipped.
- **The brief is where the feature earns its keep.** PLAN-plan-mode's law is "anything
  not covered by a contract is an open decision — the loop surfaces it and stops."
  ADRs widen contract coverage from *technology* to *design*: a runner session
  receives the why inline and is forbidden from silently re-deciding it.
- **The discipline (M2) is template-only work** riding on a proven contract: the prd
  skill's two-tier interview and the assumption sweep change authored prose, not Go.

---

## 2. Storage (extends the project-wide convention, revision 3)

| Path | Role | Author |
|---|---|---|
| `docs/adr/NNNN-<slug>.md` | One decision record: title + 1–3 sentences (context, decision, why); optional `status`/`superseded-by` frontmatter. | Human/LLM prose (the skill writes it); linted by the CLI, never written by it. |

Decisions are **project-scoped, never plan-scoped** (the `docs/stack.md` precedent): a
decision made while authoring plan X must be visible when plan Y is authored — burying
it in `docs/plans/<slug>/` recreates the failure mode this feature removes. Numbering
is **append-only**; files are never renumbered.

---

## 3. Design principles (inviolable)

1. **The triple gate is the admission contract.** Hard to reverse + surprising without
   context + real trade-off — all three, or it is not an ADR. The skill states the
   gate verbatim; the reviewer enforces it at approve.
2. **The CLI never authors decisions.** The skill writes ADR files (authored prose,
   like `plan.md`); the CLI lints them. Numbering is a mechanical convention
   (scan highest + 1) covered by lint, not by generation — no `csdd adr` command
   group in v1.
3. **Decisions outlive their plan.** Project-level `docs/adr/`, one flat sequence.
4. **The why travels with the what.** A cited ADR is inlined in full into every brief
   for that feat; a gate-positive decision not covered by an ADR or stack row is an
   open decision — the session stops, it never improvises.
5. **Grill decisions, look up facts.** Anything recoverable from steering, `docs/`,
   `specs/` or code is never a question; anything gate-positive is never bundled into
   a batch.
6. **Aggressive reuse.** `adr:` is a third ref token beside `stack:` and `[[wiki]]` —
   same tokenizer, same validate pass, same brief assembly, same graph seam. No second
   mechanism for a solved problem.

---

## 4. Requirements (content of `requirements.md`)

EARS format. Requirement IDs in annotations are explicit comma-separated lists.

### Requirement 1: The ADR contract
**Objective:** As the toolchain, I want decision records machine-checkable where it
matters, so citations resolve and the record set stays sound.
**Acceptance Criteria**
1. THE SYSTEM SHALL treat as an ADR every `docs/adr/` file matching
   `NNNN-<slug>.md` (NNNN four-digit zero-padded, slug kebab-case), whose content is
   an optional frontmatter block followed by a `# <title>` heading and free prose.
2. THE SYSTEM SHALL parse optional frontmatter keys `status`
   (`accepted` when absent | `superseded`) and `superseded-by: <NNNN>`.
3. THE SYSTEM SHALL resolve a slug to at most one file; body length (1–3 sentences)
   is a skill convention, not a lint.

### Requirement 2: `adr:` refs in the plan grammar
**Objective:** As the plan parser, I want ADR citations tokenized like stack and wiki
refs, so downstream passes work from explicit structure.
**Acceptance Criteria**
1. WHEN a plan's `Refs` cell contains `adr:<slug>` THEN the system SHALL tokenize it
   into the feat's `ADRRefs` list, preserving the verbatim token in `Refs`
   (the `model.go` superset contract).
2. IF an `adr:` token's slug is empty or not kebab-case THEN `validate` SHALL report
   a malformed decision ref.

### Requirement 3: ADR lint (`csdd plan validate`)
**Objective:** As a reviewer (human or runner), I want broken decision wiring surfaced
mechanically, so the approve gate acts on sound citations.
**Acceptance Criteria**
1. THE SYSTEM SHALL report every `adr:<slug>` ref with no matching `docs/adr/` file
   ("broken decision ref") and every slug matching two or more files
   ("ambiguous decision ref").
2. WHERE `docs/adr/` exists THE SYSTEM SHALL report: malformed filenames, duplicate
   numbers, a missing `# <title>` heading, and a `superseded-by` target that does not
   exist ("dangling supersession").
3. THE SYSTEM SHALL report a feat citing an ADR whose status is `superseded`
   ("cites superseded decision — cite its successor").
4. IF any violation is found THEN the system SHALL exit non-zero (the R3.5 precedent
   of PLAN-plan-mode).

### Requirement 4: ADRs in the brief (`csdd plan brief`)
**Objective:** As a fresh runner session, I want the decisions my feat depends on
inline in my context pack, so I execute the why instead of re-deriving it.
**Acceptance Criteria**
1. WHEN `brief` assembles a step for a feat with `ADRRefs` THEN the system SHALL
   inline each cited ADR **in full** (title + body — they are short by format, the
   stack-row treatment, not the wiki path-only treatment).
2. WHERE a cited ADR does not resolve THE SYSTEM SHALL emit a WARNING line (the
   broken-wiki-ref precedent in `brief.go`).
3. THE brief's forbidden-actions block SHALL state: making a decision that passes the
   triple gate and is not covered by a cited ADR or stack row is an **open decision** —
   report `blocked` with a revision proposal; never improvise.
4. THE SYSTEM SHALL remain byte-deterministic (R7.3 of PLAN-plan-mode).

### Requirement 5: The Decision grill (prd skill, Draft pass)
**Objective:** As the plan author, I want load-bearing decisions discussed one at a
time and recorded the moment they resolve, so nothing structural passes bundled.
**Acceptance Criteria**
1. THE prd SKILL SHALL run Draft in two tiers: the existing batched lettered
   multiple-choice for scope facts, then a **Decision grill** for every candidate that
   passes the triple gate (stated verbatim in the skill).
2. THE grill SHALL put decisions to the human **one at a time**, each with a
   recommended answer, ordered so decisions that unblock other decisions come first.
3. WHEN a decision is resolved THEN the skill SHALL write its ADR **immediately**
   (scan-highest + 1 numbering; never batched) and cite it from the affected feats'
   `Refs` at Decompose.
4. THE SKILL SHALL look up facts (steering, `docs/stack.md`, `specs/`, code) instead
   of asking, and SHALL never put a gate-negative question through the grill.

### Requirement 6: Assumption discipline (prd skill, Present pass)
**Objective:** As the approver, I want no unowned structural decisions hiding as
assumptions, so approval means every trade-off was either discussed or knowingly
deferred.
**Acceptance Criteria**
1. WHEN the Present pass begins THEN the skill SHALL sweep every remaining
   `[ASSUMPTION]`; each that passes the triple gate SHALL be grilled into an ADR or
   explicitly deferred by the human and re-marked `[DEFERRED-BY-HUMAN]` in `plan.md`.
2. THE SKILL SHALL NOT present a plan for approval while a gate-positive
   `[ASSUMPTION]` survives unmarked.

### Requirement 7: The Revise funnel (prd skill)
**Objective:** As the plan owner, I want run deviations that embody new decisions to
leave the same trail as authoring-time decisions, so mid-flight choices are not
second-class.
**Acceptance Criteria**
1. WHEN Revise processes a `log.md` deviation whose revision proposal embodies a
   gate-positive decision THEN the skill SHALL grill it and write a new ADR — or mark
   the superseded one (`status: superseded`, `superseded-by`) and write its
   successor — **before** amending `plan.md` for re-approval.

### Requirement 8: quick-prd, the light gate
**Objective:** As a single-feature author, I want the same admission contract without
the multi-feat ceremony, so small work leaves the same trail.
**Acceptance Criteria**
1. THE quick-prd SKILL SHALL state the triple gate and SHALL record every
   gate-positive decision to `docs/adr/` in the same format (no sequential grill tier —
   at single-feature scale decisions surface organically).
2. THE produced PRD SHALL reference its ADRs so `/prd`'s Draft inherits them as
   recorded decisions, not re-asked questions.

### Requirement 9: ADRs in the graph
**Objective:** As a knowledge-base user, I want decisions in the same graph as plans,
specs and code, so "why" is one hop from "what".
**Acceptance Criteria**
1. WHEN `csdd graph build` runs THEN the system SHALL emit an `adr` node per record
   (id from slug; attrs: number, title, status) with edges: feat `cites` adr, and adr
   `superseded_by` adr.
2. WHEN `csdd graph analyze` runs THEN the system SHALL surface broken/ambiguous
   decision refs alongside the existing plan-ref lints, plus an informational
   "orphan decision" (no inbound `cites`) — decisions may legitimately predate plans;
   informational only.

### Requirement 10: Satellites (templates + managed docs)
**Objective:** As a csdd workspace, I want the convention visible everywhere authors
look, so the mechanism is discoverable without reading this plan.
**Acceptance Criteria**
1. THE plan template's `Refs` column comment SHALL document the `adr:<slug>` token.
2. THE root knowledge section SHALL gain the `docs/adr/` row of §2.
3. THE managed CLAUDE.md section SHALL gain the moment: decisions passing the triple
   gate become ADRs — never decide silently, never edit a superseded record.
4. THE `/prd` command template SHALL describe the two-tier Draft and the ADR trail.

---

## 5. Design (content of `design.md`)

### 5.1 Overview
One new file in the in-flight plan package (`internal/plan/adr.go` — scan, parse,
resolve) + surgical touches to `model.go`/`parse.go`/`validate.go`/`brief.go` + one
graph extractor (`internal/graph/extract_adr.go`) + one paths helper + template edits.
**Zero new CLI commands** — `validate`, `brief`, and `graph` gain behavior behind
their existing surfaces.

### 5.2 Goals / Non-Goals
**Goals:** lintable decision records; `adr:` as the third ref token; broken/ambiguous/
superseded citation lint; ADRs inlined in briefs with the open-decision rule; the
two-tier Draft grill; the assumption sweep; the Revise funnel; quick-prd's light gate;
adr nodes and `cites` edges in the graph.
**Non-Goals (v1):** a `csdd adr` command group (numbering is lint-covered; promote to
`csdd adr new` only if drift shows up in practice); glossary / `CONTEXT.md` /
ubiquitous language (own phase, designed with the graph); ADR refs from spec artifacts
(plan-level only — briefs carry them into sessions); a status lifecycle beyond
`superseded-by` (one hop, no state machine); migrating legacy decisions out of
existing prose; rendering ADRs in the web dashboard.

### 5.3 Reuse map (verified against the current tree)
| Need | Exists in | Use |
|---|---|---|
| Ref token pattern | `internal/plan/parse.go` (`stackRefPrefix`, `reWikiRef`; Refs tokenized ~L174) | add `adrRefPrefix = "adr:"` beside them |
| Refs model contract | `internal/plan/model.go:46-48` (`WikiRefs`/`StackRefs`/`Refs` superset) | add `ADRRefs []string` |
| Ref resolution findings | `internal/plan/validate.go:101-117` + normalize helpers (~L313-368) | ADR resolution findings in the same pass |
| Full-row inlining | `internal/plan/brief.go:67-84` (stack rows §3) | ADR bodies get the same treatment |
| Broken-ref WARNING | `internal/plan/brief.go:91` (wiki) | mirror for `adr:` |
| Forbidden-actions text | `internal/plan/brief.go:20` (stack line) | add the open-decision line (R4.3) |
| Graph extractor seam | `internal/graph/extract_plan.go` (`references`/`uses_tech` edges) | `extract_adr.go` + `cites`/`superseded_by` |
| Paths | `internal/paths` (`DocsPlans`/`DocsWiki` pattern) | `DocsADR` |
| Frontmatter | `internal/frontmatter` | ADR `status`/`superseded-by` |
| Templates | `internal/templater` (embedded, LF-normalized) | skill/command/plan/knowledge edits |
| Managed CLAUDE.md | `internal/cli/claudemd.go` | the decision moment (R10.3) |

### 5.4 The ADR grammar (deterministic)
- Filename: `^\d{4}-[a-z0-9]+(?:-[a-z0-9]+)*\.md$`. Number = identity (stable,
  append-only); slug = citation currency.
- Content: optional frontmatter (`status`, `superseded-by`), then the first `# ` line
  is the title; everything after is the body (convention: 1–3 sentences — context,
  decision, why).
- Resolution: strip the `NNNN-` prefix → slug → file. Two files sharing a slug is an
  ambiguity finding (rename the newer before merge; citations bind to slugs, so a
  rename is caught as a broken ref, never silent).
- `superseded-by` targets a **number** (file identity), not a slug.

### 5.5 Skill mechanics (the discipline, stated once)
The triple gate lives verbatim in the prd skill's Decision grill section; quick-prd
carries the same three bullets (accepted duplication — installed skills are
self-contained files; the gate is three lines and stable). The grill's rules: one
decision per exchange, recommendation first, dependency order, ADR written before the
next question is asked (the domain-modeling inline rule), citation wired at Decompose.
The Present sweep (R6) and Revise funnel (R7) close the two leaks that survive
authoring: assumptions and mid-run deviations.

### 5.6 CLI surface
```text
(unchanged)
csdd plan validate <slug>   # gains: broken/ambiguous/malformed decision refs,
                            #        docs/adr well-formedness, cites-superseded
csdd plan brief <slug>      # gains: "## Decisions (docs/adr — the why)" section
csdd graph build|analyze    # gains: adr nodes, cites/superseded_by, orphan lint
```

### 5.7 File structure plan
```text
internal/plan/adr.go            # scan docs/adr, parse, resolve slugs   [ADR]
internal/plan/adr_test.go       # table-driven; fixtures in testdata/
internal/graph/extract_adr.go   # adr nodes + cites/superseded_by       [Extract]
docs/adr/0001..0003-*.md        # dogfood: this feature's own decisions (task 8)
```
**Modified:** `internal/plan/{model,parse,validate,brief}.go` (+ tests),
`internal/paths/paths.go` (`DocsADR`), `internal/graph` (vocab + analyze),
`internal/cli/claudemd.go` (moment), templates:
`skills/prd/SKILL.md.tmpl`, `skills/quick-prd/SKILL.md.tmpl`,
`commands/prd.md.tmpl`, `plan/plan.md.tmpl` (Refs comment),
`root/knowledge-section.md.tmpl` (storage row).

### 5.8 Requirements Traceability
| Requirement | Components | Interfaces | Flows |
|---|---|---|---|
| 1.1, 1.2, 1.3 | ADR | ScanADRs(), ResolveADR() | validate/brief/graph |
| 2.1, 2.2 | Parse, Model | ParsePlan() | all |
| 3.1, 3.2, 3.3, 3.4 | Validate | ValidatePlan() | validate |
| 4.1, 4.2, 4.3, 4.4 | Brief | Brief() | brief |
| 5.1, 5.2, 5.3, 5.4 | Templates | prd skill Draft | authoring |
| 6.1, 6.2 | Templates | prd skill Present | authoring |
| 7.1 | Templates | prd skill Revise | revise |
| 8.1, 8.2 | Templates | quick-prd skill | authoring |
| 9.1, 9.2 | Extract | Extract(), AnalyzeGaps() | build/analyze |
| 10.1, 10.2, 10.3, 10.4 | Templates, CLI | templater, claudemd | init/update |

---

## 6. Tasks (content of `tasks.md`) — TDD, boundaries, dependencies

### Phase 1: The decision contract (M1)
- [ ] 1. ADR store _Boundary: ADR_
  - [ ] 1.1 RED — fixtures: filename grammar good/bad, duplicate numbers, frontmatter
    (`status`, `superseded-by`), missing title, dangling supersession, slug
    resolution + ambiguity
    - _Requirements: 1.1, 1.2, 1.3_
  - [ ] 1.2 GREEN — `internal/plan/adr.go` + `paths.DocsADR`
    - _Requirements: 1.1, 1.2, 1.3_
- [ ] 2. `adr:` in the plan grammar (P) _Boundary: Parse_
  - [ ] 2.1 RED — tokenizer: `adr:<slug>` → `ADRRefs`; verbatim `Refs` superset holds;
    malformed slug surfaced
    - _Requirements: 2.1, 2.2_
  - [ ] 2.2 GREEN — `parse.go` `adrRefPrefix` + `model.go` `ADRRefs`
    - _Requirements: 2.1, 2.2_
- [ ] 3. Validate findings _Boundary: Validate_
  - [ ] 3.1 RED — each R3 finding class incl. cites-superseded; exit codes
    - _Requirements: 3.1, 3.2, 3.3, 3.4_
    - _Depends: 1.2, 2.2_
  - [ ] 3.2 GREEN — `validate.go` wiring
    - _Requirements: 3.1, 3.2, 3.3, 3.4_
- [ ] 4. Brief inlining (P) _Boundary: Brief_
  - [ ] 4.1 RED — cited ADR inlined in full; WARNING on broken ref; open-decision
    forbidden-actions line present; byte-determinism (double-run equality)
    - _Requirements: 4.1, 4.2, 4.3, 4.4_
    - _Depends: 1.2, 2.2_
  - [ ] 4.2 GREEN — `brief.go`
    - _Requirements: 4.1, 4.2, 4.3, 4.4_
- [ ] 5. Graph (P) _Boundary: Extract_
  - [ ] 5.1 RED — adr nodes with attrs, `cites`/`superseded_by` edges, orphan-decision
    informational, broken-ref lint parity with wiki refs
    - _Requirements: 9.1, 9.2_
    - _Depends: 1.2, 2.2_
  - [ ] 5.2 GREEN — `extract_adr.go` + vocab + analyze rules
    - _Requirements: 9.1, 9.2_

### Phase 2: The discipline (M2 — template-only, rides on M1)
- [ ] 6. prd skill: the Decision grill _Boundary: Templates_
  - [ ] 6.1 RED — installed-skill assertions: triple gate verbatim; grill rules
    (one-at-a-time, recommendation, dependency order, inline ADR write); two-tier
    Draft intact (batch tier unchanged); Present sweep + `[DEFERRED-BY-HUMAN]`;
    Revise funnel incl. supersession; `adr:` in Decompose/completion criteria
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 6.1, 6.2, 7.1_
  - [ ] 6.2 GREEN — `skills/prd/SKILL.md.tmpl` + `commands/prd.md.tmpl`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 6.1, 6.2, 7.1, 10.4_
- [ ] 7. quick-prd + satellites (P) _Boundary: Templates_
  - [ ] 7.1 RED — quick-prd gate + record rule + PRD-references-ADRs; plan template
    Refs comment; knowledge-section row; claudemd moment
    - _Requirements: 8.1, 8.2, 10.1, 10.2, 10.3_
  - [ ] 7.2 GREEN — `skills/quick-prd/SKILL.md.tmpl`, `plan/plan.md.tmpl`,
    `root/knowledge-section.md.tmpl`, `claudemd.go`
    - _Requirements: 8.1, 8.2, 10.1, 10.2, 10.3_
- [ ] 8. Dogfood seed + e2e
  - [ ] 8.1 — author `docs/adr/0001-decision-records-storage.md` (docs/adr + `adr:`
    token over wiki-page and plan-scoped alternatives),
    `0002-two-tier-interview.md` (batch facts / grill decisions over pure-grilling
    and batch-only), `0003-glossary-deferred.md` (ubiquitous language is its own
    phase — the explicit no); e2e on a fixture workspace: a plan citing them
    validates clean; broken/ambiguous/superseded variants produce their findings;
    the brief inlines 0001 in full
    - _Requirements: 1.1, 3.1, 3.3, 4.1_
    - _Depends: 3.2, 4.2, 6.2_

**Execution order:** {1, 2} → 3 → {4, 5} → {6, 7} → 8.

---

## 7. Milestones

| Milestone | Delivery | Tasks |
|---|---|---|
| **M1 — The decision contract** | `docs/adr/` lints, `adr:` resolves in validate, briefs carry the why, decisions in the graph — hand-written ADRs are first-class before any skill changes | 1–5 |
| **M2 — The discipline** | two-tier Draft grill, assumption sweep, Revise funnel, quick-prd gate, satellites; dogfood ADRs 0001–0003 recorded and linted | 6–8 |
| **Post-v1** | `csdd adr new` scaffold (if numbering drift shows up); glossary/`CONTEXT.md` phase (canonical aliases feeding the graph extractor); `adr:` refs from spec artifacts; ADRs in the web dashboard; wiki page ⇄ ADR cross-linking | — |

---

## 8. Risks and decisions

- **Gate applied loosely → ADR spam (the anti-goal).** Mitigation: the gate is stated
  verbatim in both skills with the domain-modeling contrapositives ("if it's easy to
  reverse, you'll just reverse it; if it's not surprising, nobody will wonder; if
  there was no alternative, there is nothing to record"), and the completion criteria
  make a gate-negative ADR a review finding.
- **Gate text duplicated across prd and quick-prd (accepted).** Installed skills are
  self-contained files; the gate is three stable bullets. The single-source
  alternative (a shared format file) was rejected — it reintroduces the
  domain-modeling repo's own drift bug across an install boundary.
- **Slug ambiguity as `docs/adr/` grows (lint-bounded).** Ambiguity is a hard finding;
  slugs are human-chosen at write time when the namespace is visible.
- **In-flight substrate (sequenced, not risky).** This plan edits files that exist on
  this branch but are not on main (`internal/plan/*`, `extract_plan.go`). Tasks 1–5
  land after (or squashed with) PLAN-plan-mode M1/M3 — never in parallel PRs against
  the same seams.
- **Supersession is one hop, not a lifecycle (kept minimal).** `superseded-by`
  answers "where is the current decision"; chains resolve by hopping. A status
  machine is post-v1 if ever.
- **Interview cost (bounded by design).** The grill applies only to gate-positive
  candidates — typically 2–5 per initiative; scope facts stay batched. Pure
  grilling's cost was the reason for the two-tier split (ADR 0002).
- **Body length is a convention, not a lint** (the right-sizing precedent of
  PLAN-plan-mode §8): the skill teaches 1–3 sentences; a length lint may come later
  if records bloat.

---

## 9. Appendices

### 9.1 References
- `docs/plans/PLAN-plan-mode.md` — the substrate: plan grammar, validate, brief,
  runner, and the contract-chain law this feature extends to design decisions.
- `docs/plans/PLAN-knowledge-base.md` — graph vocabulary and extractor seam.
- github.com/mattpocock/skills — `skills/productivity/grilling/SKILL.md` (the
  interview mechanics) and `skills/engineering/domain-modeling/SKILL.md` (+
  `ADR-FORMAT.md`, `CONTEXT-FORMAT.md`) — studied 2026-07-07.
- Michael Nygard, "Documenting Architecture Decisions" (cognitect.com, 2011) — the
  ADR canon the domain-modeling format minimizes.

### 9.2 Executor notes (this repo / workstation)
- **Verify:** `make check` (gofmt + vet + race). Distribution gate: `CGO_ENABLED=0`
  via `make dist`.
- **CLI pattern:** stdlib `flag` + subcommand switch in `internal/cli/cli.go`;
  `--json` via the `jsonout` helpers; table-driven tests beside each file.
- **Line endings:** CRLF working tree over LF index on this workstation — stage only
  the paths you changed; never `git add -A`. ADR fixtures and templates must be
  written LF.
- **Flaky tests:** four update/clean `.old`-backup CLI tests fail non-deterministically
  on `/mnt/c` (WSL) — re-run before treating as regression.
- **Go toolchain:** `~/.local/go/bin` (export PATH + GOPATH/GOCACHE).
- **Before starting:** `git fetch origin` and check open PRs — parallel sessions land
  related work; this plan's seams are on an in-flight branch.
- **Commits/PRs:** conventional style (`feat(plan): …`); repo templates forbid AI
  attribution in commits/PRs.
