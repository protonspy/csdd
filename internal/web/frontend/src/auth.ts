const KEY = 'csdd_token'

export function getToken(): string {
  try {
    return localStorage.getItem(KEY) ?? ''
  } catch {
    return ''
  }
}

export function setToken(t: string) {
  try {
    localStorage.setItem(KEY, t)
  } catch {
    /* ignore */
  }
  // Mirror to a cookie so the server can authenticate the SSE EventSource and
  // any request the client makes before the header is attached.
  const secure = window.location.protocol === 'https:' ? '; Secure' : ''
  document.cookie = `csdd_token=${encodeURIComponent(t)}; path=/; SameSite=Lax; max-age=2592000${secure}`
}

export function clearToken() {
  try {
    localStorage.removeItem(KEY)
  } catch {
    /* ignore */
  }
  document.cookie = 'csdd_token=; path=/; max-age=0'
}

export function authHeader(): Record<string, string> {
  const t = getToken()
  return t ? { Authorization: `Bearer ${t}` } : {}
}

// requireAuth signals the app that a 401 happened and the login screen is needed.
export function requireAuth() {
  window.dispatchEvent(new Event('csdd-auth-required'))
}

// consumeTokenLink handles a /token/<token> magic link: it stores the token and
// rewrites the URL to /. Returns true when it handled one.
export function consumeTokenLink(): boolean {
  const m = window.location.pathname.match(/^\/token\/(.+)$/)
  if (!m) return false
  setToken(decodeURIComponent(m[1]))
  window.history.replaceState(null, '', '/')
  return true
}
