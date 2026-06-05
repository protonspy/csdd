import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { ResourceKind, Selection, View } from '../App'
import type { Artifact, Overview, WorkspaceTree } from '../types'
import { api } from '../api'
import { FileTree } from './FileTree'

interface Props {
  overview: Overview | null
  view: View
  selection: Selection
  onSelect: (s: Selection) => void
  version: number
  open: boolean
}

// Sidebar is the contextual navigator for the Resources and Files views (Specs
// and Tests are full-width pages, so they render no sidebar).
export function Sidebar({ overview, view, selection, onSelect, version, open }: Props) {
  const [tree, setTree] = useState<WorkspaceTree>({ csdd: [], project: [] })

  useEffect(() => {
    if (view !== 'files') return
    api
      .tree()
      .then((t) => setTree({ csdd: t?.csdd ?? [], project: t?.project ?? [] }))
      .catch(() => setTree({ csdd: [], project: [] }))
  }, [version, view])

  const selectedPath = selection?.kind === 'file' ? selection.path : null
  const selectedResource = selection?.kind === 'resource' ? selection.artifact.path : null
  const openFile = (path: string) => onSelect({ kind: 'file', path })
  const openResource = (resource: ResourceKind, artifact: Artifact) =>
    onSelect({ kind: 'resource', resource, artifact })

  return (
    <aside className={`sidebar ${open ? 'open' : ''}`}>
      {view === 'resources' && (
        <>
          <ResourceSection title="Agents" resource="agent" items={overview?.agents} selectedPath={selectedResource} onOpen={openResource} defaultOpen />
          <ResourceSection title="Skills" resource="skill" items={overview?.skills} selectedPath={selectedResource} onOpen={openResource} defaultOpen />
          <ResourceSection title="Steering" resource="steering" items={overview?.steering} selectedPath={selectedResource} onOpen={openResource} defaultOpen />
        </>
      )}

      {view === 'files' && (
        <>
          <Section title="csdd" count={tree.csdd.length} defaultOpen>
            <FileTree nodes={tree.csdd} selectedPath={selectedPath} onOpenFile={openFile} />
          </Section>
          <Section title="Project" count={tree.project.length} defaultOpen={false}>
            <FileTree nodes={tree.project} selectedPath={selectedPath} onOpenFile={openFile} />
          </Section>
        </>
      )}
    </aside>
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
  defaultOpen = false,
}: {
  title: string
  resource: ResourceKind
  items: Artifact[] | undefined
  selectedPath: string | null
  onOpen: (resource: ResourceKind, a: Artifact) => void
  defaultOpen?: boolean
}) {
  const list = items ?? []
  return (
    <Section title={title} count={list.length} defaultOpen={defaultOpen}>
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
