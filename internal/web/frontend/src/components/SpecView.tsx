import { useEffect, useState } from 'react'
import { api } from '../api'
import type { SpecDetail, SpecReport } from '../types'
import { Markdown } from './Markdown'
import { TaskBoard } from './TaskBoard'
import { ProgressBar, PhasePill, Stat, covColor } from './bits'

type Tab = 'overview' | 'requirements' | 'design' | 'tasks'

export function SpecView({
  feature,
  version,
  onBack,
}: {
  feature: string
  version: number
  onBack?: () => void
}) {
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

  const back = onBack && (
    <button className="back-link" onClick={onBack}>
      ‹ All specs
    </button>
  )

  if (err)
    return (
      <div className="spec-view">
        {back}
        <div className="banner error">{err}</div>
      </div>
    )
  if (!detail)
    return (
      <div className="spec-view">
        {back}
        <div className="empty">Loading {feature}…</div>
      </div>
    )

  const has = (name: string) => (detail.artifacts ?? []).includes(name)
  const tabs: { id: Tab; label: string; enabled: boolean }[] = [
    { id: 'overview', label: 'Overview', enabled: true },
    { id: 'requirements', label: 'Requirements', enabled: has('requirements.md') },
    { id: 'design', label: 'Design', enabled: has('design.md') },
    { id: 'tasks', label: 'Tasks', enabled: has('tasks.md') },
  ]

  return (
    <div className="spec-view">
      {back}
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
  // TDD-only displays (RED/GREEN task counters) are meaningless under the `unit`
  // flow, where tasks are implement→cover with no RED/GREEN markers. Treat an
  // absent flow as tdd (the historical default) so legacy specs keep their display.
  const isTdd = detail.developmentFlow !== 'unit'
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
            {isTdd && <Stat label="RED" value={detail.tasks.red} />}
            {isTdd && <Stat label="GREEN" value={detail.tasks.green} />}
          </div>
        </div>
      </div>

      {detail.report ? (
        <>
          <MetricsCard report={detail.report} red={detail.tasks.red} green={detail.tasks.green} isTdd={isTdd} />
          <TaskEvidenceCard report={detail.report} />
        </>
      ) : (
        <MetricsHint feature={detail.feature} />
      )}

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


function MetricsCard({ report, red, green, isTdd }: { report: SpecReport; red: number; green: number; isTdd: boolean }) {
  const t = report.tests
  const c = report.coverage
  const passRate = t && t.total > 0 ? Math.round((t.passed * 100) / t.total) : 0
  return (
    <div className="card">
      <div className="card-title">
        Test metrics
        {report.command && <span className="muted small">· {report.command}</span>}
      </div>
      <div className="metrics-grid">
        {t && (
          <div className="metric-block">
            <div className="metric-head">
              <span>Tests</span>
              {t.failed > 0 ? (
                <span className="badge warn">{t.failed} failed</span>
              ) : (
                <span className="badge ready">passing</span>
              )}
            </div>
            <div className="metric-line">
              <b>{t.passed}</b>/{t.total} passed{t.skipped > 0 ? ` · ${t.skipped} skipped` : ''}
            </div>
            <ProgressBar pct={passRate} />
          </div>
        )}
        {c && (
          <div className="metric-block">
            <div className="metric-head">
              <span>Coverage</span>
              <span className="muted small">
                {c.covered}/{c.lines} lines
              </span>
            </div>
            <div className="metric-line">
              <b style={{ color: covColor(c.pct) }}>{c.pct.toFixed(1)}%</b>
            </div>
            <ProgressBar pct={c.pct} />
          </div>
        )}
        {isTdd && (
          <div className="metric-block">
            <div className="metric-head">
              <span>TDD</span>
            </div>
            <div className="metric-line">
              <span className="tdd red">RED {red}</span> <span className="tdd green">GREEN {green}</span>
            </div>
          </div>
        )}
      </div>
      {report.attentions && report.attentions.length > 0 && (
        <ul className="attentions">
          {report.attentions.map((a, i) => (
            <li key={i} className="attention">
              {a}
            </li>
          ))}
        </ul>
      )}
      {report.testPaths && report.testPaths.length > 0 && (
        <div className="muted small evidence">
          evidence: {report.testPaths.map((p) => <code key={p}>{p}</code>).reduce((a, b) => <>{a} · {b}</>)}
        </div>
      )}
      <div className="muted small">updated {report.updatedAt}</div>
    </div>
  )
}

/**
 * compareTaskIds orders dotted task IDs the way a reader expects: 2 before 10,
 * and 1.2 before 1.10. Sorting them as strings gets both backwards, which makes
 * the table read as if tasks landed out of order.
 */
function compareTaskIds(a: string, b: string): number {
  const pa = a.split('.').map(Number)
  const pb = b.split('.').map(Number)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? -1) - (pb[i] ?? -1)
    if (d !== 0) return d
  }
  return 0
}

/**
 * TaskEvidenceCard shows what each task's own recorded run said.
 *
 * The rollup above it is the latest run and cannot name an owner: when several
 * implementers work a spec at once, the last writer wins and a red result loses
 * the task that produced it. This is the view of `SpecReport.tasks`, which the
 * CLI fills via `csdd spec test-report <feature> --run --task <id>`.
 */
function TaskEvidenceCard({ report }: { report: SpecReport }) {
  const entries = Object.entries(report.tasks ?? {}).sort(([a], [b]) => compareTaskIds(a, b))
  if (entries.length === 0) return null
  return (
    <div className="card">
      <div className="card-title">
        Evidence by task
        <span className="muted small">· {entries.length} recorded</span>
      </div>
      <div className="task-ev">
        {entries.map(([id, t]) => {
          const c = t.tests
          const failed = (c?.failed ?? 0) > 0
          const open = t.attentions ?? []
          return (
            <div key={id} className="task-ev-row">
              <code className="task-ev-id">{id}</code>
              <span className="task-ev-tests">
                {c ? (
                  <>
                    <b className={failed ? 'tdd red' : 'tdd green'}>
                      {c.passed}/{c.total}
                    </b>
                    {c.skipped > 0 && <span className="muted small"> · {c.skipped} skipped</span>}
                  </>
                ) : (
                  <span className="muted small">no counts</span>
                )}
              </span>
              <span className="task-ev-cov">
                {t.coverage ? (
                  <span style={{ color: covColor(t.coverage.pct) }}>{t.coverage.pct.toFixed(1)}%</span>
                ) : (
                  <span className="muted small">—</span>
                )}
              </span>
              <span className="task-ev-status">
                {failed ? (
                  <span className="badge warn">{c?.failed} failed</span>
                ) : open.length > 0 ? (
                  <span className="badge warn">
                    {open.length} attention{open.length > 1 ? 's' : ''}
                  </span>
                ) : (
                  <span className="badge ready">green</span>
                )}
              </span>
              <span className="muted small task-ev-when">{t.updatedAt}</span>
              {open.length > 0 && (
                <ul className="attentions task-ev-att">
                  {open.map((a, i) => (
                    <li key={i} className="attention">
                      {a}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function MetricsHint({ feature }: { feature: string }) {
  return (
    <div className="card">
      <div className="card-title">Test metrics</div>
      <div className="muted small">
        No <code>test-report.json</code> yet. Record one after running the suite:
      </div>
      <pre className="codeblock" style={{ marginTop: 8 }}>
        <code>npx @protonspy/csdd spec test-report {feature} --junit junit.xml --coverage coverage/lcov.info</code>
      </pre>
      <div className="muted small" style={{ marginTop: 6 }}>
        Or auto-discover reports by language (python · typescript · java · go · rust):{' '}
        <code>npx @protonspy/csdd spec test-report {feature} --lang go --path tests/</code>
      </div>
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
