import { useEffect } from 'react'

/**
 * Following a citation should say where it landed. When a route carries an
 * anchor (`#/stack?row=go`, `#/glossary?term=Public link`), scroll its row into
 * view and flash it once — otherwise the reader arrives at a table and has to
 * find the line the link meant.
 *
 * `deps` should include whatever loads the content, so the target is looked up
 * after it exists rather than before.
 */
export function useFlashTarget(target: string | null | undefined, deps: unknown[] = []) {
  useEffect(() => {
    if (!target) return
    const el = document.getElementById(`t-${slugifyAnchor(target)}`)
    if (!el) return
    el.classList.add('flash')
    el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    const t = window.setTimeout(() => el.classList.remove('flash'), 1800)
    return () => window.clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target, ...deps])
}

/** The id form used by anchor targets: lowercase, non-alphanumerics collapsed. */
export function slugifyAnchor(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
}
