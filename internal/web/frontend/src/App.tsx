import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { useLive } from './useLive'
import type { Overview } from './types'
import { Sidebar } from './components/Sidebar'
import { SpecView } from './components/SpecView'
import { FileViewer } from './components/FileViewer'
import { TestsView } from './components/TestsView'

export type Selection =
  | { kind: 'spec'; feature: string }
  | { kind: 'file'; path: string }
  | { kind: 'tests' }
  | null

export function App() {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selection, setSelection] = useState<Selection>(null)
  const [sidebarOpen, setSidebarOpen] = useState(false)
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
    const specs = overview?.specs ?? []
    if (!selection && specs.length > 0) {
      setSelection({ kind: 'spec', feature: specs[0].feature })
    }
  }, [overview, selection])

  // Picking something closes the mobile drawer.
  const handleSelect = (s: Selection) => {
    setSelection(s)
    setSidebarOpen(false)
  }

  return (
    <div className="app">
      <header className="topbar">
        <button
          className="menu-btn"
          onClick={() => setSidebarOpen((o) => !o)}
          aria-label="Toggle sidebar"
          aria-expanded={sidebarOpen}
        >
          ☰
        </button>
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
        <Sidebar
          overview={overview}
          selection={selection}
          onSelect={handleSelect}
          version={version}
          open={sidebarOpen}
        />
        {sidebarOpen && <div className="sidebar-backdrop" onClick={() => setSidebarOpen(false)} />}
        <main className="content">
          {error && <div className="banner error">Could not reach the server: {error}</div>}
          {selection?.kind === 'spec' && <SpecView feature={selection.feature} version={version} />}
          {selection?.kind === 'file' && <FileViewer path={selection.path} version={version} />}
          {selection?.kind === 'tests' && <TestsView version={version} />}
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
  if ((overview.specs?.length ?? 0) === 0) {
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
