# Glossary — csdd's ubiquitous language

This is the project's **ubiquitous-language contract**: one canonical term per
domain concept, defined as what it **is**, with an `_Avoid_` list of the synonyms
it bans. Identifiers (feat slugs, spec directories, wiki page names) are minted
from canonical terms; `csdd plan validate`, `csdd graph analyze`, and
`csdd wiki lint` report an avoided term used as a whole token. Prose outside the
`## Language` section is ignored by the parser.

## Language

### Planning

**Plan**: The structured document above specs (`docs/plans/<slug>/`) that
decomposes an initiative into feats; each feat becomes exactly one spec.
_Avoid_: prd, epic

**Feat**: One row of a plan's Feats table — a spec-sized chunk of work with an
explicit objective, dependencies, and refs; it becomes exactly one spec.
_Avoid_: feature, story, ticket

**Spec**: The per-feature contract under `specs/<name>/` (requirements, design,
tasks) that a feat is realized as.
_Avoid_: specification-doc

**Seed**: A pre-authored artifact under a plan's `seeds/<feat>/` that hands the
eventual spec a head start.

**Brief**: The deterministic context pack `csdd plan brief` assembles for one run
step — the feat, its refs, decisions, and the step contract.

### Contracts

**Quality Gate**: A `- <label>: <command>` line in a plan's Quality Gates section;
the runner executes every gate after each implementation step.

**Stack Row**: One Decided-table row of `docs/stack.md` — a one-line technology
contract a feat cites as `stack:<name>`.

**ADR**: A decision record under `docs/adr/NNNN-<slug>.md` capturing a
gate-positive decision (hard to reverse, surprising, a real trade-off); a feat
cites it as `adr:<slug>`.
_Avoid_: decision-doc

### Knowledge base

**Steering**: The project-memory documents under `.claude/steering/` (product,
tech, structure) that source facts for the authoring flows.

**Wiki Page**: One concept page under `docs/wiki/pages/<slug>.md`, derived from a
raw source and cross-linked with `[[wikilinks]]`.
_Avoid_: article
