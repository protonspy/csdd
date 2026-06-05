import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { useLive } from './useLive'
import type { Overview } from './types'
import { Sidebar } from './components/Sidebar'
import { SpecView } from './components/SpecView'
import { FileViewer } from './components/FileViewer'

export type Selection =
  | { kind: 'spec'; feature: string }
  | { kind: 'file'; path: string }
  | null

export function App() {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selection, setSelection] = useState<Selection>(null)
  const { version, connected } = useLive()

  const refresh = useCallback(() => {
    api
      .overview()
      .then((ov) => {
        setOverview(ov)
        setError(null)
      })
      .catch((e) => setError(String(e)))
  }, [])

  // Re-fetch on mount and whenever the workspace changes (SSE version bump).
  useEffect(() => {
    refresh()
  }, [refresh, version])

  // Auto-select the first spec once the overview loads.
  useEffect(() => {
    if (!selection && overview && overview.specs.length > 0) {
      setSelection({ kind: 'spec', feature: overview.specs[0].feature })
    }
  }, [overview, selection])

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="logo">csdd</span>
          <span className="brand-sub">web</span>
        </div>
        <div className="topbar-spacer" />
        {overview && (
          <div className="root-path" title={overview.root}>
            {overview.root}
          </div>
        )}
        <div className={`live ${connected ? 'on' : 'off'}`} title={connected ? 'live updates connected' : 'reconnecting…'}>
          <span className="dot" />
          {connected ? 'live' : 'offline'}
        </div>
      </header>

      <div className="layout">
        <Sidebar overview={overview} selection={selection} onSelect={setSelection} version={version} />
        <main className="content">
          {error && <div className="banner error">Could not reach the server: {error}</div>}
          {selection?.kind === 'spec' && <SpecView feature={selection.feature} version={version} />}
          {selection?.kind === 'file' && <FileViewer path={selection.path} version={version} />}
          {!selection && <EmptyState overview={overview} />}
        </main>
      </div>
    </div>
  )
}

function EmptyState({ overview }: { overview: Overview | null }) {
  if (!overview) {
    return <div className="empty">Loading workspace…</div>
  }
  if (overview.specs.length === 0) {
    return (
      <div className="empty">
        <h2>No specs yet</h2>
        <p>
          Create one with <code>csdd spec init &lt;feature&gt;</code>, then generate its requirements.
          The dashboard updates live as you go.
        </p>
      </div>
    )
  }
  return <div className="empty">Select a spec or a file on the left.</div>
}
