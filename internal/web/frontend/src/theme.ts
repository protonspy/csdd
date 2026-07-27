// Theme state. The active theme is stamped on <html data-theme>, which is what
// every CSS token in tokens.css keys off — so CSS needs nothing from this module
// at runtime.
//
// Three consumers cannot read CSS custom properties, because they paint outside
// the cascade: the vis-network canvas, the Monaco editor and Mermaid. They read
// the resolved values through `cssVar()` and re-read them when `subscribe` fires.

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'csdd-theme'
const listeners = new Set<(t: Theme) => void>()

let current: Theme = 'dark'

function stored(): Theme | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return v === 'dark' || v === 'light' ? v : null
  } catch {
    return null // private mode / storage disabled
  }
}

/** Resolve the startup theme and stamp it. Called once, before the app mounts. */
export function initTheme(): Theme {
  const preferred =
    stored() ?? (window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark')
  apply(preferred)
  return preferred
}

function apply(t: Theme) {
  current = t
  document.documentElement.dataset.theme = t
}

export function getTheme(): Theme {
  return current
}

export function setTheme(t: Theme) {
  if (t === current) return
  apply(t)
  try {
    localStorage.setItem(STORAGE_KEY, t)
  } catch {
    // a theme that cannot be remembered is still a theme that works
  }
  // The stamp has to land before listeners read computed values back out.
  for (const fn of listeners) fn(t)
}

export function toggleTheme() {
  setTheme(current === 'dark' ? 'light' : 'dark')
}

export function subscribe(fn: (t: Theme) => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/**
 * The resolved value of a design token, for the canvases that cannot use var().
 * Returns `fallback` when the token is missing, so a typo degrades to a visible
 * colour instead of an invisible node.
 */
export function cssVar(name: string, fallback = '#888888'): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}
