/* =============================================================================
   csdd web — draft shell: hash router, information architecture, pages.

   Vanilla on purpose. Every page here is a pure function of the mock data, so
   porting to the existing React app is a mechanical translation: the markup and
   the class names are the contract, this file is the scaffolding under them.

   Information architecture (the structural change in this draft)
   -----------------------------------------------------------------------------
   Today's app has seven flat tabs and grows one per feature. This draft has five
   primary areas, two of which own a contextual rail:

     Overview   the one screen that answers "where does this workspace stand"
     Specs      the contract layer — list, then one spec
     Plans      the layer above specs — list, then one plan and its feats
     Knowledge  wiki · decisions · glossary · stack · codewiki · graph
     Workspace  files · resources · tests

   Knowledge is the group that was missing: four of its six surfaces exist on
   disk and in the CLI today with no page at all.
   ========================================================================== */

const esc = Refs.esc
const $ = (sel) => document.querySelector(sel)

/* -- primitives ----------------------------------------------------------- */

const badge = (kind, icon, text, title = "") =>
  `<span class="badge ${kind}"${title ? ` title="${esc(title)}"` : ""}><span class="badge-icon">${icon}</span>${esc(text)}</span>`

const STATE_BADGE = {
  ok: (t) => badge("ok", "●", t || "ok"),
  warn: (t) => badge("warn", "▲", t || "warning"),
  serious: (t) => badge("serious", "▲", t || "attention"),
  bad: (t) => badge("bad", "✕", t || "failing"),
  muted: (t) => badge("", "○", t || "idle"),
}

function meter(pct, severity = "") {
  const w = Math.max(0, Math.min(100, pct))
  return `<div class="meter ${severity}"><div class="meter-fill" style="width:${w}%"></div></div>`
}

function meterRow(label, value, pct, severity = "") {
  return `<div class="meter-row">
    <span class="meter-label">${esc(label)}</span>
    <span class="meter-value tabular">${esc(value)}</span>
    ${meter(pct, severity)}
  </div>`
}

/** Stat tile: label · value · optional delta · optional 12-point sparkline. */
function stat({ label, value, unit = "", foot = "", delta = null, spark = null }) {
  const d = delta
    ? `<span class="stat-delta ${delta.dir}"><span class="arrow">${delta.dir === "up" ? "▲" : "▼"}</span>${esc(delta.text)}</span>`
    : ""
  return `<div class="stat">
    <span class="stat-label">${esc(label)}</span>
    <span class="stat-value">${esc(value)}${unit ? `<span class="stat-unit">${esc(unit)}</span>` : ""}</span>
    <span class="stat-foot">${d}${foot ? `<span>${esc(foot)}</span>` : ""}</span>
    ${spark ? sparkline(spark) : ""}
  </div>`
}

/** 2px line, ≥8px end marker with a surface ring — the mark spec, in SVG. */
function sparkline(points) {
  const w = 150,
    h = 26,
    max = Math.max(...points),
    min = Math.min(...points),
    span = max - min || 1
  const xy = points.map((p, i) => [(i / (points.length - 1)) * (w - 6) + 3, h - 3 - ((p - min) / span) * (h - 8)])
  const d = xy.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ")
  const [ex, ey] = xy[xy.length - 1]
  // CSS custom properties go in `style`, not in presentation attributes —
  // `fill="var(--x)"` is not reliably resolved.
  return `<svg class="stat-spark" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true">
    <path d="${d}" style="fill:none;stroke:var(--seq-400);opacity:.5" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
    <circle cx="${ex.toFixed(1)}" cy="${ey.toFixed(1)}" r="4" style="fill:var(--accent);stroke:var(--surface-1)" stroke-width="2"/>
  </svg>`
}

const panel = (title, body, right = "") =>
  `<section class="panel">
    <header class="panel-head"><h2>${esc(title)}</h2><span class="spacer"></span>${right}</header>
    ${body}
  </section>`

const empty = (title, hint) => `<div class="empty"><h2>${esc(title)}</h2><p class="muted">${hint}</p></div>`

const pageHead = (title, sub, crumbs = null, right = "") => `
  <header class="page-head">
    <div style="min-width:0">
      ${crumbs ? `<nav class="crumbs">${crumbs}</nav>` : ""}
      <h1>${title}</h1>
      ${sub ? `<p class="page-sub">${sub}</p>` : ""}
    </div>
    <span class="spacer"></span>
    ${right}
  </header>`

/* -- information architecture --------------------------------------------- */

const AREAS = [
  { id: "overview", label: "Overview", route: "#/overview" },
  { id: "specs", label: "Specs", route: "#/specs" },
  { id: "plans", label: "Plans", route: "#/plans" },
  { id: "knowledge", label: "Knowledge", route: "#/wiki" },
  { id: "workspace", label: "Workspace", route: "#/files" },
]

const AREA_OF = {
  overview: "overview",
  specs: "specs",
  plans: "plans",
  plan: "plans",
  wiki: "knowledge",
  adr: "knowledge",
  glossary: "knowledge",
  stack: "knowledge",
  codewiki: "knowledge",
  graph: "knowledge",
  files: "workspace",
  resources: "workspace",
  tests: "workspace",
}

/* -- router --------------------------------------------------------------- */

function parseRoute() {
  const raw = (location.hash || "#/overview").slice(2)
  const [pathPart, queryPart] = raw.split("?")
  const seg = pathPart.split("/").filter(Boolean).map(decodeURIComponent)
  const query = Object.fromEntries(new URLSearchParams(queryPart || ""))
  return { page: seg[0] || "overview", id: seg[1] || null, query }
}

function render() {
  const r = parseRoute()
  const area = AREA_OF[r.page] || "overview"

  $("#topNav").innerHTML = AREAS.map(
    (a) => `<a class="nav-tab ${a.id === area ? "active" : ""}" href="${a.route}">${esc(a.label)}</a>`,
  ).join("")

  $("#rail").innerHTML = renderRail(area, r)
  $("#content").innerHTML = (PAGES[r.page] || (() => empty("Not found", "That route does not exist in this draft.")))(r)

  // A followed reference lands on its target and says so.
  const target = r.query.feat || r.query.row || r.query.term
  if (target) {
    const el = document.getElementById(`t-${Refs.slugify(target)}`)
    if (el) {
      el.classList.add("flash")
      el.scrollIntoView({ block: "center", behavior: "smooth" })
    }
  } else {
    $("#content").scrollTop = 0
  }
  closeRail()
}

/* -- rail ----------------------------------------------------------------- */

function railGroup(title, items, activeHref) {
  return `<div class="rail-group">
    <div class="rail-group-title">${esc(title)}</div>
    ${items
      .map(
        (i) => `<a class="rail-item ${i.href === activeHref ? "active" : ""}" href="${i.href}">
          <span style="min-width:0">
            <span class="rail-item-name">${i.name}</span>
            ${i.desc ? `<span class="rail-item-desc">${esc(i.desc)}</span>` : ""}
          </span>
          ${i.count != null ? `<span class="rail-count">${i.count}</span>` : ""}
        </a>`,
      )
      .join("")}
  </div>`
}

function renderRail(area, r) {
  const here = `#/${r.page}${r.id ? "/" + r.id : ""}`

  if (area === "specs")
    return (
      `<div class="rail-head">Specs</div>` +
      railGroup(
        "features",
        DATA.specs.map((s) => ({ href: `#/specs/${s.feature}`, name: esc(s.feature), desc: `${s.phase} · ${s.tasks.done}/${s.tasks.total}` })),
        here,
      )
    )

  if (area === "plans")
    return (
      `<div class="rail-head">Plans</div>` +
      railGroup(
        "initiatives",
        DATA.plans.map((p) => ({ href: `#/plan/${p.slug}`, name: esc(p.name), desc: `${p.done}/${p.feats} feats${p.approved ? "" : " · unapproved"}` })),
        here,
      )
    )

  if (area === "knowledge")
    return (
      `<div class="rail-head">Knowledge</div>` +
      railGroup("wiki", DATA.wiki.pages.map((p) => ({ href: `#/wiki/${p.slug}`, name: esc(p.title), desc: p.category || "not in index" })), here) +
      railGroup("decisions", DATA.adrs.map((a) => ({ href: `#/adr/${a.slug}`, name: `<span class="mono faint">${String(a.number).padStart(4, "0")}</span> ${esc(a.slug)}`, desc: a.status === "superseded" ? "superseded" : "" })), here) +
      railGroup("contract", [
        { href: "#/stack", name: "Tech stack", desc: `${DATA.stack.rows.length} decided rows`, count: DATA.stack.undeclared.length ? "!" : null },
        { href: "#/glossary", name: "Glossary", desc: `${DATA.glossary.length} canonical terms` },
        { href: "#/codewiki", name: "Codewiki", desc: `${DATA.codewiki.docs.length} repo documents` },
        { href: "#/graph", name: "Graph", desc: `${DATA.graph.nodes} nodes` },
      ], here)
    )

  if (area === "workspace")
    return (
      `<div class="rail-head">Workspace</div>` +
      railGroup("areas", [
        { href: "#/files", name: "Files", desc: "workspace tree" },
        { href: "#/resources", name: "Resources", desc: "steering · skills · agents · mcp" },
        { href: "#/tests", name: "Tests", desc: `${DATA.tests.tests.passed}/${DATA.tests.tests.total} passing` },
      ], here) +
      (r.page === "files" ? railGroup("tree", flattenTree(DATA.tree).map((n) => ({ href: `#/files?path=${encodeURIComponent(n.path)}`, name: `<span class="mono">${esc(n.name)}</span>`, desc: n.dirPath })), "") : "")
    )

  return ""
}

function flattenTree(nodes, prefix = "") {
  const out = []
  for (const n of nodes) {
    if (n.dir) out.push(...flattenTree(n.children || [], `${prefix}${n.name}/`))
    else out.push({ name: n.name, path: n.path, dirPath: prefix })
  }
  return out
}

/* -- pages ---------------------------------------------------------------- */

const PAGES = {}

/* Overview — the screen that did not exist. One hero figure, one KPI row, the
   gate strip, plan progress, recent activity. */
PAGES.overview = () => {
  const specsDone = DATA.specs.filter((s) => s.tasks.total && s.tasks.done >= s.tasks.total).length
  const tasks = DATA.specs.reduce((a, s) => ({ total: a.total + s.tasks.total, done: a.done + s.tasks.done }), { total: 0, done: 0 })
  const pct = Math.round((tasks.done / tasks.total) * 100)
  const failing = DATA.gates.filter((g) => g.state === "bad").length

  return `<div class="page">
    ${pageHead(
      "Photo sharing",
      "Everything the workspace knows about itself, on one screen. Each figure links to the page that owns it.",
      null,
      `<div class="row">${STATE_BADGE.ok("plan approved")}${failing ? STATE_BADGE.bad(`${failing} gates failing`) : STATE_BADGE.ok("gates clean")}</div>`,
    )}

    <section class="card">
      <div class="overview-hero">
        <div class="hero">
          <span class="hero-value tabular">${pct}%</span>
          <span class="hero-side">of ${tasks.total} tasks checked<br /><span class="faint">across ${DATA.specs.length} specs</span></span>
        </div>
        <div class="spacer"></div>
        <div style="min-width:260px;flex:1;max-width:420px">
          ${DATA.planDetail["photo-sharing"].milestones
            .map((m) => meterRow(m.name, `${m.done}/${m.total}`, (m.done / m.total) * 100))
            .join("")}
        </div>
      </div>
    </section>

    <div class="kpi-row">
      ${stat({ label: "Specs completed", value: `${specsDone}`, unit: `/${DATA.specs.length}`, foot: "2 in progress", delta: { dir: "up", text: "+1 this week" } })}
      ${stat({ label: "Tests passing", value: DATA.tests.tests.passed.toLocaleString(), foot: `${DATA.tests.tests.failed} failing`, spark: [1268, 1271, 1270, 1274, 1276, 1275, 1279, 1279, 1281, 1277, 1279, 1279] })}
      ${stat({ label: "Coverage", value: DATA.tests.coverage.pct.toFixed(1), unit: "%", foot: `${DATA.tests.coverage.covered.toLocaleString()} of ${DATA.tests.coverage.lines.toLocaleString()} lines`, delta: { dir: "up", text: "+1.8 pt" } })}
      ${stat({ label: "Open decisions", value: `${DATA.stack.undeclared.length + DATA.stack.open.length}`, foot: "1 undeclared dep, 1 open question" })}
      ${stat({ label: "Graph", value: DATA.graph.nodes.toLocaleString(), unit: " nodes", foot: `${DATA.graph.edges.toLocaleString()} edges` })}
    </div>

    ${panel(
      "Gates",
      `<div class="panel-body"><div class="gate-grid">
        ${DATA.gates
          .map(
            (g) => `<div class="gate">
              <span>${(STATE_BADGE[g.state] || STATE_BADGE.muted)("")}</span>
              <span style="min-width:0">
                <span class="gate-name">${esc(g.name)}</span>
                <span class="gate-detail">${esc(g.detail)}</span>
              </span>
            </div>`,
          )
          .join("")}
      </div></div>`,
      `<span class="faint xs">run before every commit</span>`,
    )}

    <div class="grid grid-2">
      ${panel(
        "Plan · photo-sharing",
        `<div class="panel-body stack">
          ${DATA.planDetail["photo-sharing"].feats
            .slice(0, 4)
            .map(
              (f) => `<div class="row">
                <span class="state-dot state-${f.state}" title="${f.state}"></span>
                <a href="#/plan/photo-sharing?feat=${f.slug}" class="cell-title">${esc(f.slug)}</a>
                <span class="spacer"></span>
                ${Refs.chips(f.refs.slice(0, 2))}
              </div>`,
            )
            .join("")}
          <a class="badge accent" href="#/plan/photo-sharing">open the plan →</a>
        </div>`,
      )}
      ${panel(
        "Recent activity",
        `<div class="panel-body"><div class="activity">
          ${DATA.activity.map((a) => `<div class="activity-row"><span class="activity-when">${esc(a.when)}</span><span class="activity-what">${Refs.linkify(a.what)}</span></div>`).join("")}
        </div></div>`,
      )}
    </div>
  </div>`
}

/* Specs */
PAGES.specs = (r) => {
  if (r.id) return specDetail(r.id)
  return `<div class="page">
    ${pageHead("Specs", "The contract layer. A spec crosses requirements → design → tasks → implementation, one human-approved gate at a time.")}
    <div class="toolbar">
      <div class="filter-pills">
        <button class="filter-pill active">All <span class="filter-pill-n">${DATA.specs.length}</span></button>
        <button class="filter-pill">In progress <span class="filter-pill-n">${DATA.specs.filter((s) => s.tasks.done < s.tasks.total || !s.tasks.total).length}</span></button>
        <button class="filter-pill">Completed <span class="filter-pill-n">${DATA.specs.filter((s) => s.tasks.total && s.tasks.done >= s.tasks.total).length}</span></button>
      </div>
      <span class="spacer"></span>
      <input class="input" placeholder="Filter by name…" />
    </div>
    <div class="grid grid-3">
      ${DATA.specs.map(specCard).join("")}
    </div>
    <div class="pager">
      <span class="pager-range">1–${DATA.specs.length} of ${DATA.specs.length}</span>
      <div class="row"><button class="pager-btn" disabled>‹ Prev</button><span class="small">1 / 1</span><button class="pager-btn" disabled>Next ›</button></div>
    </div>
  </div>`
}

function specCard(s) {
  const done = s.tasks.total && s.tasks.done >= s.tasks.total
  const state = done ? STATE_BADGE.ok("done") : s.issues ? STATE_BADGE.warn(`${s.issues} issues`) : s.ready ? badge("accent", "▸", "ready") : STATE_BADGE.muted("active")
  const order = ["requirements", "design", "tasks"]
  const dots = order
    .map((p) => `<span class="phase-dot ${s.approvals[p] ? "done" : s.phase === p ? "current" : ""}" title="${p}"></span>`)
    .join("")
  return `<a class="card card-link" href="#/specs/${s.feature}">
    <div class="spec-card-head"><span class="spec-card-name">${esc(s.feature)}</span><span class="spacer"></span>${state}</div>
    <div class="spec-card-meta">
      <span class="phase-pill"><span class="phase-dots">${dots}</span>${esc(s.phase)}</span>
      <span class="spacer"></span>
      ${s.tasks.total ? `<span class="muted small tabular">${s.tasks.done}/${s.tasks.total} tasks</span>` : `<span class="faint small">no tasks yet</span>`}
    </div>
    ${s.tasks.total ? meter(s.tasks.pct) : ""}
  </a>`
}

function specDetail(feature) {
  const d = DATA.specDetail[feature]
  if (!d) return empty("Spec not shown in this draft", `Only <code class="inline-code">album-sharing</code> carries a full detail mock. <a href="#/specs">Back to specs</a>.`)
  const gates = ["requirements", "design", "tasks", "implementation"]
  return `<div class="page">
    ${pageHead(
      esc(d.feature),
      "",
      `<a href="#/specs">Specs</a> <span>›</span> <span>${esc(d.feature)}</span>`,
      `<div class="row">${badge("accent", "▸", "ready for implementation")}${STATE_BADGE.ok("0 issues")}</div>`,
    )}

    <div class="gate-rail">
      ${gates
        .map((g) => {
          const approved = d.approvals[g]
          const current = d.phase === g
          return `<div class="gate-step ${approved ? "done" : current ? "current" : ""}">
            <span class="gate-step-name">${approved ? "✓" : current ? "▸" : "○"} ${esc(g)}</span>
            <span class="gate-step-state">${approved ? "approved by a human" : current ? "in progress" : "not reached"}</span>
          </div>`
        })
        .join("")}
    </div>

    <section class="card">
      <div class="side-title">Cited by this spec</div>
      ${Refs.chips(d.refs)}
      <p class="page-sub" style="margin-top:10px">
        Every token here resolves — hover one to see what it says, click to open it. A citation that
        stops resolving is a lint, not a dead link the reader discovers.
      </p>
    </section>

    <div class="grid grid-2">
      ${panel(
        "Test evidence",
        `<div class="panel-body stack">
          <div class="row"><span class="muted">${esc(d.report.command)}</span><span class="spacer"></span>${STATE_BADGE.ok("green")}</div>
          <div class="kpi-row">
            ${stat({ label: "Passed", value: d.report.tests.passed, foot: "0 failed" })}
            ${stat({ label: "Coverage", value: d.report.coverage.pct.toFixed(1), unit: "%", foot: `${d.report.coverage.covered}/${d.report.coverage.lines} lines` })}
          </div>
          <span class="faint xs">recorded ${esc(d.report.updatedAt)}</span>
        </div>`,
      )}
      ${panel(
        "Boundaries",
        `<div class="panel-body stack">
          ${["ShareStore", "ShareService", "LinkService", "ShareAPI"]
            .map((b) => {
              const all = d.phases.flatMap((p) => p.tasks).filter((t) => t.boundary === b)
              const done = all.filter((t) => t.done).length
              return meterRow(b, `${done}/${all.length}`, (done / all.length) * 100)
            })
            .join("")}
          <span class="faint xs">Two <code class="inline-code">(P)</code> tasks never share a boundary — that is what makes them safe to run in parallel.</span>
        </div>`,
      )}
    </div>

    ${panel(
      "Tasks",
      `<div class="panel-body flush">
        ${d.phases
          .map(
            (p) => `<div class="task-phase-head">${esc(p.name)}</div>` +
              p.tasks
                .map(
                  (t) => `<div class="task-row">
                    <span class="task-box ${t.done ? "done" : ""}">✓</span>
                    <span class="task-id">${esc(t.id)}</span>
                    <span class="task-title">
                      ${esc(t.title)}
                      <span class="task-meta">
                        ${t.parallel ? badge("accent", "⇉", "P") : ""}
                        ${t.boundary ? badge("", "▣", t.boundary) : ""}
                        ${t.requirements ? badge("", "§", t.requirements.join(", ")) : ""}
                        ${t.depends ? badge("", "→", "after " + t.depends.join(", ")) : ""}
                        ${t.refs ? Refs.chips(t.refs) : ""}
                      </span>
                    </span>
                  </div>`,
                )
                .join(""),
          )
          .join("")}
      </div>`,
      `<span class="faint xs tabular">${d.phases.flatMap((p) => p.tasks).filter((t) => t.done).length}/${d.phases.flatMap((p) => p.tasks).length}</span>`,
    )}
  </div>`
}

/* Plans */
PAGES.plans = () => `<div class="page">
  ${pageHead("Plans", "A plan decomposes an initiative into feats, each becoming exactly one spec. The runner executes it; this page reads it.")}
  <div class="grid grid-2">
    ${DATA.plans
      .map(
        (p) => `<a class="card card-link" href="#/plan/${p.slug}">
          <div class="row"><h2>${esc(p.name)}</h2><span class="spacer"></span>${p.approved ? STATE_BADGE.ok("approved") : STATE_BADGE.warn("not approved")}${p.drift ? STATE_BADGE.bad("drift") : ""}</div>
          <p class="page-sub">${p.done} of ${p.feats} feats done</p>
          ${meter((p.done / p.feats) * 100)}
        </a>`,
      )
      .join("")}
  </div>
</div>`

PAGES.plan = (r) => {
  const d = DATA.planDetail[r.id]
  if (!d) return empty("Plan not shown in this draft", `<a href="#/plans">Back to plans</a>.`)
  const broken = d.feats.flatMap((f) => f.refs).filter((t) => {
    const res = Refs.resolve(t)
    return res.state === "broken" || res.state === "superseded"
  })

  return `<div class="page page-wide">
    ${pageHead(
      esc(d.name),
      "",
      `<a href="#/plans">Plans</a> <span>›</span> <span>${esc(d.slug)}</span>`,
      `<div class="row">${d.approved ? STATE_BADGE.ok("approved") : STATE_BADGE.warn("not approved")}${d.drift ? STATE_BADGE.bad("drift") : STATE_BADGE.ok("no drift")}</div>`,
    )}

    ${broken.length ? `<div class="banner error"><span class="banner-icon">✕</span><span><strong>csdd plan validate fails.</strong> ${broken.length} citation${broken.length > 1 ? "s do" : " does"} not resolve cleanly: ${Refs.chips([...new Set(broken)])} — a superseded record is not a citable decision, and a wikilink to nothing is a page somebody forgot to write.</span></div>` : ""}

    <div class="kpi-row">
      ${d.milestones.map((m) => stat({ label: m.name, value: `${m.done}`, unit: `/${m.total}`, foot: m.done === m.total ? "complete" : "in flight" })).join("")}
      ${stat({ label: "Tasks checked", value: d.feats.reduce((a, f) => a + f.tasks_checked, 0), unit: `/${d.feats.reduce((a, f) => a + f.tasks_total, 0)}`, foot: "across every feat" })}
    </div>

    ${panel(
      "Feats",
      `<div class="table-wrap"><table class="data">
        <thead><tr>
          <th style="width:34px">#</th><th>Feat</th><th>Objective</th><th>Depends</th><th>Milestone</th><th>Progress</th><th style="min-width:280px">Refs</th>
        </tr></thead>
        <tbody>
          ${d.feats
            .map(
              (f) => `<tr id="t-${Refs.slugify(f.slug)}">
                <td class="feat-num">${esc(f.num)}</td>
                <td class="tight">
                  <span class="row"><span class="state-dot state-${f.state}" title="${f.state}"></span><span class="cell-title">${esc(f.slug)}</span>${f.parallel ? ' <span class="badge" title="runs parallel to other (P) feats"><span class="badge-icon">⇉</span>P</span>' : ""}</span>
                  <span class="cell-sub">${esc(f.state)}</span>
                </td>
                <td>${esc(f.objective)}</td>
                <td class="tight">${f.depends.length ? `<span class="dep-list">${f.depends.map((n) => `<span class="badge"><span class="badge-icon">→</span>${esc(n)}</span>`).join("")}</span>` : `<span class="faint">—</span>`}</td>
                <td class="tight"><span class="cell-sub">${esc(f.milestone)}</span></td>
                <td style="min-width:120px">
                  ${f.tasks_total ? `<span class="cell-sub tabular">${f.tasks_checked}/${f.tasks_total}</span>${meter((f.tasks_checked / f.tasks_total) * 100)}` : `<span class="faint">no spec yet</span>`}
                </td>
                <td>${Refs.chips(f.refs)}</td>
              </tr>`,
            )
            .join("")}
        </tbody>
      </table></div>`,
      `<span class="faint xs">docs/plans/${esc(d.slug)}/plan.md</span>`,
    )}

    ${panel(
      "Run journal",
      `<div class="panel-body"><div class="activity">
        ${d.log.map((l) => `<div class="activity-row"><span class="activity-when">${esc(l.when)}</span><span class="activity-what">${Refs.linkify(l.what)}</span></div>`).join("")}
      </div></div>`,
      `<span class="faint xs">docs/plans/${esc(d.slug)}/log.md</span>`,
    )}
  </div>`
}

/* Wiki */
PAGES.wiki = (r) => {
  const page = DATA.wiki.pages.find((p) => p.slug === r.id) || DATA.wiki.pages[0]
  const backlinks = DATA.wiki.pages.filter((p) => p.slug !== page.slug && p.links.includes(page.slug))
  return `<div class="page">
    ${pageHead(esc(page.title), "", `<a href="#/wiki">Wiki</a> <span>›</span> <span>${esc(page.category || "unlisted")}</span>`, page.in_index ? "" : STATE_BADGE.warn("not in index.md"))}
    <div class="split">
      <article class="card">
        <div class="md">${markdown(page.body)}</div>
      </article>
      <aside>
        <div class="side-block">
          <div class="side-title">Sources</div>
          ${page.sources.length ? page.sources.map((s) => `<div class="row" style="margin-bottom:4px"><a class="ref" href="#/codewiki"><span class="ref-label">${esc(s)}</span></a></div>`).join("") : `<span class="faint small">none recorded</span>`}
        </div>
        <div class="side-block">
          <div class="side-title">Backlinks</div>
          ${backlinks.length ? backlinks.map((b) => Refs.chip(`[[${b.slug}]]`, { label: b.title })).join(" ") : `<span class="faint small">none</span>`}
        </div>
        <div class="side-block">
          <div class="side-title">Tags</div>
          ${page.tags.length ? page.tags.map((t) => badge("", "#", t)).join(" ") : `<span class="faint small">none</span>`}
        </div>
        <div class="side-block">
          <div class="side-title">File</div>
          <span class="path">${esc(page.path)}</span>
        </div>
      </aside>
    </div>
  </div>`
}

/* Decision records */
PAGES.adr = (r) => {
  if (r.id) {
    const a = DATA.adrs.find((x) => x.slug === r.id)
    if (!a) return empty("No such record", `<a href="#/adr">All decision records</a>.`)
    const successor = a.superseded_by ? DATA.adrs.find((x) => x.number === a.superseded_by) : null
    return `<div class="page">
      ${pageHead(
        esc(a.title),
        "",
        `<a href="#/adr">Decisions</a> <span>›</span> <span class="mono">${String(a.number).padStart(4, "0")}</span>`,
        a.status === "superseded" ? STATE_BADGE.warn("superseded") : STATE_BADGE.ok("accepted"),
      )}
      ${successor ? `<div class="banner warn"><span class="banner-icon">▲</span><span><strong>Superseded.</strong> This record is history — cite ${Refs.chip("adr:" + successor.slug)} instead. The record itself is never edited; that is what keeps the trail readable.</span></div>` : ""}
      <article class="card"><div class="md">${markdown("# " + a.title + "\n\n" + a.body)}</div></article>
      <div class="grid grid-2">
        ${panel("Cited by", `<div class="panel-body">${Refs.chips(a.cited_by)}</div>`)}
        ${panel("Record", `<div class="panel-body stack"><span class="path">${esc(a.file)}</span><span class="faint xs">Append-only. A superseded record is marked, never rewritten.</span></div>`)}
      </div>
    </div>`
  }

  return `<div class="page">
    ${pageHead(
      "Decision records",
      "A decision earns a record when it passes the triple gate: hard to reverse, surprising without context, the result of a real trade-off. Everything else is prose.",
    )}
    <section class="panel"><div class="panel-body">
      ${DATA.adrs
        .map(
          (a) => `<div class="adr-item">
            <span class="adr-num">${String(a.number).padStart(4, "0")}</span>
            <div class="adr-body">
              <div class="adr-title"><a href="#/adr/${a.slug}">${esc(a.title)}</a>${a.status === "superseded" ? STATE_BADGE.warn("superseded by " + String(a.superseded_by).padStart(4, "0")) : STATE_BADGE.ok("accepted")}</div>
              <p class="adr-text">${esc(a.body)}</p>
              <div class="row-wrap"><span class="faint xs">cited by</span>${Refs.chips(a.cited_by)}</div>
            </div>
          </div>`,
        )
        .join("")}
    </div></section>
  </div>`
}

/* Glossary */
PAGES.glossary = () => `<div class="page">
  ${pageHead(
    "Glossary",
    "One canonical term per concept, and the synonyms it bans. Identifiers — feat slugs, spec directories, wiki page names — are minted from these.",
  )}
  <section class="panel"><div class="panel-body">
    ${DATA.glossary
      .map(
        (g) => `<div class="term" id="t-${Refs.slugify(g.canonical)}">
          <div class="row"><span class="term-name">${esc(g.canonical)}</span>${badge("", "▣", g.cluster)}</div>
          <p class="term-def">${esc(g.definition)}</p>
          <div class="row-wrap">
            ${g.avoid.map((a) => `<span class="avoid" title="banned synonym — a lint when used as a whole token">✕ <span class="strike">${esc(a)}</span></span>`).join("")}
            <span class="spacer"></span>
            ${Refs.chips(g.used_in)}
          </div>
        </div>`,
      )
      .join("")}
  </div></section>
  <div class="banner info"><span class="banner-icon">●</span><span>Renaming a term never removes the old one: it moves to the successor's <code class="inline-code">_Avoid_</code> list, so the old name stays banned forever.</span></div>
</div>`

/* Tech contract */
PAGES.stack = () => `<div class="page page-wide">
  ${pageHead(
    "Tech stack",
    "The contract is law: a technology not listed here is an open decision, not a default. Each row is refined against current documentation before its first use.",
    null,
    `<div class="row">${STATE_BADGE.warn("1 unrefined")}${STATE_BADGE.warn("1 phantom")}${STATE_BADGE.bad("1 undeclared")}</div>`,
  )}

  ${DATA.stack.undeclared
    .map(
      (u) => `<div class="banner error"><span class="banner-icon">✕</span><span><strong>Undeclared: ${esc(u.name)}</strong> — found in ${esc(u.where)} but absent from the Decided table. ${esc(u.hint)}</span></div>`,
    )
    .join("")}

  ${panel(
    "Decided",
    `<div class="table-wrap"><table class="data">
      <thead><tr><th>Domain</th><th>Choice</th><th>Version</th><th>Why</th><th>Refs</th><th>Lint</th></tr></thead>
      <tbody>
        ${DATA.stack.rows
          .map(
            (row) => `<tr id="t-${Refs.slugify(row.name)}">
              <td class="tight"><span class="cell-sub">${esc(row.domain)}</span></td>
              <td class="tight"><span class="cell-title">${esc(row.choice)}</span>${row.external ? ' <span class="badge" title="external service — no dependency manifest will ever list it"><span class="badge-icon">☁</span>external</span>' : ""}<span class="cell-sub mono">stack:${esc(row.name)}</span></td>
              <td class="tight mono">${esc(row.version)}</td>
              <td>${esc(row.why)}</td>
              <td>${Refs.chips(row.refs)}</td>
              <td class="tight">${row.lint === "ok" ? STATE_BADGE.ok("refined") : row.lint === "unrefined" ? STATE_BADGE.warn("unrefined") : STATE_BADGE.serious("phantom")}</td>
            </tr>`,
          )
          .join("")}
      </tbody>
    </table></div>`,
    `<span class="faint xs">docs/stack.md</span>`,
  )}

  ${panel(
    "Open questions",
    `<div class="panel-body stack">${DATA.stack.open.map((o) => `<div class="row"><span class="badge-icon" style="color:var(--warning)">▲</span><span class="muted">${esc(o)}</span></div>`).join("")}
    <span class="faint xs">An open question is not a blocker — it is a decision nobody has been asked to make yet.</span></div>`,
  )}
</div>`

/* Codewiki */
PAGES.codewiki = (r) => {
  const doc = DATA.codewiki.docs.find((d) => d.name === r.id) || DATA.codewiki.docs[0]
  return `<div class="page page-wide">
    ${pageHead(
      "Codewiki",
      "A repository is not ingested page by page — it is read into one cited document. The LLM writes the prose; <code class=\"inline-code\">csdd codewiki lint</code> proves every citation against the checkout.",
      null,
      doc.lint.status === "fail" ? STATE_BADGE.bad(`${doc.lint.findings} unresolved citations`) : STATE_BADGE.ok("citations resolve"),
    )}

    <div class="row-wrap">
      ${DATA.codewiki.docs
        .map(
          (d) => `<a class="badge ${d.name === doc.name ? "accent" : ""}" href="#/codewiki/${d.name}"><span class="badge-icon">▤</span>${esc(d.owner)}-${esc(d.name)}</a>`,
        )
        .join("")}
    </div>

    <div class="kpi-row">
      ${stat({ label: "Sections", value: doc.sections, foot: "<<< SECTION: >>> delimited" })}
      ${stat({ label: "Citations", value: doc.citations, foot: `${doc.resolved} resolve` })}
      ${stat({ label: "Ingested into", value: doc.ingested_by.length, unit: " pages", foot: "wiki pages derived from it" })}
      ${stat({ label: "Structure", value: doc.structure.length, unit: " entries", foot: "tree ↔ sections in sync" })}
    </div>

    ${doc.findings.length
      ? panel(
          "Lint findings",
          `<div class="panel-body flush">
            ${doc.findings
              .map(
                (f) => `<div class="cite-line">
                  <span class="cite-bad">✕</span>
                  <span class="mono faint">L${f.line}</span>
                  ${Refs.chip("src:" + f.cite)}
                  <span class="muted">${esc(f.msg)}</span>
                </div>`,
              )
              .join("")}
          </div>`,
          `<span class="faint xs">${esc(doc.repo)}</span>`,
        )
      : ""}

    <div class="split">
      ${panel(
        "Sections",
        `<div class="panel-body flush">
          ${doc.toc
            .map(
              (t) => `<div class="cite-line">
                <span class="cite-ok">●</span>
                <span style="flex:1;min-width:0" class="mono">${esc(t.slug)}</span>
                <span class="muted">${esc(t.title)}</span>
                <span class="faint tabular">${t.cites} cites</span>
              </div>`,
            )
            .join("")}
        </div>`,
        `<span class="faint xs">${esc(doc.file)}</span>`,
      )}
      <aside>
        <div class="side-block">
          <div class="side-title">Structure tree</div>
          ${doc.structure.map((f) => `<div class="path" style="padding:2px 0">${esc(f.path)}${f.lines ? ` <span class="faint">· ${f.lines} lines</span>` : ""}</div>`).join("") || `<span class="faint small">not a checkout</span>`}
        </div>
        <div class="side-block">
          <div class="side-title">Ingested into</div>
          ${Refs.chips(doc.ingested_by.map((s) => `[[${s}]]`))}
        </div>
        <div class="side-block">
          <div class="side-title">Provenance</div>
          <span class="faint small">compiled ${esc(doc.generated)} from ${esc(doc.repo)}</span>
        </div>
      </aside>
    </div>
  </div>`
}

/* Graph */
PAGES.graph = () => {
  const hues = ["var(--ref-wiki)", "var(--ref-adr)", "var(--ref-stack)"]
  return `<div class="page page-wide">
    ${pageHead("Graph", "The deterministic brain: every spec, task, requirement, page, decision, term, and file, plus the edges between them. Query it before you grep.")}
    <div class="toolbar">
      <input class="input" style="flex:1;max-width:520px" placeholder="csdd graph query — e.g. &quot;what depends on album-sharing&quot;" />
      <button class="pager-btn">Query</button>
      <span class="spacer"></span>
      <span class="faint small tabular">${DATA.graph.nodes.toLocaleString()} nodes · ${DATA.graph.edges.toLocaleString()} edges</span>
    </div>
    <div class="graph-stage">
      <div class="graph-hint">drag to pan · scroll to zoom · click a node to open it</div>
      <svg width="100%" height="100%" viewBox="0 0 800 520" aria-label="knowledge graph preview">
        ${graphSketch()}
      </svg>
      <div class="graph-legend">
        ${DATA.graph.kinds
          .map((k, i) => {
            const c = i < 3 ? hues[i] : "var(--ref-neutral)"
            return `<div class="legend-row"><span class="legend-swatch" style="background:${c}"></span>${esc(k.kind)} <span class="faint tabular">${k.n}</span></div>`
          })
          .join("")}
        <div class="legend-row faint xs" style="margin-top:4px">past three kinds, identity comes from the label — not a fourth hue</div>
      </div>
    </div>
  </div>`
}

function graphSketch() {
  const nodes = [
    { x: 400, y: 250, r: 16, c: "var(--accent)", label: "album-sharing" },
    { x: 250, y: 160, r: 10, c: "var(--ref-wiki)", label: "storage-design" },
    { x: 560, y: 150, r: 10, c: "var(--ref-adr)", label: "signed-urls" },
    { x: 590, y: 340, r: 10, c: "var(--ref-stack)", label: "postgres" },
    { x: 230, y: 350, r: 10, c: "var(--ref-neutral)", label: "task 2.4" },
    { x: 400, y: 420, r: 9, c: "var(--ref-neutral)", label: "Share" },
    { x: 130, y: 250, r: 8, c: "var(--ref-wiki)", label: "audit-trail" },
  ]
  const edges = [[0, 1], [0, 2], [0, 3], [0, 4], [0, 5], [1, 6], [4, 5]]
  return (
    edges
      .map(([a, b]) => `<line x1="${nodes[a].x}" y1="${nodes[a].y}" x2="${nodes[b].x}" y2="${nodes[b].y}" style="stroke:var(--border-strong)" stroke-width="1"/>`)
      .join("") +
    nodes
      .map(
        (n) => `<g>
          <circle cx="${n.x}" cy="${n.y}" r="${n.r}" style="fill:${n.c};stroke:var(--surface-1)" stroke-width="2"/>
          <text x="${n.x}" y="${n.y + n.r + 14}" text-anchor="middle" style="fill:var(--ink-2);font:11px var(--sans)">${esc(n.label)}</text>
        </g>`,
      )
      .join("")
  )
}

/* Files */
PAGES.files = (r) => {
  const path = r.query.path || "docs/plans/photo-sharing/plan.md"
  const text = DATA.fileSample[path] || `# ${path}\n\n(this draft carries one sample file)`
  return `<div class="page page-wide">
    ${pageHead("Files", "", null, `<span class="path">${esc(path)}</span>`)}
    <section class="panel">
      <header class="panel-head"><h2>${esc(path.split("/").pop())}</h2><span class="spacer"></span><span class="faint xs">read-only · secrets redacted server-side</span></header>
      <pre class="codeblock" style="border:0;border-radius:0">${esc(text)}</pre>
    </section>
    <div class="banner info"><span class="banner-icon">●</span><span>A <code class="inline-code">Refs</code> cell in a viewed file is still just text — the chips above it on the Plan page are the same tokens, resolved.</span></div>
  </div>`
}

/* Resources */
PAGES.resources = () => {
  const groups = [
    ["Steering", DATA.resources.steering, "always-on project memory"],
    ["Skills", DATA.resources.skills, "executable workflow bundles"],
    ["Agents", DATA.resources.agents, "least-privilege sub-agents"],
    ["Commands", DATA.resources.commands, "slash commands"],
    ["Hooks", DATA.resources.hooks, "harness automation"],
    ["MCP", DATA.resources.mcp, "connected servers"],
  ]
  return `<div class="page">
    ${pageHead("Resources", "The governed artifacts csdd authors. Read-only here; the CLI and the MCP tools are the only sanctioned authors.")}
    <div class="grid grid-2">
      ${groups
        .map(
          ([title, items, sub]) =>
            panel(
              `${title} · ${items.length}`,
              `<div class="panel-body flush">
                ${items
                  .map(
                    (i) => `<div class="task-row">
                      <span style="min-width:0;flex:1">
                        <span class="cell-title mono">${esc(i.name)}</span>
                        <span class="cell-sub">${esc(i.description)}</span>
                      </span>
                      ${i.inclusion ? badge("", "◈", i.inclusion) : ""}
                      ${i.tools ? badge("", "⚒", i.tools) : ""}
                    </div>`,
                  )
                  .join("")}
              </div>`,
              `<span class="faint xs">${esc(sub)}</span>`,
            ),
        )
        .join("")}
    </div>
  </div>`
}

/* Tests */
PAGES.tests = () => {
  const t = DATA.tests
  const passRate = (t.tests.passed / t.tests.total) * 100
  return `<div class="page">
    ${pageHead("Tests", "The evidence behind a done verdict. A feat is not done because a session says so — it is done because this is green.", null, t.tests.failed ? STATE_BADGE.bad(`${t.tests.failed} failing`) : STATE_BADGE.ok("green"))}
    <div class="kpi-row">
      ${stat({ label: "Passing", value: t.tests.passed.toLocaleString(), unit: `/${t.tests.total.toLocaleString()}`, foot: `${passRate.toFixed(1)}%` })}
      ${stat({ label: "Failing", value: t.tests.failed, foot: t.tests.skipped + " skipped" })}
      ${stat({ label: "Coverage", value: t.coverage.pct.toFixed(1), unit: "%", foot: t.coverage.format })}
      ${stat({ label: "Duration", value: t.tests.durationSec.toFixed(0), unit: "s", foot: t.tests.source })}
    </div>
    ${panel(
      "Suites",
      `<div class="panel-body flush">
        <div class="suite-row head"><span>suite</span><span class="num">total</span><span class="num">passed</span><span class="num">failed</span><span class="num">time</span></div>
        ${t.suites
          .map(
            (s) => `<div class="suite-row">
              <span class="suite-name">${esc(s.name)}</span>
              <span class="tabular" style="text-align:right">${s.total}</span>
              <span class="tabular" style="text-align:right">${s.passed}</span>
              <span class="tabular" style="text-align:right;color:${s.failed ? "var(--ink)" : "var(--ink-3)"}">${s.failed || "—"}</span>
              <span class="tabular faint" style="text-align:right">${s.time.toFixed(1)}s</span>
            </div>`,
          )
          .join("")}
      </div>`,
    )}
    ${panel(
      `Failures · ${t.failures.length}`,
      `<div class="panel-body flush">
        ${t.failures
          .map(
            (f) => `<div class="failure">
              <div class="row"><span class="badge-icon" style="color:var(--critical)">✕</span><span class="failure-name">${esc(f.name)}</span><span class="spacer"></span><span class="faint xs mono">${esc(f.suite)}</span></div>
              <div class="failure-msg">${esc(f.message)}</div>
            </div>`,
          )
          .join("")}
      </div>`,
    )}
  </div>`
}

/* -- a very small markdown renderer (draft-only) -------------------------- */

function markdown(src) {
  const lines = String(src).split("\n")
  let out = "",
    inCode = false,
    codeLang = "",
    buf = [],
    inTable = false
  const flushTable = () => {
    if (!buf.length) return
    const rows = buf.filter((l) => !/^\|[\s:-]+\|$/.test(l.replace(/\s/g, "")))
    out += "<table>" + rows
      .map((l, i) => {
        const cells = l.split("|").slice(1, -1).map((c) => (i === 0 ? `<th>${Refs.linkify(inline(c.trim()))}</th>` : `<td>${Refs.linkify(inline(c.trim()))}</td>`))
        return `<tr>${cells.join("")}</tr>`
      })
      .join("") + "</table>"
    buf = []
    inTable = false
  }
  const inline = (s) =>
    esc(s)
      .replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>')
      .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")

  for (const line of lines) {
    if (line.startsWith("```")) {
      if (inCode) {
        out += codeLang === "mermaid"
          ? `<div class="codeblock" style="text-align:center;color:var(--ink-3)">◇ mermaid diagram — rendered live in the app<br /><span class="xs">${esc(buf.join(" "))}</span></div>`
          : `<pre class="codeblock">${esc(buf.join("\n"))}</pre>`
        buf = []
        inCode = false
      } else {
        inCode = true
        codeLang = line.slice(3).trim()
      }
      continue
    }
    if (inCode) {
      buf.push(line)
      continue
    }
    if (line.startsWith("|")) {
      inTable = true
      buf.push(line.trim())
      continue
    }
    if (inTable) flushTable()

    if (/^#{1,3} /.test(line)) {
      const level = line.match(/^#+/)[0].length
      out += `<h${level}>${Refs.linkify(inline(line.replace(/^#+ /, "")))}</h${level}>`
    } else if (/^[-*] /.test(line)) {
      out += `<ul><li>${Refs.linkify(inline(line.slice(2)))}</li></ul>`
    } else if (line.trim() === "") {
      // paragraph break
    } else {
      out += `<p>${Refs.linkify(inline(line))}</p>`
    }
  }
  if (inTable) flushTable()
  return out.replace(/<\/ul><ul>/g, "")
}

/* -- reference hover preview ---------------------------------------------- */

const preview = $("#refPreview")

function showPreview(el) {
  const token = el.getAttribute("data-ref")
  const r = Refs.resolve(token)
  const stateBadge =
    r.state === "broken" ? STATE_BADGE.bad("does not resolve") : r.state === "superseded" ? STATE_BADGE.warn("superseded") : r.state === "warn" ? STATE_BADGE.warn("lint") : STATE_BADGE.ok("resolves")
  preview.innerHTML = `
    <div class="ref-preview-head">
      <span class="ref-preview-kind">${esc(r.prefix || "[[…]]")}</span>
      <span class="spacer"></span>${stateBadge}
    </div>
    <div class="ref-preview-title">${esc(r.title || r.label)}</div>
    <div class="ref-preview-body">${esc(r.body || "")}</div>
    ${r.successor ? `<div class="ref-preview-body" style="margin-top:6px">→ cite <strong>${esc(r.successor.slug)}</strong> instead: ${esc(r.successor.title)}</div>` : ""}
    <div class="ref-preview-foot"><span>${esc(r.meta || "")}</span><span>${r.route ? "click to open" : ""}</span></div>`
  const box = el.getBoundingClientRect()
  preview.hidden = false
  const pb = preview.getBoundingClientRect()
  let left = Math.min(box.left, window.innerWidth - pb.width - 12)
  let top = box.bottom + 8
  if (top + pb.height > window.innerHeight - 8) top = Math.max(8, box.top - pb.height - 8)
  preview.style.left = `${Math.max(8, left)}px`
  preview.style.top = `${top}px`
}

document.addEventListener("mouseover", (e) => {
  const el = e.target.closest("[data-ref]")
  if (el) showPreview(el)
})
document.addEventListener("mouseout", (e) => {
  if (e.target.closest("[data-ref]")) preview.hidden = true
})
document.addEventListener("focusin", (e) => {
  const el = e.target.closest("[data-ref]")
  if (el) showPreview(el)
})
document.addEventListener("focusout", () => (preview.hidden = true))
window.addEventListener("hashchange", () => (preview.hidden = true))

/* -- jump-to palette ------------------------------------------------------ */

const omni = { open: false, items: [], active: 0 }

function openOmni() {
  omni.open = true
  $("#omniOverlay").hidden = false
  $("#omniInput").value = ""
  fillOmni("")
  $("#omniInput").focus()
}
function closeOmni() {
  omni.open = false
  $("#omniOverlay").hidden = true
}
function fillOmni(q) {
  const all = Refs.index()
  const needle = q.trim().toLowerCase()
  omni.items = (needle ? all.filter((i) => (i.label + " " + i.token + " " + i.hint).toLowerCase().includes(needle)) : all).slice(0, 40)
  omni.active = 0
  $("#omniResults").innerHTML = omni.items.length
    ? omni.items
        .map(
          (i, n) => `<button class="omni-item ${n === omni.active ? "active" : ""}" data-n="${n}">
            ${Refs.chip(i.token, { label: i.kind })}
            <span class="omni-item-label">${esc(i.label)}</span>
            <span class="faint xs">${esc(i.hint)}</span>
          </button>`,
        )
        .join("")
    : `<div class="omni-empty">Nothing matches “${esc(q)}”.</div>`
}
function activateOmni(n) {
  const item = omni.items[n]
  if (!item) return
  const r = Refs.resolve(item.token)
  closeOmni()
  if (r.route) location.hash = r.route
}

$("#omniBtn").addEventListener("click", openOmni)
$("#omniOverlay").addEventListener("click", (e) => {
  if (e.target.id === "omniOverlay") closeOmni()
  const item = e.target.closest(".omni-item")
  if (item) activateOmni(Number(item.dataset.n))
})
$("#omniInput").addEventListener("input", (e) => fillOmni(e.target.value))
document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
    e.preventDefault()
    omni.open ? closeOmni() : openOmni()
    return
  }
  if (!omni.open) return
  if (e.key === "Escape") closeOmni()
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault()
    omni.active = Math.max(0, Math.min(omni.items.length - 1, omni.active + (e.key === "ArrowDown" ? 1 : -1)))
    ;[...document.querySelectorAll(".omni-item")].forEach((el, n) => el.classList.toggle("active", n === omni.active))
    document.querySelector(".omni-item.active")?.scrollIntoView({ block: "nearest" })
  }
  if (e.key === "Enter") activateOmni(omni.active)
})

/* -- chrome --------------------------------------------------------------- */

$("#themeBtn").addEventListener("click", () => {
  const next = document.documentElement.dataset.theme === "dark" ? "light" : "dark"
  document.documentElement.dataset.theme = next
  try {
    localStorage.setItem("csdd-theme", next)
  } catch {}
})
try {
  const saved = localStorage.getItem("csdd-theme")
  if (saved) document.documentElement.dataset.theme = saved
} catch {}

const closeRail = () => {
  $("#rail").classList.remove("open")
  $("#railBackdrop").classList.remove("on")
}
$("#railToggle").addEventListener("click", () => {
  $("#rail").classList.toggle("open")
  $("#railBackdrop").classList.toggle("on")
})
$("#railBackdrop").addEventListener("click", closeRail)

window.addEventListener("hashchange", render)
render()
