/* =============================================================================
   The reference system
   -----------------------------------------------------------------------------
   One token grammar, one resolver, one chip — used by every surface that can
   cite something: a plan's Refs cell, a task annotation, a spec body, a wiki
   page, an ADR, a codewiki section.

     [[album-sharing]]              → a wiki page
     adr:signed-urls-over-acl       → a decision record
     stack:postgres                 → a row of the docs/stack.md Decided table
     spec:album-sharing             → a spec
     feat:share-link-expiry         → a feat inside a plan
     term:Public link               → a glossary term
     src:internal/plan/runner.go:41-88  → a line range in a checkout

   The first three are the tokens `csdd plan validate` already knows (§ Refs).
   The last four are in-app entities the same syntax can address for free — and
   the reason the chip's colour is capped at three hues: the kind prefix is the
   real identity channel, so the additional kinds cost no colour.

   Every resolution carries a state: ok · broken · superseded. Broken and
   superseded are what the linters find; the UI must show them, not swallow them.
   ========================================================================== */

const Refs = (() => {
  const esc = (s) =>
    String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c])

  // Order matters: the [[…]] form first, then prefix forms longest-first.
  const TOKEN_RE = /\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]|\b(adr|stack|spec|feat|term|src):([A-Za-z0-9][A-Za-z0-9._/-]*(?::\d+-\d+)?)/g

  /** parse one token string into {kind, target} — null when it is not a token. */
  function parse(token) {
    const t = String(token ?? "").trim()
    const wiki = /^\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]$/.exec(t)
    if (wiki) return { kind: "wiki", target: wiki[1].trim(), raw: t }
    const pref = /^(adr|stack|spec|feat|term|src):(.+)$/.exec(t)
    if (pref) return { kind: pref[1], target: pref[2].trim(), raw: t }
    return null
  }

  const findPlanOfFeat = (slug) => {
    for (const [planSlug, detail] of Object.entries(DATA.planDetail)) {
      if (detail.feats.some((f) => f.slug === slug)) return planSlug
    }
    return null
  }

  /**
   * resolve(token) → the full record the UI needs:
   *   { kind, label, prefix, route, state, title, body, meta }
   * `route` is a hash route; null when the target does not exist.
   *
   * In production this is exactly the shape a `GET /api/ref?token=…` endpoint
   * should return — see README § "What the backend still owes this".
   */
  function resolve(token) {
    const p = parse(token)
    if (!p) return { kind: "unknown", label: String(token), prefix: "", route: null, state: "broken", title: "Not a reference token" }

    switch (p.kind) {
      case "wiki": {
        const page = DATA.wiki.pages.find((w) => w.slug === p.target)
        return {
          kind: "wiki",
          prefix: "",
          label: p.target,
          route: page ? `#/wiki/${page.slug}` : null,
          state: page ? "ok" : "broken",
          title: page ? page.title : "No such wiki page",
          body: page ? firstProse(page.body) : "No page under docs/wiki/pages/ matches this link. `csdd wiki lint` reports it as a broken wikilink.",
          meta: page ? page.path : "docs/wiki/pages/" + p.target + ".md",
        }
      }
      case "adr": {
        const rec = DATA.adrs.find((a) => a.slug === p.target)
        if (!rec)
          return { kind: "adr", prefix: "adr:", label: p.target, route: null, state: "broken", title: "No such decision record", body: "`csdd plan validate` breaks on a citation that resolves to nothing.", meta: "docs/adr/" }
        const successor = rec.status === "superseded" ? DATA.adrs.find((a) => a.number === rec.superseded_by) : null
        return {
          kind: "adr",
          prefix: "adr:",
          label: rec.slug,
          route: `#/adr/${rec.slug}`,
          state: rec.status === "superseded" ? "superseded" : "ok",
          title: rec.title,
          body: rec.body,
          meta: rec.file,
          successor: successor ? { slug: successor.slug, title: successor.title, route: `#/adr/${successor.slug}` } : null,
        }
      }
      case "stack": {
        const row = DATA.stack.rows.find((r) => r.name === p.target)
        return {
          kind: "stack",
          prefix: "stack:",
          label: p.target,
          route: row ? `#/stack?row=${encodeURIComponent(row.name)}` : null,
          state: row ? (row.lint === "ok" ? "ok" : "warn") : "broken",
          title: row ? `${row.choice} ${row.version !== "—" ? row.version : ""}`.trim() : "Not in the Decided table",
          body: row
            ? row.why + (row.lint === "unrefined" ? "  ⚠ not refined against current docs yet." : row.lint === "phantom" ? "  ⚠ in the contract but no usage detected." : "")
            : "Any technology not listed in docs/stack.md is an open decision — propose options and ask the human.",
          meta: row ? `docs/stack.md · ${row.domain}` : "docs/stack.md",
        }
      }
      case "spec": {
        const spec = DATA.specs.find((s) => s.feature === p.target)
        return {
          kind: "spec",
          prefix: "spec:",
          label: p.target,
          route: spec ? `#/specs/${spec.feature}` : null,
          state: spec ? "ok" : "broken",
          title: spec ? spec.feature : "No such spec",
          body: spec ? `Phase ${spec.phase} · ${spec.tasks.done}/${spec.tasks.total} tasks · ${spec.ready ? "ready for implementation" : "not ready"}` : "",
          meta: spec ? `specs/${spec.feature}/` : "specs/",
        }
      }
      case "feat": {
        const planSlug = findPlanOfFeat(p.target)
        const feat = planSlug ? DATA.planDetail[planSlug].feats.find((f) => f.slug === p.target) : null
        return {
          kind: "feat",
          prefix: "feat:",
          label: p.target,
          route: feat ? `#/plan/${planSlug}?feat=${feat.slug}` : null,
          state: feat ? "ok" : "broken",
          title: feat ? `Feat ${feat.num} — ${feat.slug}` : "No such feat",
          body: feat ? feat.objective : "",
          meta: feat ? `docs/plans/${planSlug}/plan.md` : "docs/plans/",
        }
      }
      case "term": {
        const term = DATA.glossary.find((g) => g.canonical.toLowerCase() === p.target.toLowerCase())
        return {
          kind: "term",
          prefix: "term:",
          label: p.target,
          route: term ? `#/glossary?term=${encodeURIComponent(term.canonical)}` : null,
          state: term ? "ok" : "broken",
          title: term ? term.canonical : "Not a canonical term",
          body: term ? term.definition : "",
          meta: "docs/glossary.md",
        }
      }
      case "src": {
        // src:<path>:<start>-<end> — a codewiki citation into the checkout.
        const m = /^(.+?):(\d+)-(\d+)$/.exec(p.target)
        const path = m ? m[1] : p.target
        const doc = DATA.codewiki.docs[0]
        const inTree = doc.structure.some((f) => f.path === path)
        const file = doc.structure.find((f) => f.path === path)
        const overrun = m && file && file.lines && Number(m[3]) > file.lines
        return {
          kind: "src",
          prefix: "",
          label: p.target,
          route: inTree && !overrun ? `#/files?path=${encodeURIComponent(path)}` : null,
          state: !inTree ? "broken" : overrun ? "broken" : "ok",
          title: path,
          body: !inTree
            ? "Path is not in the document's Structure tree — csdd codewiki lint fails this citation."
            : overrun
              ? `Range ends at ${m[3]} but the file has ${file.lines} lines.`
              : `Lines ${m[2]}–${m[3]} of ${file.lines}.`,
          meta: doc.repo,
        }
      }
      default:
        return { kind: "unknown", label: p.target, prefix: "", route: null, state: "broken", title: "Unknown reference kind" }
    }
  }

  /** chip(token) → the <a>/<span> HTML for one citation. */
  function chip(token, opts = {}) {
    const r = resolve(token)
    const hue = r.kind === "wiki" || r.kind === "adr" || r.kind === "stack" ? `ref-${r.kind}` : ""
    const state = r.state === "broken" ? "ref-broken" : r.state === "superseded" ? "ref-superseded" : ""
    const label = opts.label ?? r.label
    const inner =
      (r.prefix ? `<span class="ref-kind">${esc(r.prefix)}</span>` : "") +
      `<span class="ref-label">${esc(label)}</span>` +
      (r.state === "superseded" ? `<span class="ref-state">superseded</span>` : "") +
      (r.state === "broken" ? `<span class="ref-state">⚠</span>` : "")
    const cls = `ref ${hue} ${state}`.trim()
    const data = `data-ref="${esc(token)}"`
    if (!r.route) return `<span class="${cls}" ${data} title="${esc(r.title)}">${inner}</span>`
    return `<a class="${cls}" ${data} href="${r.route}">${inner}</a>`
  }

  /** chips(list) → a wrapped row of chips (a Refs cell). */
  function chips(tokens, opts) {
    if (!tokens || !tokens.length) return `<span class="faint xs">—</span>`
    return `<span class="ref-list">${tokens.map((t) => chip(t, opts)).join("")}</span>`
  }

  /**
   * linkify(text) → the same text with every citation token turned into a chip.
   * This is what makes a reference a hyperlink *in prose* — a wiki page that
   * writes `adr:signed-urls-over-acl` mid-sentence gets a working link without
   * the author writing any markup.
   */
  function linkify(text) {
    return String(text ?? "").replace(TOKEN_RE, (m) => chip(m))
  }

  /** index() → every citable entity, for the jump-to palette. */
  function index() {
    const out = []
    DATA.specs.forEach((s) => out.push({ token: `spec:${s.feature}`, kind: "spec", label: s.feature, hint: `phase ${s.phase}` }))
    Object.entries(DATA.planDetail).forEach(([slug, d]) =>
      d.feats.forEach((f) => out.push({ token: `feat:${f.slug}`, kind: "feat", label: f.slug, hint: `${d.name} · feat ${f.num}` })),
    )
    DATA.wiki.pages.forEach((p) => out.push({ token: `[[${p.slug}]]`, kind: "wiki", label: p.title, hint: p.category || "wiki" }))
    DATA.adrs.forEach((a) => out.push({ token: `adr:${a.slug}`, kind: "adr", label: a.title, hint: `ADR ${String(a.number).padStart(4, "0")}${a.status === "superseded" ? " · superseded" : ""}` }))
    DATA.stack.rows.forEach((r) => out.push({ token: `stack:${r.name}`, kind: "stack", label: `${r.choice} ${r.version !== "—" ? r.version : ""}`.trim(), hint: r.domain }))
    DATA.glossary.forEach((g) => out.push({ token: `term:${g.canonical}`, kind: "term", label: g.canonical, hint: g.cluster }))
    return out
  }

  const slugify = (s) => String(s).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")
  const firstProse = (md) =>
    String(md ?? "")
      .split("\n")
      .filter((l) => l.trim() && !l.startsWith("#") && !l.startsWith("|") && !l.startsWith("```"))
      .slice(0, 1)
      .join(" ")
      .slice(0, 180)

  return { parse, resolve, chip, chips, linkify, index, esc, slugify }
})()
