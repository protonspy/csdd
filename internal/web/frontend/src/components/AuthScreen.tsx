import { useState } from 'react'
import type { FormEvent } from 'react'
import { setToken } from '../auth'

// AuthScreen is the /auth login: it asks for the access code printed in the
// terminal, verifies it against the API, stores it, then reloads authenticated.
export function AuthScreen() {
  const [code, setCode] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const token = code.trim()
    if (!token) return
    setBusy(true)
    setErr('')
    try {
      const res = await fetch('/api/overview', { headers: { Authorization: `Bearer ${token}` } })
      if (res.status === 401) {
        setErr('Invalid access code')
        setBusy(false)
        return
      }
      if (!res.ok) {
        setErr(`Error ${res.status}`)
        setBusy(false)
        return
      }
      setToken(token)
      window.location.assign('/')
    } catch {
      setErr('Connection error')
      setBusy(false)
    }
  }

  return (
    <div className="auth-screen">
      <form className="auth-card" onSubmit={submit}>
        <div className="auth-brand">
          <span className="logo">csdd</span>
          <span className="brand-sub">web</span>
        </div>
        <h2>Authentication required</h2>
        <p className="muted small">
          Enter the access code printed in the terminal where you started <code>csdd web</code>.
        </p>
        <input
          className="auth-input"
          type="password"
          placeholder="access code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          autoFocus
        />
        {err && <div className="auth-err">{err}</div>}
        <button className="auth-btn" type="submit" disabled={busy}>
          {busy ? 'Checking…' : 'Unlock'}
        </button>
      </form>
    </div>
  )
}
