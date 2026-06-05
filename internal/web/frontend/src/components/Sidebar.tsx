import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { ResourceKind, Selection } from '../App'
import type { Artifact, Overview, SpecCard, WorkspaceTree } from '../types'
import { api } from '../api'
import { FileTree } from './FileTree'
import { ProgressBar, PhasePill } from './bits'

interface Props {
  overview: Overview | null
  selection: Selection
  onSelect: (s: Selection) => void
  version: number
  open: boolean
}

export function Sidebar({ overview, selection, onSelect, version, open }: Props) {
  const [tree, setTree] = useState<WorkspaceTree>({ csdd: [], project: [] })

  useEffect(() => {
    api
      .tree()
      .then((t) => setTree({ csdd: t?.csdd ?? [], project: t?.project ?? [] }))
      .catch(() => setTree({ csdd: [], project: [] }))
  }, [version])

  const selectedFeature = selection?.kind === 'spec' ? selection.feature : null
  const selectedPath = selection?.kind === 'file' ? selection.path : null
  const selectedResource = selection?.kind === 'resource' ? selection.artifact.path : null
  const openFile = (path: string) => onSelect({ kind: 'file', path })
  const openResource = (resource: ResourceKind, artifact: Artifact) =>
    onSelect({ kind: 'resource', resource, artifact })

  const specCount = overview?.specs?.length ?? 0

  return (
    <aside className={`sidebar ${open ? 'open' : ''}`}>
      <nav className="side-nav">
        <button
          className={`nav-row ${selection?.kind === 'tests' ? 'active' : ''}`}
          onClick={() => onSelect({ kind: 'tests' })}
        >
          <span className="nav-icon">✓</span> Tests &amp; Coverage
        </button>
      </nav>

      <Section title="Specs" count={specCount} defaultOpen>
        <div className="spec-list">
          {(overview?.specs ?? []).map((s) => (
            <SpecRow
              key={s.feature}
              spec={s}
              active={s.feature === selectedFeature}
              onClick={() => onSelect({ kind: 'spec', feature: s.feature })}
            />
          ))}
          {overview && specCount === 0 && <div className="muted small">no specs</div>}
        </div>
      </Section>

      <SideGroup label="Resources">
        <ResourceSection
          title="Agents"
          resource="agent"
          items={overview?.agents}
          selectedPath={selectedResource}
          onOpen={openResource}
        />
        <ResourceSection
          title="Skills"
          resource="skill"
          items={overview?.skills}
          selectedPath={selectedResource}
          onOpen={openResource}
        />
        <ResourceSection
          title="Steering"
          resource="steering"
          items={overview?.steering}
          selectedPath={selectedResource}
          onOpen={openResource}
        />
      </SideGroup>

      <SideGroup label="Files">
        <Section title="csdd" count={tree.csdd.length} defaultOpen={false}>
          <FileTree nodes={tree.csdd} selectedPath={selectedPath} onOpenFile={openFile} />
        </Section>
        <Section title="Project" count={tree.project.length} defaultOpen={false}>
          <FileTree nodes={tree.project} selectedPath={selectedPath} onOpenFile={openFile} />
        </Section>
      </SideGroup>

      {overview && <WorkspaceChips overview={overview} />}
    </aside>
  )
}

// SideGroup is a labelled band that visually groups related sections.
function SideGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="side-group">
      <div className="side-group-label">{label}</div>
      {children}
    </div>
  )
}

function SpecRow({ spec, active, onClick }: { spec: SpecCard; active: boolean; onClick: () => void }) {
  return (
    <button className={`spec-row ${active ? 'active' : ''}`} onClick={onClick}>
      <div className="spec-row-head">
        <span className="spec-name">{spec.feature}</span>
        {spec.ready ? (
          <span className="badge ready" title="ready for implementation">
            ready
          </span>
        ) : spec.issues > 0 ? (
          <span className="badge warn" title={`${spec.issues} validation issue(s)`}>
            {spec.issues}
          </span>
        ) : null}
      </div>
      <div className="spec-row-meta">
        <PhasePill phase={spec.phase} />
        {spec.tasks.total > 0 && (
          <span className="muted small">
            {spec.tasks.done}/{spec.tasks.total}
          </span>
        )}
      </div>
      {spec.tasks.total > 0 && <ProgressBar pct={spec.tasks.pct} />}
    </button>
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
  selectedPath,
  onOpen,
}: {
  title: string
  resource: ResourceKind
  items: Artifact[] | undefined
  selectedPath: string | null
  onOpen: (resource: ResourceKind, a: Artifact) => void
}) {
  const list = items ?? []
  return (
    <Section title={title} count={list.length} defaultOpen={false}>
      <div className="resource-list">
        {list.map((a) => (
          <button
            key={a.path}
            className={`resource-row ${a.path === selectedPath ? 'active' : ''}`}
            onClick={() => onOpen(resource, a)}
            title={a.description || a.name}
          >
            <div className="resource-row-head">
              <span className="resource-name">{displayName(a.name)}</span>
              {a.inclusion && <span className="badge muted">{a.inclusion}</span>}
            </div>
            {a.description && <div className="resource-desc">{a.description}</div>}
          </button>
        ))}
        {list.length === 0 && <div className="muted small">none</div>}
      </div>
    </Section>
  )
}

function WorkspaceChips({ overview }: { overview: Overview }) {
  const chips: [string, number][] = [
    ['mcp', overview.mcp?.length ?? 0],
    ['hooks', overview.hooks?.length ?? 0],
    ['commands', overview.commands?.length ?? 0],
  ]
  const shown = chips.filter(([, n]) => n > 0)
  if (shown.length === 0) return null
  return (
    <div className="ws-chips">
      {shown.map(([label, n]) => (
        <span className="chip" key={label}>
          <b>{n}</b> {label}
        </span>
      ))}
    </div>
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
