import { useEffect, useState } from 'react'
import { getToken } from './auth'

// useLive subscribes to the server's SSE change stream and returns a version
// number that increments whenever the workspace changes on disk. Components
// re-fetch their data when this value changes. EventSource auto-reconnects.
export function useLive(): { version: number; connected: boolean } {
  const [version, setVersion] = useState(0)
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    // EventSource can't set headers, so the token rides as a query param.
    const t = getToken()
    const es = new EventSource(t ? `/api/events?token=${encodeURIComponent(t)}` : '/api/events')
    es.addEventListener('open', () => setConnected(true))
    es.addEventListener('change', (e) => {
      setConnected(true)
      try {
        const data = JSON.parse((e as MessageEvent).data)
        setVersion(typeof data.version === 'number' ? data.version : (v) => v + 1)
      } catch {
        setVersion((v) => v + 1)
      }
    })
    es.onerror = () => setConnected(false)
    return () => es.close()
  }, [])

  return { version, connected }
}
