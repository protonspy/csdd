import { useEffect, useState } from 'react'
import { api } from '../api'
import type { ADROverview, ADRRecord } from '../types'
import { Markdown } from './Markdown'
import { Panel } from './bits'
import { Ref, RefList, rewriteRefTokens } from './Ref'
import { href } from '../router'

// Decision records — docs/adr/, append-only.
//
// The two things this page exists to make visible: a record is never edited
// once superseded (it is marked and its successor is written), and every record
// carries who cites it. A decision nobody cites is either new or dead, and both
// are worth seeing.
export function AdrPage({ slug, version }: { slug: string | null; version: number }) {
  const [ov, setOv] = useState<ADROverview | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setErr(null)
    api.adr().then(setOv).catch((e) => setErr(String(e)))
  }, [version])

  if (err) return <div className="banner error">{err}</div>
  if (!ov) return <div className="empty">Loading…</div>
  if (!ov.present) return <NoRecords />
  if (ov.records.length === 0) return <NoRecords empty />

  if (slug) {
    const rec = ov.records.find((r) => r.slug === slug)
    if (!rec) {
      return (
        <div className="empty">
          <h2>No such record</h2>
          <p className="muted">
            Nothing under <code>docs/adr/</code> carries the slug <code>{slug}</code>.{' '}
            <a className="link-btn" href="#/adr">
              All decision records
            </a>
          </p>
        </div>
      )
    }
    return <AdrDetail rec={rec} all={ov.records} />
  }

  return (
    <div className="page-stack">
      <header className="page-head">
        <h1>Decision records</h1>
        <p className="page-sub">
          A decision earns a record when it passes the triple gate: hard to reverse, surprising without
          context, the result of a real trade-off. Everything else is prose.
        </p>
      </header>

      <Panel title={`Records · ${ov.records.length}`}>
        {ov.records.map((rec) => (
          <div className="adr-item" key={rec.slug}>
            <span className="adr-num">{String(rec.number).padStart(4, '0')}</span>
            <div className="adr-body">
              <div className="adr-title">
                <a href={href('adr', rec.slug)}>{rec.title || rec.slug}</a>
                <StatusBadge rec={rec} />
              </div>
              {rec.body && <p className="adr-text">{rec.body}</p>}
              <div className="row-wrap">
                <span className="faint xs">cited by</span>
                <RefList tokens={rec.cited_by} empty="nothing yet" />
              </div>
            </div>
          </div>
        ))}
      </Panel>
    </div>
  )
}

function AdrDetail({ rec, all }: { rec: ADRRecord; all: ADRRecord[] }) {
  const supersedes = all.filter((r) => r.superseded_by === rec.number)
  return (
    <div className="page-stack">
      <header className="page-head">
        <nav className="crumbs">
          <a href="#/adr">Decisions</a> <span>›</span> <span className="mono">{String(rec.number).padStart(4, '0')}</span>
        </nav>
        <h1>{rec.title || rec.slug}</h1>
        <div className="row-wrap" style={{ marginTop: 6 }}>
          <StatusBadge rec={rec} />
          <span className="mono faint xs">adr:{rec.slug}</span>
        </div>
      </header>

      {rec.superseded_by_slug && (
        <div className="banner warn">
          <span>
            <strong>Superseded.</strong> This record is history — cite <Ref token={`adr:${rec.superseded_by_slug}`} />{' '}
            instead. The record itself is never rewritten; that is what keeps the trail readable.
          </span>
        </div>
      )}

      <article className="card">
        <div className="markdown-host">
          <Markdown text={rewriteRefTokens(rec.body || '_No body recorded._')} />
        </div>
      </article>

      <div className="overview-cols">
        <Panel title="Cited by">
          <RefList tokens={rec.cited_by} empty="no feat cites this record" />
        </Panel>
        <Panel title="Record">
          <div className="stack-col">
            <span className="path">{rec.file}</span>
            {supersedes.length > 0 && (
              <div className="row-wrap">
                <span className="faint xs">supersedes</span>
                <RefList tokens={supersedes.map((r) => `adr:${r.slug}`)} />
              </div>
            )}
            <span className="faint xs">Append-only. A superseded record is marked, never edited.</span>
          </div>
        </Panel>
      </div>
    </div>
  )
}

function StatusBadge({ rec }: { rec: ADRRecord }) {
  if (rec.status === 'superseded') {
    return (
      <span className="badge attention">
        superseded{rec.superseded_by ? ` by ${String(rec.superseded_by).padStart(4, '0')}` : ''}
      </span>
    )
  }
  return <span className="badge ok">accepted</span>
}

function NoRecords({ empty = false }: { empty?: boolean }) {
  return (
    <div className="empty">
      <h2>{empty ? 'No records yet' : 'No decision records'}</h2>
      <p className="muted">
        A decision that is <b>hard to reverse</b>, <b>surprising without context</b>, and <b>the result of a
        real trade-off</b> belongs in <code>docs/adr/NNNN-slug.md</code>. Feats then cite it as{' '}
        <code>adr:&lt;slug&gt;</code>, and <code>csdd plan validate</code> breaks on a citation that stops
        resolving.
      </p>
      <p className="muted">A decision that fails that gate is prose — that is what stops record spam.</p>
    </div>
  )
}
