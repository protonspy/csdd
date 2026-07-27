import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { Artifact, Overview, PlanSummary, WikiPage, WorkspaceTree } from '../types'
import { RESOURCES, type ResourceKind } from '../resources'
import { api } from '../api'
import { FileTree } from './FileTree'
import { ProgressBar } from './bits'
import { PlanStateBadge } from './PlansView'
import { href, hrefWithQuery, navigate, type Route } from '../router'

// The contextual rail. It carries two things: where else this area can go, and
// what this area contains. Both are anchors — a rail row is a link like any
// other, so it can be middle-clicked, copied and bookmarked.
//
// Each area fetches its own list (plans, wiki). That is one extra request for a
// small JSON against a local server, and it buys the rail independence from
// whatever page happens to be open beside it.
export function Rail({
  route,
  overview,
  version,
  open,
}: {
  route: Route
  overview: Overview | null
  version: number
  open: boolean
}) {
  return (
    <aside className={`sidebar ${open ? 'open' : ''}`}>
      <AreaNav route={route} />
      {route.area === 'specs' && <SpecsRail overview={overview} current={route.id} />}
      {route.area === 'plans' && <PlansRail version={version} current={route.id} />}
      {route.area === 'knowledge' && route.page === 'wiki' && <WikiRail version={version} current={route.id} />}
      {route.area === 'workspace' && route.page === 'files' && (
        <FilesRail version={version} current={route.query.path ?? null} />
      )}
      {route.area === 'workspace' && route.page === 'resources' && (
        <ResourcesRail overview={overview} kind={route.id} name={route.sub} />
      )}
    </aside>
  )
}

/** The other pages of the current area. Areas with a single page render none. */
function AreaNav({ route }: { route: Route }) {
  const links =
    route.area === 'knowledge'
      ? [
          { to: '#/wiki', page: 'wiki', label: 'Wiki', desc: 'authored pages' },
          { to: '#/adr', page: 'adr', label: 'Decisions', desc: 'the why' },
          { to: '#/stack', page: 'stack', label: 'Tech stack', desc: 'the contract' },
          { to: '#/glossary', page: 'glossary', label: 'Glossary', desc: 'the language' },
          { to: '#/graph', page: 'graph', label: 'Graph', desc: 'the brain' },
        ]
      : route.area === 'workspace'
        ? [
            { to: '#/files', page: 'files', label: 'Files', desc: 'workspace tree' },
            { to: '#/resources', page: 'resources', label: 'Resources', desc: 'steering · skills · agents' },
            { to: '#/tests', page: 'tests', label: 'Tests', desc: 'evidence' },
          ]
        : []
  if (links.length === 0) return null
  return (
    <nav className="rail-nav">
      {links.map((l) => (
        <a key={l.to} className={`rail-link ${route.page === l.page ? 'active' : ''}`} href={l.to}>
          {l.label}
          <span className="rail-link-desc">{l.desc}</span>
        </a>
      ))}
    </nav>
  )
}

function SpecsRail({ overview, current }: { overview: Overview | null; current: string | null }) {
  const specs = overview?.specs ?? []
  return (
    <Section title="Specs" count={specs.length} defaultOpen>
      <div className="spec-list">
        {specs.map((s) => (
          <a
            key={s.feature}
            className={`rail-row spec-row ${s.feature === current ? 'active' : ''}`}
            href={href('specs', s.feature)}
            title={s.feature}
          >
            <div className="spec-row-head">
              <span className="spec-name">{s.feature}</span>
              {s.issues > 0 && <span className="badge warn">{s.issues}</span>}
            </div>
            <div className="spec-row-meta">
              <span className="muted small">{s.phase}</span>
              {s.tasks.total > 0 && (
                <span className="muted small">
                  {s.tasks.done}/{s.tasks.total}
                </span>
              )}
            </div>
            {s.tasks.total > 0 && <ProgressBar pct={s.tasks.pct} />}
          </a>
        ))}
        {specs.length === 0 && <div className="muted small">none yet</div>}
      </div>
    </Section>
  )
}

function PlansRail({ version, current }: { version: number; current: string | null }) {
  const [plans, setPlans] = useState<PlanSummary[]>([])
  useEffect(() => {
    api
      .plans()
      .then((p) => setPlans(p ?? []))
      .catch(() => setPlans([]))
  }, [version])

  return (
    <Section title="Plans" count={plans.length} defaultOpen>
      <div className="spec-list">
        {plans.map((p) => (
          <a
            key={p.slug}
            className={`rail-row spec-row ${p.slug === current ? 'active' : ''}`}
            href={href('plans', p.slug)}
            title={p.slug}
          >
            <div className="spec-row-head">
              <span className="spec-name">{p.name || p.slug}</span>
              <PlanStateBadge approved={p.approved} drift={p.drift} complete={p.complete} />
            </div>
            <div className="spec-row-meta">
              <span className="muted small">
                {p.done}/{p.feats} feats
              </span>
            </div>
            <ProgressBar pct={p.feats > 0 ? Math.round((p.done * 100) / p.feats) : 0} />
          </a>
        ))}
        {plans.length === 0 && <div className="muted small">none yet</div>}
      </div>
    </Section>
  )
}

function WikiRail({ version, current }: { version: number; current: string | null }) {
  const [pages, setPages] = useState<WikiPage[]>([])
  const [categories, setCategories] = useState<string[]>([])
  useEffect(() => {
    api
      .wiki()
      .then((w) => {
        setPages(w?.pages ?? [])
        setCategories(w?.categories ?? [])
      })
      .catch(() => setPages([]))
  }, [version])

  if (pages.length === 0) return <div className="side-body muted small">no pages yet</div>

  // Grouped by the index.md catalog, in the order the catalog lists them; pages
  // absent from it fall into a trailing group (the read model sorts them last).
  const groups = [...categories, '\0'].map((c) => ({
    name: c === '\0' ? 'Not in index' : c,
    pages: pages.filter((p) => (p.in_index && p.category ? p.category : '\0') === c),
  }))

  return (
    <>
      {groups
        .filter((g) => g.pages.length > 0)
        .map((g) => (
          <Section key={g.name} title={g.name} count={g.pages.length} defaultOpen>
            <div className="resource-list">
              {g.pages.map((p) => (
                <a
                  key={p.slug}
                  className={`rail-row resource-row ${p.slug === current ? 'active' : ''}`}
                  href={href('wiki', p.slug)}
                  title={p.title}
                >
                  <div className="resource-row-head">
                    <span className="resource-name">{p.title}</span>
                    {!p.in_index && <span className="badge muted">unlisted</span>}
                  </div>
                </a>
              ))}
            </div>
          </Section>
        ))}
    </>
  )
}

function FilesRail({ version, current }: { version: number; current: string | null }) {
  const [tree, setTree] = useState<WorkspaceTree>({ csdd: [], project: [] })
  useEffect(() => {
    api
      .tree()
      .then((t) => setTree({ csdd: t?.csdd ?? [], project: t?.project ?? [] }))
      .catch(() => setTree({ csdd: [], project: [] }))
  }, [version])

  const openFile = (path: string) => navigate(hrefWithQuery('files', { path }))
  return (
    <>
      <Section title="csdd" count={tree.csdd.length} defaultOpen>
        <FileTree nodes={tree.csdd} selectedPath={current} onOpenFile={openFile} />
      </Section>
      <Section title="Project" count={tree.project.length} defaultOpen={false}>
        <FileTree nodes={tree.project} selectedPath={current} onOpenFile={openFile} />
      </Section>
    </>
  )
}

function ResourcesRail({
  overview,
  kind,
  name,
}: {
  overview: Overview | null
  kind: string | null
  name: string | null
}) {
  return (
    <>
      {RESOURCES.map((r) => (
        <ResourceSection
          key={r.kind}
          title={r.plural}
          resource={r.kind}
          items={r.items(overview)}
          activeName={r.kind === kind ? name : null}
        />
      ))}
    </>
  )
}

// strip a trailing ".md" for display (agents/steering rows keep it on disk).
function displayName(name: string): string {
  return name.replace(/\.md$/, '')
}

function ResourceSection({
  title,
  resource,
  items,
  activeName,
  defaultOpen = false,
}: {
  title: string
  resource: ResourceKind
  items: Artifact[] | undefined
  activeName: string | null
  defaultOpen?: boolean
}) {
  const list = items ?? []
  return (
    <Section title={title} count={list.length} defaultOpen={defaultOpen}>
      <div className="resource-list">
        {list.map((a) => {
          const short = displayName(a.name)
          return (
            <a
              key={a.path}
              className={`rail-row resource-row ${short === activeName || a.name === activeName ? 'active' : ''}`}
              href={href('resources', resource, short)}
              title={a.description || a.name}
            >
              <div className="resource-row-head">
                <span className="resource-name">{short}</span>
                {a.inclusion && <span className="badge muted">{a.inclusion}</span>}
              </div>
              {a.description && <div className="resource-desc">{a.description}</div>}
            </a>
          )
        })}
        {list.length === 0 && <div className="muted small">none</div>}
      </div>
    </Section>
  )
}

function Section({
  title,
  count,
  defaultOpen = true,
  children,
}: {
  title: string
  count?: number
  defaultOpen?: boolean
  children: ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <section className="side-section">
      <button className="side-heading" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <span className={`side-caret ${open ? 'open' : ''}`}>▸</span>
        <span className="side-title">{title}</span>
        {typeof count === 'number' && <span className="count">{count}</span>}
      </button>
      {open && <div className="side-body">{children}</div>}
    </section>
  )
}
