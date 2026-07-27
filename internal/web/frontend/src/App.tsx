import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { useLive } from './useLive'
import type { Artifact, Overview } from './types'
import { Rail } from './components/Rail'
import { OverviewPage } from './components/OverviewPage'
import { SpecsPage } from './components/SpecsPage'
import { SpecView } from './components/SpecView'
import { FileViewer } from './components/FileViewer'
import { TestsView } from './components/TestsView'
import { ResourceView } from './components/ResourceView'
import { PlansView } from './components/PlansView'
import { WikiView } from './components/WikiView'
import { AdrPage } from './components/AdrPage'
import { StackPage } from './components/StackPage'
import { GlossaryPage } from './components/GlossaryPage'
import { RefPreview, RefProvider } from './components/Ref'
import { AuthScreen } from './components/AuthScreen'
import { RESOURCES, resourceKindsHint, type ResourceKind } from './resources'
import { href, navigate, useRoute, type Area } from './router'
import { toggleTheme } from './theme'
import { useTheme } from './useTheme'

// The Graph tab pulls in vis-network, which is heavy enough to dominate the
// initial bundle. Split it out so it is only fetched when the tab is opened.
const GraphView = lazy(() => import('./components/GraphView').then((m) => ({ default: m.GraphView })))

// Five primary areas, each with a landing route. Two of them (Knowledge,
// Workspace) hold several pages, which is the point: the dashboard grows a rail
// entry per new surface instead of a top-level tab per new surface.
const AREAS: { id: Area; label: string; to: string }[] = [
  { id: 'overview', label: 'Overview', to: '#/overview' },
  { id: 'specs', label: 'Specs', to: '#/specs' },
  { id: 'plans', label: 'Plans', to: '#/plans' },
  { id: 'knowledge', label: 'Knowledge', to: '#/wiki' },
  { id: 'workspace', label: 'Workspace', to: '#/files' },
]

export function App() {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [railOpen, setRailOpen] = useState(false)
  const [needsAuth, setNeedsAuth] = useState(window.location.pathname === '/auth')
  const { version, connected } = useLive()
  const route = useRoute()
  const theme = useTheme()

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

  // Navigating closes the drawer the narrow layout opens over the content.
  useEffect(() => {
    setRailOpen(false)
  }, [route.page, route.id, route.sub])

  if (needsAuth) {
    return <AuthScreen />
  }

  const hasRail = route.area !== 'overview'

  return (
    <RefProvider version={version}>
    <div className="app">
      <header className="topbar">
        {hasRail && (
          <button
            className="menu-btn"
            onClick={() => setRailOpen((o) => !o)}
            aria-label="Toggle sidebar"
            aria-expanded={railOpen}
          >
            ☰
          </button>
        )}
        <a className="brand" href="#/overview">
          <span className="logo">csdd</span>
          <span className="brand-sub">web</span>
        </a>
        <nav className="top-tabs">
          {AREAS.map((a) => (
            <a key={a.id} className={`top-tab ${route.area === a.id ? 'active' : ''}`} href={a.to}>
              {a.label}
            </a>
          ))}
        </nav>
        <div className="topbar-spacer" />
        {overview && (
          <div className="root-path" title={overview.root}>
            {overview.root}
          </div>
        )}
        <div
          className={`live ${connected ? 'on' : 'off'}`}
          title={connected ? 'live updates connected' : 'reconnecting…'}
        >
          <span className="dot" />
          {connected ? 'live' : 'offline'}
        </div>
        <button
          className="icon-btn"
          onClick={toggleTheme}
          title={theme === 'dark' ? 'Switch to light' : 'Switch to dark'}
          aria-label="Toggle colour theme"
        >
          {theme === 'dark' ? '☾' : '☀'}
        </button>
      </header>

      <div className="layout">
        {hasRail && <Rail route={route} overview={overview} version={version} open={railOpen} />}
        {hasRail && railOpen && <div className="sidebar-backdrop" onClick={() => setRailOpen(false)} />}
        <main className="content">
          {error && <div className="banner error">Could not reach the server: {error}</div>}
          <Page route={route} overview={overview} version={version} />
        </main>
      </div>
      <RefPreview />
    </div>
    </RefProvider>
  )
}

function Page({
  route,
  overview,
  version,
}: {
  route: ReturnType<typeof useRoute>
  overview: Overview | null
  version: number
}) {
  switch (route.page) {
    case 'overview':
      return <OverviewPage overview={overview} version={version} />

    case 'specs':
      return route.id ? (
        <SpecView feature={route.id} version={version} onBack={() => navigate('#/specs')} />
      ) : (
        <SpecsPage specs={overview?.specs ?? null} onOpen={(feature) => navigate(href('specs', feature))} />
      )

    case 'plans':
      return <PlansView slug={route.id} version={version} />

    case 'wiki':
      return <WikiView slug={route.id} version={version} />

    case 'adr':
      return <AdrPage slug={route.id} version={version} />

    case 'stack':
      return <StackPage row={route.query.row ?? null} version={version} />

    case 'glossary':
      return <GlossaryPage term={route.query.term ?? null} version={version} />

    case 'graph':
      return (
        <Suspense fallback={<EmptyPrompt title="Graph" hint="Loading graph…" />}>
          <GraphView version={version} />
        </Suspense>
      )

    case 'tests':
      return <TestsView version={version} />

    case 'files': {
      const path = route.query.path
      return path ? (
        <FileViewer path={path} version={version} />
      ) : (
        <EmptyPrompt title="Files" hint="Pick a file on the left to view it." />
      )
    }

    case 'resources': {
      const found = findResource(overview, route.id, route.sub)
      return found ? (
        <ResourceView resource={found.kind} artifact={found.artifact} version={version} />
      ) : (
        <EmptyPrompt title="Resources" hint={`Pick ${resourceKindsHint()} on the left.`} />
      )
    }
  }
}

/** Resolve `#/resources/<kind>/<name>` against the loaded overview. */
function findResource(
  overview: Overview | null,
  kind: string | null,
  name: string | null,
): { kind: ResourceKind; artifact: Artifact } | null {
  if (!overview || !kind || !name) return null
  const def = RESOURCES.find((r) => r.kind === kind)
  if (!def) return null
  const artifact = def.items(overview).find((a) => a.name === name || a.name.replace(/\.md$/, '') === name)
  return artifact ? { kind: def.kind, artifact } : null
}

function EmptyPrompt({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="empty">
      <h2>{title}</h2>
      <p className="muted">{hint}</p>
    </div>
  )
}
