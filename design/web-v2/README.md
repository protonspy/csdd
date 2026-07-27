# csdd web — design draft (v2)

A static, self-contained prototype of the redesigned dashboard: palette, component
library, information architecture, every page, and a working reference system.

```
design/web-v2/
├── index.html   app shell (topbar · rail · content · jump-to palette)
├── tokens.css   design tokens — the whole palette, both themes
├── app.css      component library + page layouts
├── data.js      one fictional workspace, in the JSON shapes the API serves
├── refs.js      the reference system: token grammar, resolver, chip, linkify
└── app.js       hash router, IA, and every page as a pure function of the data
```

Open `index.html` directly in a browser — no build, no server, no dependencies.
Everything navigates: click a `adr:` chip in a plan's Refs cell and you land on
the record; hover it and you read it without leaving. `Ctrl`/`Cmd`+`K` opens the
jump-to palette. The `◐` button in the top-right swaps light/dark.

This is a **draft for review**, not the port. Nothing under `internal/web/` was
touched.

---

## 1. What changes structurally

Today's app is seven flat tabs (`Specs · Plans · Resources · Files · Tests ·
Wiki · Graph`) and grows one tab per feature. Meanwhile four knowledge-base
surfaces exist on disk and in the CLI with **no page at all**: decision records,
the glossary, the tech contract, and the codewiki documents.

The draft regroups into five areas, two of which own a contextual rail:

| Area | Holds | New? |
|---|---|---|
| **Overview** | the one screen that answers "where does this workspace stand" | **new** |
| **Specs** | list → one spec (gate rail, boundaries, tasks, evidence) | redesigned |
| **Plans** | list → one plan (milestones, feats, **Refs**, journal) | redesigned |
| **Knowledge** | Wiki · **Decisions** · **Glossary** · **Stack** · **Codewiki** · Graph | 4 new pages |
| **Workspace** | Files · Resources · Tests | redesigned |

Adding the next surface costs a rail entry, not a top-level tab.

---

## 2. Palette

Dark is the default; light is *selected*, not an inverted dark — every coloured
value carries its own step for its own surface. All of it lives in
`tokens.css` as roles (`--ink-2`, `--surface-2`, `--ref-adr`), so a component
never names a colour.

### Identity hues are capped at three, and the cap is measured

The three citation tokens (`[[wiki]]`, `adr:`, `stack:`) appear **side by side in
one Refs cell**, so they are an all-pairs categorical problem, not an adjacent
one. Run against this draft's real surfaces:

| | CVD ΔE | normal-vision ΔE | contrast | verdict |
|---|---|---|---|---|
| dark `#11151d` | 9.4 | 20.9 | all ≥ 3:1 | **pass** |
| light `#ffffff` | 9.2 | 24.0 | aqua 2.82:1 | **pass**, relief applies |

A fourth and fifth hue were measured and rejected: 9.8 and 12.9 normal-vision ΔE,
under the hard floor of 15 — two chips a full-colour reader cannot tell apart.
So the other reference kinds (`spec:`, `feat:`, `term:`, `src:`) wear a neutral
chip and are identified by their prefix. **The prefix was always the real identity
channel**; the hue only shortcuts it for the three that repeat most.

The aqua relief obligation is met structurally: a ref chip always carries a
visible text label, never a bare swatch.

| Role | Dark | Light | Note |
|---|---|---|---|
| `--ref-wiki` | `#3987e5` | `#2a78d6` | slot 1 |
| `--ref-adr` | `#d95926` | `#eb6834` | slot 2 |
| `--ref-stack` | `#199e70` | `#1baf7a` | slot 3 |
| `--ref-neutral` | `#7d8899` | `#5b6676` | every other kind |

### Brand and status

`--accent` stays csdd amber (`#f0a23b`, 8.7:1 on dark; re-stepped to `#8a5200` on
white). It is reserved for brand and selection — it is **never** an identity hue,
which is also why the categorical set skips the yellow slot that would collide
with it.

Status (`--good #0ca30c` · `--warning #fab219` · `--serious #ec835a` ·
`--critical #d03b3b`) is fixed and never themed, and is never reused as a series
colour. On white, warning (1.8:1) and serious (2.6:1) are sub-3:1 by design —
which is why **every badge in this draft is an icon *and* a word**, never a bare
colour. On dark, text ink is used for badge labels rather than the status hue, so
nothing depends on a 3.8:1 colour to be readable.

### Ink

| | Dark | Light |
|---|---|---|
| `--ink` | `#e6ecf5` (15.4:1) | `#12161d` (18.1:1) |
| `--ink-2` | `#9aa7ba` (7.5:1) | `#535d6d` (6.7:1) |
| `--ink-3` | `#6f7c90` (4.3:1) | `#6f7a8a` (4.4:1) |

`--ink-3` is the floor for real text; anything fainter is decoration.

---

## 3. Component inventory

Class names are the contract with the React port — the markup is what should be
copied, this draft's JS is scaffolding.

| Component | Class | Notes |
|---|---|---|
| App shell | `.topbar` `.rail` `.content` `.page` | rail collapses to a drawer < 860px |
| Nav tab | `.nav-tab` | five areas, active state on `--accent-soft` |
| Card / panel | `.card` `.panel` `.panel-head` `.panel-body` | `.card-link` for a whole-card link |
| Stat tile | `.stat` | label · value · delta · optional 12-point sparkline |
| Hero figure | `.hero-value` | exactly one per view (the Overview) |
| Meter | `.meter` `.meter-row` | fill carries severity; track is a step of the fill's own ramp |
| Badge | `.badge` `.badge.ok/warn/serious/bad/accent` | always icon + word |
| Phase pill | `.phase-pill` `.phase-dot` | requirements → design → tasks at a glance |
| Gate rail | `.gate-rail` `.gate-step` | the four phase gates of a spec |
| **Ref chip** | `.ref` `.ref-wiki/adr/stack` `.ref-broken` `.ref-superseded` | § 5 |
| Ref preview | `.ref-preview` | hover/focus popover, resolved on the spot |
| Data table | `table.data` | sticky head, `.num` for tabular columns |
| Banner | `.banner.info/warn/error/ok` | icon + a sentence that names the fix |
| Filter pills | `.filter-pills` `.filter-pill` | segmented, with counts |
| Tabs | `.tabs` `.tab` | underline, accent |
| Tree | `.tree-item` | files |
| Markdown body | `.md` | headings, tables, code, mermaid placeholder |
| Jump-to palette | `.omni-*` | `Ctrl`/`Cmd`+`K`, searches every citable entity |
| Flash target | `.flash` | a followed reference says where it landed |

Chart marks follow the fixed specs: 2px lines, ≥8px end markers with a 2px
surface ring, 6px meters, hairline recessive borders, text always in ink tokens
(never the data colour).

---

## 4. Pages

| Route | Page | What it is |
|---|---|---|
| `#/overview` | **Overview** | hero % of tasks checked · KPI row · **gate strip** (`spec validate`, `graph analyze --strict`, `wiki lint`, `codewiki lint`, `plan validate`) · plan snapshot · activity |
| `#/specs` | Specs | filter + search + card grid, phase dots per card |
| `#/specs/<f>` | Spec | gate rail · **refs this spec cites** · test evidence · per-boundary meters · task list with `(P)`/boundary/requirement/depends annotations |
| `#/plans` | Plans | approval + drift + progress per plan |
| `#/plan/<slug>` | Plan | milestone KPIs · **feats table with a live Refs column** · validate banner · run journal |
| `#/wiki[/<slug>]` | Wiki | page body with citations linkified in prose · sources · backlinks · tags |
| `#/adr[/<slug>]` | **Decisions** | the triple gate stated · accepted/superseded · **cited by** · a superseded record points at its successor instead of being edited |
| `#/glossary` | **Glossary** | canonical term · definition · `_Avoid_` tombstones · where the term is used |
| `#/stack` | **Tech stack** | Decided table with `undeclared`/`phantom`/`unrefined` lint state, external rows marked, open questions |
| `#/codewiki` | **Codewiki** | repo-derived documents: sections, citation count, **lint findings with the failing citation**, structure tree, which wiki pages were ingested from it |
| `#/graph` | Graph | query bar, canvas, legend (with the three-hue cap stated in the legend itself) |
| `#/files` | Files | tree + read-only viewer |
| `#/resources` | Resources | steering · skills · agents · commands · hooks · mcp |
| `#/tests` | Tests | pass/coverage KPIs · suites · failures with output |

---

## 5. The reference system

One grammar, one resolver, one chip — on every surface that can cite something.

```
[[album-sharing]]                     → a wiki page
adr:signed-urls-over-acl              → a decision record
stack:postgres                        → a row of the Decided table
spec:album-sharing                    → a spec
feat:share-link-expiry                → a feat inside a plan
term:Public link                      → a glossary term
src:internal/plan/runner.go:41-88     → a line range in a checkout (codewiki)
```

The first three are exactly the tokens `csdd plan validate` already parses
(`internal/plan/parse.go` § `tokenizeRefs`). The last four are in-app entities the
same syntax addresses for free.

**Anatomy.** `[dot] kind: label [state]`. The dot carries the hue; the text stays
in ink tokens. Never a bare swatch.

**States, and why they are visible.** A citation is not a link that either works
or 404s — it is a lint the workspace already computes:

| State | Rendered as | Comes from |
|---|---|---|
| `ok` | normal chip, navigable | resolves |
| `broken` | strike-through + `⚠`, not clickable | `plan validate` / `wiki lint` / `codewiki lint` |
| `superseded` | dashed edge + the word, preview names the successor | ADR `status: superseded` |
| `warn` | normal chip, preview carries the lint | `graph analyze` tech findings (`unrefined`, `phantom`) |

`#/plan/photo-sharing` demonstrates all four at once, and the page banner says
what `csdd plan validate` would say.

**Three interactions.**
1. **Click** → navigate, and the target row flashes so you see where you landed
   (`?feat=`, `?row=`, `?term=` anchors).
2. **Hover / focus** → resolve in place: title, body, provenance path, successor.
3. **`Ctrl`+`K`** → jump to any citable entity by name, or paste a token.

**Prose, not just cells.** `Refs.linkify()` turns a token written mid-sentence into
a chip. A wiki page that writes `adr:signed-urls-over-acl` in a paragraph, or a run
journal line naming a feat, gets a working link with no markup from the author.
That is visible on the Wiki and Overview pages.

### What the backend still owes this

The draft resolves against mock data. To make it real, `internal/web` needs:

| # | Change | Where | Source already exists |
|---|---|---|---|
| 1 | `webFeat` gains `Refs []string` (and optionally the pre-tokenized trio) | `internal/web/plans.go` | `plan.Feat.Refs` / `.WikiRefs` / `.StackRefs` / `.ADRRefs` — `internal/plan/model.go` |
| 2 | `GET /api/adr` — number, slug, title, body, status, superseded_by, file, **cited_by** | new `internal/web/adr.go` | `plan.ScanADRs(root)` — `internal/plan/adr.go` |
| 3 | `GET /api/stack` — Decided rows + `undeclared`/`phantom`/`unrefined` per row | new `internal/web/stack.go` | the Decided parser in `internal/graph/extract_stack_impl.go` (`stackContract`) and `plan.StackRow` in `internal/plan/validate.go` |
| 4 | `GET /api/glossary` — canonical, cluster, definition, avoid, used_in | new `internal/web/glossary.go` | `glossary.Load(root)` — `internal/glossary/glossary.go` |
| 5 | `GET /api/codewiki` — documents, sections, citation counts, lint findings | new `internal/web/codewiki.go` | `internal/codewiki` |
| 6 | `GET /api/ref?token=…` — the resolver | new `internal/web/refs.go` | see below |
| 7 | Spec detail gains the refs its documents cite | `internal/session` spec read-model | same tokenizer |

Register each with `a.protect(...)` in `newMux` (`internal/web/handlers.go`) and
keep the existing discipline: `setWriteDeadline`, opaque errors (never leak an
absolute path — follow `planNotFound`), and `workspace.SafeName` on any slug that
reaches the filesystem.

**On #6 — resolve server-side, not in the browser.** The rules for "does this
token resolve" are already written once, in `csdd plan validate`. If the frontend
re-implements them it will drift, and the UI will disagree with the gate. One
endpoint that returns `{kind, target, route, state, title, body, meta,
successor}` keeps the linter and the dashboard saying the same thing. Batch it
(`POST`-less: `?token=a&token=b`) so a feats table is one request.

Frontend side, the pieces that already exist and only need generalizing:

- `Markdown.tsx` intercepts `wiki:` hrefs today (its `onWikiLink` prop). That
  becomes `onRef(token)` handling every kind.
- `WikiView.tsx`'s `rewriteWikiLinks` becomes `rewriteRefs` and runs the full
  token regex, not only `[[…]]`.
- `types.ts` gains `Ref`, `ADR`, `StackRow`, `Term`, `CodewikiDoc`.

---

## 6. Porting notes

- Tokens land as `tokens.css` imported before `styles.css`; the existing
  `--bg/--panel/--text/--accent` names map onto the new roles one-to-one, so the
  swap can be done in a single commit without touching component markup.
- Pages are already one function each; each becomes a component with the same
  markup and class names.
- The rail is data-driven (`renderRail`) — the React `Sidebar` gains a second
  mode rather than a second component.
- Nothing in the draft needs a new dependency. The mermaid placeholder is where
  the existing `Mermaid.tsx` plugs back in; the graph canvas is where
  `GraphView.tsx` plugs back in unchanged.

## 7. Known gaps in the draft

- Filters, search, and the pager are rendered but inert (they are existing
  behaviour, not new design).
- The graph canvas is a static SVG sketch — the real view keeps vis-network.
- Markdown rendering is a ~60-line stand-in; the app keeps `react-markdown`.
- **Not visually reviewed in a browser by me** — the pages were rendered
  headlessly and checked for structure, escaping, and reference resolution, but
  layout, spacing, and the light theme want a human eye. That is the first thing
  to do with this draft.
