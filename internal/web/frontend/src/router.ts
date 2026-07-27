import { useEffect, useState } from 'react'

// A hash router in ~40 lines, rather than a routing dependency.
//
// The dashboard is served from a Go binary with an index.html fallback, so a
// hash keeps every deep link working without the server knowing the route table.
// What matters is that a location IS a URL: the citation chips that come next
// ([[wiki]], adr:, stack:) have to be real anchors, and "the spec I am looking
// at" has to survive a reload and be pasteable into a review comment.

export type Page =
  | 'overview'
  | 'specs'
  | 'plans'
  | 'wiki'
  | 'adr'
  | 'stack'
  | 'glossary'
  | 'graph'
  | 'files'
  | 'resources'
  | 'tests'

/** The five primary areas. `page` maps onto one of them. */
export type Area = 'overview' | 'specs' | 'plans' | 'knowledge' | 'workspace'

export const AREA_OF: Record<Page, Area> = {
  overview: 'overview',
  specs: 'specs',
  plans: 'plans',
  wiki: 'knowledge',
  adr: 'knowledge',
  stack: 'knowledge',
  glossary: 'knowledge',
  graph: 'knowledge',
  files: 'workspace',
  resources: 'workspace',
  tests: 'workspace',
}

const PAGES = Object.keys(AREA_OF) as Page[]

export interface Route {
  page: Page
  area: Area
  /** First path segment after the page, decoded (a spec feature, a wiki slug…). */
  id: string | null
  /** Second segment — only `resources/<kind>/<name>` uses it today. */
  sub: string | null
  query: Record<string, string>
}

export function parseHash(hash: string): Route {
  const raw = hash.replace(/^#\/?/, '')
  const [path, search] = raw.split('?')
  const seg = path.split('/').filter(Boolean).map(decodeURIComponent)
  const page = (PAGES as string[]).includes(seg[0]) ? (seg[0] as Page) : 'overview'
  return {
    page,
    area: AREA_OF[page],
    id: seg[1] ?? null,
    sub: seg[2] ?? null,
    query: Object.fromEntries(new URLSearchParams(search ?? '')),
  }
}

/** Build a hash for a page. Segments are encoded; blank query values dropped. */
export function href(page: Page, ...segments: (string | null | undefined)[]): string {
  const tail = segments.filter((s): s is string => !!s).map(encodeURIComponent)
  return `#/${[page, ...tail].join('/')}`
}

export function hrefWithQuery(page: Page, query: Record<string, string>): string {
  const q = new URLSearchParams(Object.entries(query).filter(([, v]) => v))
  const s = q.toString()
  return `#/${page}${s ? `?${s}` : ''}`
}

export function navigate(to: string) {
  if (window.location.hash !== to) window.location.hash = to
}

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash))
  useEffect(() => {
    const onChange = () => setRoute(parseHash(window.location.hash))
    window.addEventListener('hashchange', onChange)
    // An empty hash is the landing route; normalise it so the tab reads as active.
    if (!window.location.hash) window.location.replace('#/overview')
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  return route
}
