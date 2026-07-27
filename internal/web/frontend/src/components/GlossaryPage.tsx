import { useEffect, useState } from 'react'
import { api } from '../api'
import type { GlossaryOverview } from '../types'
import { Panel } from './bits'
import { slugifyAnchor, useFlashTarget } from '../useFlashTarget'

// The ubiquitous language — docs/glossary.md.
//
// One canonical term per concept, and the synonyms it bans. The `_Avoid_` list
// is the working half: identifiers are minted from canonical terms, and using an
// avoided term as a whole token is a lint. Renaming never deletes the old name —
// it moves to the successor's Avoid list, so the tombstone outlives the rename.
export function GlossaryPage({ term, version }: { term: string | null; version: number }) {
  const [ov, setOv] = useState<GlossaryOverview | null>(null)
  const [err, setErr] = useState<string | null>(null)
  useFlashTarget(term, [ov])

  useEffect(() => {
    setErr(null)
    api.glossary().then(setOv).catch((e) => setErr(String(e)))
  }, [version])

  if (err) return <div className="banner error">{err}</div>
  if (!ov) return <div className="empty">Loading…</div>
  if (!ov.present) return <NoGlossary />
  if (ov.terms.length === 0) return <NoGlossary empty />

  // Grouped by the ### cluster the file itself declares, in file order.
  const clusters: { name: string; terms: typeof ov.terms }[] = []
  for (const t of ov.terms) {
    const name = t.cluster || ''
    const last = clusters[clusters.length - 1]
    if (last && last.name === name) last.terms.push(t)
    else clusters.push({ name, terms: [t] })
  }

  return (
    <div className="page-stack">
      <header className="page-head">
        <h1>Glossary</h1>
        <p className="page-sub">
          One canonical term per concept, and the synonyms it bans. Feat slugs, spec directories and wiki page
          names are minted from these — using an avoided term as a whole token is a lint, not a preference.
        </p>
      </header>

      {(ov.issues ?? []).length > 0 && (
        <div className="banner warn">
          <span>
            <strong>
              {ov.issues!.length} well-formedness issue{ov.issues!.length === 1 ? '' : 's'} in glossary.md.
            </strong>{' '}
            {ov.issues!.join(' · ')}
          </span>
        </div>
      )}

      {clusters.map((c, i) => (
        <Panel key={c.name || `cluster-${i}`} title={c.name || 'Terms'} right={<span className="faint xs">{c.terms.length}</span>}>
          {c.terms.map((t) => (
            <div className="term" key={t.canonical} id={`t-${slugifyAnchor(t.canonical)}`}>
              <div className="row-wrap">
                <span className="term-name">{t.canonical}</span>
              </div>
              <p className="term-def">{t.definition || <span className="faint">no definition recorded</span>}</p>
              {(t.avoid ?? []).length > 0 && (
                <div className="row-wrap">
                  <span className="faint xs">avoid</span>
                  {t.avoid!.map((a) => (
                    <span key={a} className="avoid" title="banned synonym — a lint when used as a whole token">
                      ✕ <span className="strike">{a}</span>
                    </span>
                  ))}
                </div>
              )}
            </div>
          ))}
        </Panel>
      ))}

      <div className="banner info">
        <span>
          Renaming a term never removes the old one: it moves to the successor's <code>_Avoid_</code> list in
          the same edit, so the old name stays banned forever.
        </span>
      </div>
    </div>
  )
}

function NoGlossary({ empty = false }: { empty?: boolean }) {
  return (
    <div className="empty">
      <h2>{empty ? 'No terms yet' : 'No glossary'}</h2>
      <p className="muted">
        <code>docs/glossary.md</code> is created lazily, on the first term worth pinning down. Terms live
        under a <code>## Language</code> heading, optionally grouped by <code>###</code> clusters:
      </p>
      <pre className="codeblock" style={{ textAlign: 'left', maxWidth: 520, margin: '12px auto' }}>
        <code>{`## Language

### Access

**Public link**: a share whose recipient is anyone holding the URL.
_Avoid_: magic link, anonymous link`}</code>
      </pre>
    </div>
  )
}
