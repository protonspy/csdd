import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { useLive } from './useLive'
import type { Artifact, Overview } from './types'
import { Sidebar } from './components/Sidebar'
import { SpecsPage } from './components/SpecsPage'
import { SpecView } from './components/SpecView'
import { FileViewer } from './components/FileViewer'
import { TestsView } from './components/TestsView'
import { ResourceView } from './components/ResourceView'
import { AuthScreen } from './components/AuthScreen'
import { resourceKindsHint } from './resources'
import type { ResourceKind } from './resources'

// View is the top-level workspace area chosen from the header tabs. Specs and
// Tests are full-width pages; Resources and Files use the contextual sidebar.
export type View = 'specs' | 'resources' | 'files' | 'tests'
const VIEWS: { id: View; label: string }[] = [
  { id: 'specs', label: 'Specs' },
  { id: 'resources', label: 'Resources' },
  { id: 'files', label: 'Files' },
  { id: 'tests', label: 'Tests' },
]

export type Selection =
  | { kind: 'spec'; feature: string }
  | { kind: 'resource'; resource: ResourceKind; artifact: Artifact }
  | { kind: 'file'; path: string }
  | null

export function App() {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [view, setView] = useState<View>('specs')
  const [selection, setSelection] = useState<Selection>(null)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [needsAuth, setNeedsAuth] = useState(window.location.pathname === '/auth')
  const { version, connected } = useLive()

  // A 401 anywhere raises this; show the login screen.
  useEffect(() => {
    const onAuth = () => setNeedsAuth(true)
    window.addEventListener('csdd-auth-required', onAuth)
    return () => window.removeEventListener('csdd-auth-required', onAuth)
  }, [])

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

  // Switching the top-level view clears the selection so each area opens on its
  // own landing screen (the Specs list, an empty prompt for Resources/Files).
  const changeView = (v: View) => {
    setView(v)
    setSelection(null)
    setSidebarOpen(false)
  }

  const handleSelect = (s: Selection) => {
    setSelection(s)
    setSidebarOpen(false)
  }

  if (needsAuth) {
    return <AuthScreen />
  }

  const hasSidebar = view === 'resources' || view === 'files'

  return (
    <div className="app">
      <header className="topbar">
        {hasSidebar && (
          <button
            className="menu-btn"
            onClick={() => setSidebarOpen((o) => !o)}
            aria-label="Toggle sidebar"
            aria-expanded={sidebarOpen}
          >
            ☰
          </button>
        )}
        <div className="brand">
          <span className="logo">csdd</span>
          <span className="brand-sub">web</span>
        </div>
        <nav className="top-tabs">
          {VIEWS.map((v) => (
            <button
              key={v.id}
              className={`top-tab ${view === v.id ? 'active' : ''}`}
              onClick={() => changeView(v.id)}
            >
              {v.label}
            </button>
          ))}
        </nav>
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
        {hasSidebar && (
          <Sidebar
            overview={overview}
            view={view}
            selection={selection}
            onSelect={handleSelect}
            version={version}
            open={sidebarOpen}
          />
        )}
        {hasSidebar && sidebarOpen && <div className="sidebar-backdrop" onClick={() => setSidebarOpen(false)} />}
        <main className="content">
          {error && <div className="banner error">Could not reach the server: {error}</div>}

          {view === 'specs' &&
            (selection?.kind === 'spec' ? (
              <SpecView feature={selection.feature} version={version} onBack={() => setSelection(null)} />
            ) : (
              <SpecsPage
                specs={overview?.specs ?? null}
                onOpen={(feature) => setSelection({ kind: 'spec', feature })}
              />
            ))}

          {view === 'resources' &&
            (selection?.kind === 'resource' ? (
              <ResourceView resource={selection.resource} artifact={selection.artifact} version={version} />
            ) : (
              <EmptyPrompt title="Resources" hint={`Pick ${resourceKindsHint()} on the left.`} />
            ))}

          {view === 'files' &&
            (selection?.kind === 'file' ? (
              <FileViewer path={selection.path} version={version} />
            ) : (
              <EmptyPrompt title="Files" hint="Pick a file on the left to view it." />
            ))}

          {view === 'tests' && <TestsView version={version} />}
        </main>
      </div>
    </div>
  )
}

function EmptyPrompt({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="empty">
      <h2>{title}</h2>
      <p className="muted">{hint}</p>
    </div>
  )
}
