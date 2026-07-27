import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { WikiOverview, WikiPage } from '../types'
import { Markdown } from './Markdown'
import { href } from '../router'
import { rewriteRefTokens } from './Ref'

// WikiView renders one page of docs/wiki/ — its markdown with clickable
// [[wikilinks]], its provenance (sources), and its backlinks. Page content is
// loaded through the hardened /api/file route, like PlansView's log.md.
//
// The catalog itself (index.md's categories and order) is the app rail's job
// now: the wiki no longer carries a second navigator inside the page.
export function WikiView({ slug, version }: { slug: string | null; version: number }) {
  const [ov, setOv] = useState<WikiOverview | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setErr(null)
    api.wiki().then(setOv).catch((e) => setErr(String(e)))
  }, [version])

  if (err) return <div className="banner error">{err}</div>
  if (!ov) return <div className="empty">Loading…</div>
  if (!ov.present) return <NoWiki />
  if (ov.pages.length === 0) return <EmptyWiki hasIndex={ov.has_index} />

  const current = (slug && ov.pages.find((p) => p.slug === slug)) || ov.pages[0]

  return (
    <div className="wiki-view">
      <section className="wiki-page">
        <WikiPagePane page={current} all={ov} version={version} />
      </section>
    </div>
  )
}

function WikiPagePane({ page, all, version }: { page: WikiPage; all: WikiOverview; version: number }) {
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setText(null)
    setErr(null)
    api
      .file(page.path)
      .then((f) => setText(f.text ?? ''))
      .catch((e) => setErr(String(e)))
  }, [page.path, version])

  // Citations in the body — [[wikilinks]] and adr:/stack: written in prose —
  // become chips resolved by /api/ref.
  const rendered = useMemo(() => (text == null ? '' : rewriteRefTokens(text)), [text])

  // Backlinks: pages whose resolved links point at this page.
  const backlinks = useMemo(
    () => all.pages.filter((p) => p.slug !== page.slug && p.links.some((l) => l.target === page.slug)),
    [all, page],
  )
  const broken = page.links.filter((l) => l.broken)

  return (
    <div className="wiki-page-inner">
      <header className="wiki-page-head">
        <h1>{page.title}</h1>
        <div className="wiki-page-meta">
          {!page.in_index && <span className="badge attention">not in index.md</span>}
          {(page.tags ?? []).map((t) => (
            <span key={t} className="badge muted">
              {t}
            </span>
          ))}
        </div>
      </header>

      {broken.length > 0 && (
        <div className="banner warn small">
          Broken link{broken.length > 1 ? 's' : ''}: {broken.map((l) => `[[${l.text}]]`).join(', ')}
        </div>
      )}

      <div className="wiki-body">
        {err ? (
          <div className="banner error">{err}</div>
        ) : text == null ? (
          <div className="empty">Loading…</div>
        ) : (
          <Markdown text={rendered} />
        )}
      </div>

      <footer className="wiki-page-foot">
        {(page.sources ?? []).length > 0 && (
          <div className="wiki-foot-block">
            <div className="wiki-foot-title">Sources</div>
            {(page.sources ?? []).map((s) => (
              <span key={s} className="badge muted" title="from the page's `sources:` frontmatter">
                {s}
              </span>
            ))}
          </div>
        )}
        {backlinks.length > 0 && (
          <div className="wiki-foot-block">
            <div className="wiki-foot-title">Backlinks</div>
            {backlinks.map((b) => (
              <a key={b.slug} className="chip-link" href={href('wiki', b.slug)}>
                {b.title}
              </a>
            ))}
          </div>
        )}
        <div className="wiki-foot-block">
          <span className="muted small">{page.path}</span>
        </div>
      </footer>
    </div>
  )
}

function NoWiki() {
  return (
    <div className="empty">
      <h2>No wiki yet</h2>
      <p className="muted">
        The wiki is the LLM-authored knowledge base under <code>docs/wiki/</code>. Scaffold it, then
        author pages with the <code>wiki</code> skill:
      </p>
      <pre className="codeblock" style={{ textAlign: 'left', maxWidth: 480, margin: '12px auto' }}>
        <code>{`npx @protonspy/csdd wiki init
# drop sources into docs/raw/, then author docs/wiki/pages/*.md
npx @protonspy/csdd wiki lint`}</code>
      </pre>
    </div>
  )
}

function EmptyWiki({ hasIndex }: { hasIndex: boolean }) {
  return (
    <div className="empty">
      <h2>Wiki is scaffolded but empty</h2>
      <p className="muted">
        No pages under <code>docs/wiki/pages/</code> yet.
        {hasIndex ? ' The index.md catalog is ready — add pages and list them there.' : ''}
      </p>
    </div>
  )
}
