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

  // A route with no slug — or a slug that no longer exists — lands on the wiki's
  // own front door: docs/wiki/index.md. The catalog is authored, so it is the
  // page that says what this knowledge base is; opening whichever page happened
  // to sort first never did.
  const current = slug ? ov.pages.find((p) => p.slug === slug) : undefined

  return (
    <div className="wiki-view">
      <section className="wiki-page">
        {current ? (
          <WikiPagePane page={current} all={ov} version={version} />
        ) : ov.has_index ? (
          <WikiIndexPane all={ov} version={version} />
        ) : (
          <WikiPagePane page={ov.pages[0]} all={ov} version={version} />
        )}
      </section>
    </div>
  )
}

const WIKI_INDEX_PATH = 'docs/wiki/index.md'

// WikiIndexPane renders docs/wiki/index.md as the wiki's home page: the author's
// own prose and categories, not a landing page synthesised from the read model.
// Its entries point at files on disk, so they are rewritten into app routes
// before rendering — the catalog has to navigate.
function WikiIndexPane({ all, version }: { all: WikiOverview; version: number }) {
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setText(null)
    setErr(null)
    api
      .file(WIKI_INDEX_PATH)
      .then((f) => setText(f.text ?? ''))
      .catch((e) => setErr(String(e)))
  }, [version])

  const known = useMemo(() => new Set(all.pages.map((p) => p.slug)), [all])
  const { title, body } = useMemo(() => splitLeadingHeading(text ?? ''), [text])
  const rendered = useMemo(() => rewriteIndexEntries(rewriteRefTokens(body), known), [body, known])
  const unlisted = all.pages.filter((p) => !p.in_index)

  return (
    <div className="wiki-page-inner">
      <header className="wiki-page-head">
        <h1>{title || 'Wiki'}</h1>
        <div className="wiki-page-meta">
          <span className="badge muted">
            {all.pages.length} page{all.pages.length === 1 ? '' : 's'}
          </span>
          {unlisted.length > 0 && <span className="badge attention">{unlisted.length} not in index.md</span>}
        </div>
      </header>

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
        {unlisted.length > 0 && (
          <div className="wiki-foot-block">
            <div className="wiki-foot-title">Not in the index</div>
            {unlisted.map((p) => (
              <a key={p.slug} className="chip-link" href={href('wiki', p.slug)}>
                {p.title}
              </a>
            ))}
          </div>
        )}
        <div className="wiki-foot-block">
          <span className="muted small">{WIKI_INDEX_PATH}</span>
        </div>
      </footer>
    </div>
  )
}

// The pane header carries the title, so a leading `# Heading` is lifted out of
// the body rather than rendered a second time under it.
function splitLeadingHeading(text: string): { title: string; body: string } {
  const m = /^\s*#\s+(.+?)[ \t]*(?:\n|$)/.exec(text)
  if (!m) return { title: '', body: text }
  return { title: m[1], body: text.slice(m[0].length) }
}

// index.md links a page as `[Title](pages/<slug>.md)` because it is a real file
// beside them. In the dashboard those have to be routes: a slug that exists
// becomes an in-app link; one that does not becomes a citation token, so a stale
// entry renders as the broken reference it is instead of a dead file link.
const INDEX_ENTRY_RE = /\]\(\s*(?:\.\/)?(?:[^)\s]*\/)?pages\/([^)\s/]+)\.md\s*\)/g

function rewriteIndexEntries(markdown: string, known: Set<string>): string {
  return markdown.replace(INDEX_ENTRY_RE, (_m, slug: string) =>
    known.has(slug) ? `](${href('wiki', slug)})` : `](ref:[[${slug}]])`,
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
