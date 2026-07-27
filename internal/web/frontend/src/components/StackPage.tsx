import { useEffect, useState } from 'react'
import { api } from '../api'
import type { StackOverview } from '../types'
import { Panel } from './bits'
import { RefList } from './Ref'
import { slugifyAnchor, useFlashTarget } from '../useFlashTarget'

// The tech contract — the docs/stack.md Decided table.
//
// The rule the page is built around: a technology that is not listed is an open
// decision, not a default. So the columns that matter are Why (the trade-off, in
// one line) and who cites the row — a decided technology nothing cites is either
// about to be used or should be removed.
export function StackPage({ row, version }: { row: string | null; version: number }) {
  const [ov, setOv] = useState<StackOverview | null>(null)
  const [err, setErr] = useState<string | null>(null)
  useFlashTarget(row, [ov])

  useEffect(() => {
    setErr(null)
    api.stack().then(setOv).catch((e) => setErr(String(e)))
  }, [version])

  if (err) return <div className="banner error">{err}</div>
  if (!ov) return <div className="empty">Loading…</div>
  if (!ov.present) return <NoContract />
  if (ov.rows.length === 0) return <NoContract empty />

  return (
    <div className="page-stack">
      <header className="page-head">
        <h1>Tech stack</h1>
        <p className="page-sub">
          The contract is law: a technology <b>not</b> listed here is an open decision — propose options and
          ask, never adopt a dependency silently. Each row is refined against current documentation before its
          first use.
        </p>
      </header>

      <Panel title={`Decided · ${ov.rows.length}`} right={<span className="faint xs">docs/stack.md</span>} flush>
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>Domain</th>
                <th>Choice</th>
                <th>Version</th>
                <th>Why</th>
                <th>Refs</th>
                <th>Cited by</th>
              </tr>
            </thead>
            <tbody>
              {ov.rows.map((r) => (
                <tr key={r.name} id={`t-${slugifyAnchor(r.name)}`}>
                  <td className="tight">
                    <span className="cell-sub">{r.domain || '—'}</span>
                  </td>
                  <td className="tight">
                    <div className="cell-title">{r.choice}</div>
                    <div className="cell-sub mono">stack:{r.name}</div>
                  </td>
                  <td className="tight mono">{r.version || '—'}</td>
                  <td>{r.why || <span className="faint">no reason recorded</span>}</td>
                  <td>
                    <RefList tokens={r.refs} />
                  </td>
                  <td>
                    <RefList tokens={r.cited_by} empty="nothing yet" />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>

      <div className="banner info">
        <span>
          A row with no <b>Why</b> is a decision nobody has justified, and a row nothing cites is a
          dependency nobody is using. Both are worth a second look before the next feat starts.
        </span>
      </div>
    </div>
  )
}

function NoContract({ empty = false }: { empty?: boolean }) {
  return (
    <div className="empty">
      <h2>{empty ? 'The contract is empty' : 'No tech contract'}</h2>
      <p className="muted">
        <code>docs/stack.md</code> holds a <code>## Decided</code> table: one row per technology, with the
        domain it covers, the version, and the one-line reason it won. Until a technology is listed there it
        is an open decision.
      </p>
      <pre className="codeblock" style={{ textAlign: 'left', maxWidth: 520, margin: '12px auto' }}>
        <code>{`## Decided

| Domain | Choice | Version | Why | Refs |
|---|---|---|---|---|
| Database | PostgreSQL | 16 | Relational data wants real constraints. | [[storage-design]] |`}</code>
      </pre>
    </div>
  )
}
