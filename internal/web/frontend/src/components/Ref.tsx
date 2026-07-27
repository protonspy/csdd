import { createContext, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import { api } from '../api'
import type { RefResolution } from '../types'

// The citation chip.
//
// Anatomy is fixed everywhere a citation appears — a Refs cell, a wiki page, a
// spec body:
//
//     [dot]  kind:  label   [state]
//
// The dot carries the identity hue and the text stays in ink. Only three kinds
// get a hue ([[wiki]], adr:, stack:) because those are the ones that appear side
// by side in one Refs cell; the rest are told apart by their prefix, which was
// always the real identity channel.
//
// Resolution is not guessed here. Every token goes to /api/ref, which answers
// from the same parsers `csdd plan validate` uses — so a chip and the gate can
// never disagree about whether a citation is broken.

// ---------------------------------------------------------------------------
// The store: one batched request per burst of chips.

interface Store {
  get(token: string): RefResolution | undefined
  request(token: string): void
  subscribe(fn: () => void): () => void
  snapshot(): number
}

function unresolvable(token: string): RefResolution {
  return { token, kind: 'unknown', label: token, state: 'broken', title: 'could not be resolved' }
}

function createStore(): Store {
  const cache = new Map<string, RefResolution>()
  const pending = new Set<string>()
  const inFlight = new Set<string>()
  const listeners = new Set<() => void>()
  let version = 0
  let scheduled = false

  const notify = () => {
    version++
    for (const fn of listeners) fn()
  }

  const flush = () => {
    scheduled = false
    const batch = [...pending]
    pending.clear()
    if (batch.length === 0) return
    for (const t of batch) inFlight.add(t)
    api
      .refs(batch)
      .then((resolutions) => {
        for (const r of resolutions) cache.set(r.token, r)
        // A token the server did not answer must not be retried forever.
        for (const t of batch) if (!cache.has(t)) cache.set(t, unresolvable(t))
      })
      .catch(() => {
        for (const t of batch) cache.set(t, unresolvable(t))
      })
      .finally(() => {
        for (const t of batch) inFlight.delete(t)
        notify()
      })
  }

  return {
    get: (token) => cache.get(token),
    request(token) {
      if (!token || cache.has(token) || pending.has(token) || inFlight.has(token)) return
      pending.add(token)
      if (!scheduled) {
        scheduled = true
        // A microtask, so every chip mounted in one render lands in one request.
        queueMicrotask(flush)
      }
    },
    subscribe(fn) {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    snapshot: () => version,
  }
}

const RefStoreContext = createContext<Store | null>(null)

/**
 * Provides citation resolution to everything below it. `version` is the SSE
 * change counter: when the workspace changes on disk, every resolution is stale
 * (a record may have been added, superseded, or deleted), so the cache is
 * dropped and re-fetched lazily.
 */
export function RefProvider({ version, children }: { version: number; children: ReactNode }) {
  const [store, setStore] = useState(() => createStore())
  const first = useRef(true)
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    setStore(createStore())
  }, [version])
  return <RefStoreContext.Provider value={store}>{children}</RefStoreContext.Provider>
}

/** Resolve one token, requesting it if this is the first time it is seen. */
export function useRefResolution(token: string): RefResolution | undefined {
  const store = useContext(RefStoreContext)
  useSyncExternalStore(
    (fn) => store?.subscribe(fn) ?? (() => {}),
    () => store?.snapshot() ?? 0,
    () => 0,
  )
  useEffect(() => {
    store?.request(token)
  }, [store, token])
  return store?.get(token)
}

// ---------------------------------------------------------------------------
// The chip

const HUED = new Set(['wiki', 'adr', 'stack'])

/** The `kind:` prefix a chip shows. Wikilinks show none — the brackets are gone
 *  but the blue dot and the bare slug are how a wiki page has always read. */
function prefixFor(token: string): string {
  const m = /^(adr|stack|spec|feat|term):/.exec(token)
  return m ? m[1] + ':' : ''
}

export function Ref({ token, label }: { token: string; label?: string }) {
  const r = useRefResolution(token)
  const kind = r?.kind ?? kindFromToken(token)
  const state = r?.state ?? 'ok'
  const cls = [
    'ref',
    HUED.has(kind) ? `ref-${kind}` : '',
    state === 'broken' ? 'ref-broken' : '',
    state === 'ambiguous' ? 'ref-broken' : '',
    state === 'superseded' ? 'ref-superseded' : '',
    r ? '' : 'ref-pending',
  ]
    .filter(Boolean)
    .join(' ')

  const text = label ?? r?.label ?? stripPrefix(token)
  const inner = (
    <>
      {prefixFor(token) && <span className="ref-kind">{prefixFor(token)}</span>}
      <span className="ref-label">{text}</span>
      {state === 'superseded' && <span className="ref-state">superseded</span>}
      {(state === 'broken' || state === 'ambiguous') && <span className="ref-state">⚠</span>}
    </>
  )

  const tip = r ? `${r.title ?? r.label}${r.meta ? ` — ${r.meta}` : ''}` : token

  if (!r?.route) {
    return (
      <span className={cls} title={tip} data-ref={token}>
        {inner}
      </span>
    )
  }
  return (
    <a className={cls} title={tip} data-ref={token} href={r.route}>
      {inner}
    </a>
  )
}

/**
 * A wrapped row of chips — a Refs cell. `empty=""` renders nothing at all,
 * for layouts where an empty cell should collapse rather than hold a dash.
 */
export function RefList({ tokens, empty = '—' }: { tokens: string[] | undefined; empty?: string }) {
  if (!tokens || tokens.length === 0) return empty ? <span className="faint small">{empty}</span> : null
  return (
    <span className="ref-list">
      {tokens.map((t) => (
        <Ref key={t} token={t} />
      ))}
    </span>
  )
}

/**
 * The hover/focus preview: the citation resolved in place, so following a
 * reference is a choice rather than the only way to find out what it says.
 * One node for the whole page, positioned at whatever chip is hovered.
 */
export function RefPreview() {
  const [token, setToken] = useState<string | null>(null)
  const [box, setBox] = useState<DOMRect | null>(null)
  const r = useRefResolution(token ?? '')

  useEffect(() => {
    const enter = (e: Event) => {
      const el = (e.target as HTMLElement | null)?.closest('[data-ref]') as HTMLElement | null
      if (!el) return
      setToken(el.getAttribute('data-ref'))
      setBox(el.getBoundingClientRect())
    }
    const leave = (e: Event) => {
      if ((e.target as HTMLElement | null)?.closest('[data-ref]')) setToken(null)
    }
    document.addEventListener('mouseover', enter)
    document.addEventListener('mouseout', leave)
    document.addEventListener('focusin', enter)
    document.addEventListener('focusout', leave)
    window.addEventListener('hashchange', () => setToken(null))
    return () => {
      document.removeEventListener('mouseover', enter)
      document.removeEventListener('mouseout', leave)
      document.removeEventListener('focusin', enter)
      document.removeEventListener('focusout', leave)
    }
  }, [])

  const pos = useMemo(() => {
    if (!box) return null
    const width = 340
    const left = Math.max(8, Math.min(box.left, window.innerWidth - width - 12))
    const below = box.bottom + 8
    const flip = below + 180 > window.innerHeight && box.top > 190
    return { left, top: flip ? Math.max(8, box.top - 188) : below, width }
  }, [box])

  if (!token || !pos || !r) return null

  return (
    <div className="ref-preview" role="tooltip" style={{ left: pos.left, top: pos.top, width: pos.width }}>
      <div className="ref-preview-head">
        <span className="ref-preview-kind">{prefixFor(token) || '[[…]]'}</span>
        <span className="ref-preview-spacer" />
        <RefStateBadge state={r.state} />
      </div>
      <div className="ref-preview-title">{r.title || r.label}</div>
      {r.body && <div className="ref-preview-body">{r.body}</div>}
      {r.successor && (
        <div className="ref-preview-body" style={{ marginTop: 6 }}>
          → cite <strong>{stripPrefix(r.successor)}</strong> instead
        </div>
      )}
      <div className="ref-preview-foot">
        <span>{r.meta}</span>
        {r.route && <span>click to open</span>}
      </div>
    </div>
  )
}

function RefStateBadge({ state }: { state: RefResolution['state'] }) {
  if (state === 'ok') return <span className="badge ok">resolves</span>
  if (state === 'superseded') return <span className="badge attention">superseded</span>
  if (state === 'ambiguous') return <span className="badge warn">ambiguous</span>
  return <span className="badge warn">does not resolve</span>
}

function kindFromToken(token: string): string {
  if (token.startsWith('[[')) return 'wiki'
  const m = /^(adr|stack|spec|feat|term):/.exec(token)
  return m ? m[1] : 'unknown'
}

function stripPrefix(token: string): string {
  const wiki = /^\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]$/.exec(token)
  if (wiki) return wiki[1]
  return token.replace(/^(adr|stack|spec|feat|term):/, '')
}

// ---------------------------------------------------------------------------
// Prose

const TOKEN_RE = /\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]|\b(adr|stack|spec|feat|term):([A-Za-z0-9][A-Za-z0-9._/-]*)/g

/**
 * Rewrite the citation tokens in a markdown body into `ref:<token>` links,
 * which Markdown renders as chips. This is what makes a citation written
 * mid-sentence a working link without the author writing any markup.
 *
 * Fenced and inline code are left alone: `adr:` inside a code sample is a
 * string, not a citation.
 */
export function rewriteRefTokens(markdown: string): string {
  const parts = markdown.split(/(```[\s\S]*?```|`[^`\n]*`)/g)
  return parts
    .map((part, i) => (i % 2 === 1 ? part : part.replace(TOKEN_RE, (m) => `[${stripPrefix(m)}](ref:${m})`)))
    .join('')
}
