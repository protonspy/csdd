import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { Selection } from '../App'
import type { Overview, SpecCard, TreeNode } from '../types'
import { api } from '../api'
import { FileTree } from './FileTree'
import { ProgressBar, PhasePill } from './bits'

interface Props {
  overview: Overview | null
  selection: Selection
  onSelect: (s: Selection) => void
  version: number
}

export function Sidebar({ overview, selection, onSelect, version }: Props) {
  const [tree, setTree] = useState<TreeNode[]>([])

  useEffect(() => {
    api.tree().then(setTree).catch(() => setTree([]))
  }, [version])

  const selectedFeature = selection?.kind === 'spec' ? selection.feature : null
  const selectedPath = selection?.kind === 'file' ? selection.path : null

  return (
    <aside className="sidebar">
      {overview && <WorkspaceChips overview={overview} />}

      <Section title="Specs" count={overview?.specs.length}>
        <div className="spec-list">
          {overview?.specs.map((s) => (
            <SpecRow
              key={s.feature}
              spec={s}
              active={s.feature === selectedFeature}
              onClick={() => onSelect({ kind: 'spec', feature: s.feature })}
            />
          ))}
          {overview && overview.specs.length === 0 && <div className="muted small">no specs</div>}
        </div>
      </Section>

      <Section title="Explorer">
        <FileTree
          nodes={tree}
          selectedPath={selectedPath}
          onOpenFile={(path) => onSelect({ kind: 'file', path })}
        />
      </Section>
    </aside>
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

function WorkspaceChips({ overview }: { overview: Overview }) {
  const chips: [string, number][] = [
    ['steering', overview.steering.length],
    ['skills', overview.skills.length],
    ['agents', overview.agents.length],
    ['mcp', overview.mcp.length],
    ['hooks', overview.hooks.length],
    ['commands', overview.commands.length],
  ]
  return (
    <div className="ws-chips">
      {chips
        .filter(([, n]) => n > 0)
        .map(([label, n]) => (
          <span className="chip" key={label}>
            <b>{n}</b> {label}
          </span>
        ))}
    </div>
  )
}

function Section({ title, count, children }: { title: string; count?: number; children: ReactNode }) {
  return (
    <section className="side-section">
      <div className="side-heading">
        {title}
        {typeof count === 'number' && <span className="count">{count}</span>}
      </div>
      {children}
    </section>
  )
}
