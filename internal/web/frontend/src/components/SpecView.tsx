import { useEffect, useState } from 'react'
import { api } from '../api'
import type { SpecDetail } from '../types'
import { Markdown } from './Markdown'
import { TaskBoard } from './TaskBoard'
import { ProgressBar, PhasePill } from './bits'

type Tab = 'overview' | 'requirements' | 'design' | 'tasks'

export function SpecView({ feature, version }: { feature: string; version: number }) {
  const [detail, setDetail] = useState<SpecDetail | null>(null)
  const [tab, setTab] = useState<Tab>('overview')
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setErr(null)
    api.spec(feature).then(setDetail).catch((e) => setErr(String(e)))
  }, [feature, version])

  useEffect(() => {
    setTab('overview')
  }, [feature])

  if (err) return <div className="banner error">{err}</div>
  if (!detail) return <div className="empty">Loading {feature}…</div>

  const has = (name: string) => (detail.artifacts ?? []).includes(name)
  const tabs: { id: Tab; label: string; enabled: boolean }[] = [
    { id: 'overview', label: 'Overview', enabled: true },
    { id: 'requirements', label: 'Requirements', enabled: has('requirements.md') },
    { id: 'design', label: 'Design', enabled: has('design.md') },
    { id: 'tasks', label: 'Tasks', enabled: has('tasks.md') },
  ]

  return (
    <div className="spec-view">
      <div className="spec-header">
        <h1>{detail.feature}</h1>
        <PhasePill phase={detail.phase} />
        {detail.ready && <span className="badge ready">ready</span>}
        {detail.tasks.total > 0 && (
          <span className="muted">
            {detail.tasks.pct}% · {detail.tasks.done}/{detail.tasks.total} tasks
          </span>
        )}
      </div>
      {detail.tasks.total > 0 && <ProgressBar pct={detail.tasks.pct} />}

      <nav className="tabs">
        {tabs.map((t) => (
          <button
            key={t.id}
            className={`tab ${tab === t.id ? 'active' : ''}`}
            disabled={!t.enabled}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </nav>

      <div className="tab-body">
        {tab === 'overview' && <OverviewTab detail={detail} />}
        {tab === 'requirements' && <ArtifactTab feature={feature} file="requirements.md" version={version} />}
        {tab === 'design' && <ArtifactTab feature={feature} file="design.md" version={version} />}
        {tab === 'tasks' && <TaskBoard phases={detail.phases} />}
      </div>
    </div>
  )
}

function OverviewTab({ detail }: { detail: SpecDetail }) {
  const phases = ['requirements', 'design', 'tasks']
  const issues = detail.issueList ?? []
  return (
    <div className="overview-tab">
      <div className="cards">
        <div className="card">
          <div className="card-title">Approvals</div>
          <div className="approvals">
            {phases.map((p) => {
              const a = detail.approvals[p]
              const state = a?.approved ? 'approved' : a?.generated ? 'generated' : 'missing'
              return (
                <div className="approval" key={p}>
                  <span className={`adot ${state}`} />
                  <span className="aname">{p}</span>
                  <span className="astate">{state}</span>
                </div>
              )
            })}
          </div>
        </div>
        <div className="card">
          <div className="card-title">Tasks</div>
          <div className="stat-grid">
            <Stat label="total" value={detail.tasks.total} />
            <Stat label="done" value={detail.tasks.done} />
            <Stat label="RED" value={detail.tasks.red} />
            <Stat label="GREEN" value={detail.tasks.green} />
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-title">
          Validation{' '}
          {issues.length === 0 ? (
            <span className="badge ready">passing</span>
          ) : (
            <span className="badge warn">{issues.length}</span>
          )}
        </div>
        {issues.length === 0 ? (
          <div className="muted small">No mechanical issues for the current phase.</div>
        ) : (
          <ul className="issues">
            {issues.map((is, i) => (
              <li key={i}>
                <code>
                  {is.file}
                  {is.line > 0 ? `:${is.line}` : ''}
                </code>{' '}
                {is.msg}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat">
      <div className="stat-val">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}

function ArtifactTab({ feature, file, version }: { feature: string; file: string; version: number }) {
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setText(null)
    setErr(null)
    api
      .file(`specs/${feature}/${file}`)
      .then((f) => setText(f.text))
      .catch((e) => setErr(String(e)))
  }, [feature, file, version])

  if (err) return <div className="banner error">{err}</div>
  if (text === null) return <div className="empty">Loading {file}…</div>
  return <Markdown text={text} />
}
