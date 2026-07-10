import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Graph, { type VisEventParams, type VisNetwork } from 'react-graph-vis'
import { api } from '../api'

// GraphView renders docs/graph/graph.json.gz — the csdd knowledge graph — served
// gzip-compressed through the read-only /api/graph route (host guard + auth). The
// browser decompresses the blob transparently, so this component consumes plain
// node-link JSON. It never writes; the CLI is the only author of the graph (R6.3).
//
// The graph is far too large to draw at once (a mid-size repo yields ~2k nodes
// / ~2k edges, dominated by `code` nodes and `contains` edges). Drawing it whole
// is unreadable and pins a core in physics. So the view is progressive: it seeds
// with the highest-scoring nodes and grows one hop at a time, always pulling the
// most important neighbours first.

interface GNode {
  id: string
  label: string
  file_type: string
  source_file?: string
  source_location?: string
  description?: string
}
interface GLink {
  source: string
  target: string
  relation: string
  confidence_score?: number
}
interface NodeLink {
  nodes: GNode[]
  links: GLink[]
}

const PALETTE: Record<string, string> = {
  spec: '#6C8EBF', requirement: '#B85450', criterion: '#D79B00', design: '#9673A6',
  interface: '#82B366', flow: '#3A9CA6', task: '#D6B656', steering: '#647687',
  skill: '#4C8C5A', agent: '#B46EAB', mcp: '#8C6D46', code_ref: '#999999',
  wiki_page: '#5A7DBE', raw_source: '#B0B0B0', tech: '#C97B2C', code: '#4E79A7',
  plan: '#7E57C2', feat: '#26A69A', adr: '#A1887F', term: '#EC407A',
}
const colorFor = (t: string) => PALETTE[t] ?? '#888'

// Importance = type weight × √(1 + weighted degree). Raw degree alone ranks a
// source file that `contains` 200 symbols above every spec, so structural edges
// are discounted and semantic node types are boosted. The √ compresses hub
// degree so a handful of strong semantic edges can outrank a big container.
const TYPE_WEIGHT: Record<string, number> = {
  spec: 3, design: 3, plan: 2.6, adr: 2.6, requirement: 2.4, flow: 2, interface: 2,
  feat: 2, agent: 1.8, skill: 1.8, steering: 1.8, mcp: 1.8, wiki_page: 1.6,
  criterion: 1.4, task: 1.4, tech: 1.2, term: 1.1, code: 1, code_ref: 0.7, raw_source: 0.4,
}
const REL_WEIGHT: Record<string, number> = {
  contains: 0.2, imports: 0.6, references: 0.8, links_to: 0.8, cites: 0.8, related_to: 0.8,
}

const SEED_DEFAULT = 24
const EXPAND_DEFAULT = 8
// Hard ceilings. Past a few hundred nodes vis-network physics degrades and the
// picture stops being readable — the point of this view is to stay legible.
const EXPAND_ALL_CAP = 120
const MAX_VISIBLE = 400

interface Index {
  byId: Map<string, GNode>
  links: (GLink & { key: string })[]
  linksOf: Map<string, (GLink & { key: string })[]>
  neighbors: Map<string, string[]> // sorted by score, most important first
  score: Map<string, number>
  ranked: string[] // every id, most important first
}

function buildIndex(graph: NodeLink): Index {
  const byId = new Map(graph.nodes.map((n) => [n.id, n]))
  const links = graph.links
    .filter((l) => byId.has(l.source) && byId.has(l.target))
    .map((l, i) => ({ ...l, key: `e${i}` }))

  const wdeg = new Map<string, number>()
  const nbr = new Map<string, Set<string>>()
  const linksOf = new Map<string, (GLink & { key: string })[]>()
  const touch = (id: string, other: string, l: GLink & { key: string }, w: number) => {
    wdeg.set(id, (wdeg.get(id) ?? 0) + w)
    let s = nbr.get(id)
    if (!s) nbr.set(id, (s = new Set()))
    s.add(other)
    let ls = linksOf.get(id)
    if (!ls) linksOf.set(id, (ls = []))
    ls.push(l)
  }
  for (const l of links) {
    const w = (REL_WEIGHT[l.relation] ?? 1) * (l.confidence_score ?? 1)
    touch(l.source, l.target, l, w)
    touch(l.target, l.source, l, w)
  }

  const score = new Map<string, number>()
  for (const n of graph.nodes) {
    score.set(n.id, (TYPE_WEIGHT[n.file_type] ?? 1) * Math.sqrt(1 + (wdeg.get(n.id) ?? 0)))
  }
  const desc = (a: string, b: string) => (score.get(b) ?? 0) - (score.get(a) ?? 0)

  const neighbors = new Map<string, string[]>()
  for (const [id, set] of nbr) neighbors.set(id, [...set].sort(desc))

  return { byId, links, linksOf, neighbors, score, ranked: [...byId.keys()].sort(desc) }
}

// The seed is the important *core*, not the top-n scoring nodes: those are
// mostly leaf docs (skills, agents, steering) that link to code but never to
// each other, so ranking alone yields a disconnected dot cloud. Start from a
// few top-ranked roots and grow best-first along edges, so what lands on screen
// is both important and actually connected. Any shortfall (isolated roots) is
// filled from the global ranking.
function seedNodes(index: Index, n: number): Set<string> {
  const roots = index.ranked.slice(0, Math.max(3, Math.ceil(n / 6)))
  const chosen = new Set(roots)
  const frontier = new Set<string>()
  const push = (id: string) => {
    for (const x of index.neighbors.get(id) ?? []) if (!chosen.has(x)) frontier.add(x)
  }
  roots.forEach(push)

  while (chosen.size < n && frontier.size > 0) {
    let best = ''
    let bestScore = -Infinity
    for (const c of frontier) {
      const s = index.score.get(c) ?? 0
      if (s > bestScore) {
        bestScore = s
        best = c
      }
    }
    frontier.delete(best)
    chosen.add(best)
    push(best)
  }
  for (const id of index.ranked) {
    if (chosen.size >= n) break
    chosen.add(id)
  }
  return chosen
}

// vis-network assigns a string `title` into the popup with innerHTML. Graph
// labels and descriptions are repo-authored text, so build the tooltip as a
// detached element with textContent instead of handing vis markup to parse.
//
// The element is cached per node: react-graph-vis diffs node items with lodash
// `isEqual`, which never reports two distinct DOM elements as equal, so a fresh
// element per render would mark every visible node as changed on every render.
function tooltip(n: GNode, score: number): HTMLElement {
  const el = document.createElement('div')
  el.className = 'graph-tip'
  const lines = [
    n.label,
    `${n.file_type} · importance ${score.toFixed(1)}`,
    n.source_file ? `${n.source_file}${n.source_location ? ` ${n.source_location}` : ''}` : '',
    n.description ?? '',
  ].filter(Boolean)
  for (const line of lines) {
    const div = document.createElement('div')
    div.textContent = line
    el.appendChild(div)
  }
  return el
}

// Everything vis-network needs that react-graph-vis does not already hardcode.
// NOTE: react-graph-vis merges with lodash `defaultsDeep(itsDefaults, props.options)`,
// so its own defaults WIN over anything set here — `edges.color: '#000000'`
// (black edges on a dark canvas), `autoResize: false` and `physics.stabilization`
// are unreachable through the prop. They are re-applied via `setOptions` on the
// network handle below, which does override. Keep both in sync.
const OPTIONS: Record<string, unknown> = {
  autoResize: false, // ignored by the prop; we drive resize with a ResizeObserver
  height: '100%',
  width: '100%',
  nodes: {
    shape: 'dot',
    borderWidth: 1,
    scaling: { min: 7, max: 32, label: { enabled: true, min: 11, max: 17, drawThreshold: 5 } },
    font: { color: '#d7dee8', size: 12, face: 'system-ui', strokeWidth: 0 },
  },
  edges: {
    color: { color: '#394456', highlight: '#f0a23b', hover: '#ffce8a', opacity: 0.6 },
    width: 1,
    smooth: false,
    arrows: { to: { enabled: true, scaleFactor: 0.4 } },
  },
  // improvedLayout runs a Kamada-Kawai pre-pass that is O(n²)-ish; it stalls
  // visibly on every expansion. Live physics settles fast enough on its own.
  layout: { improvedLayout: false, randomSeed: 42 },
  physics: {
    solver: 'forceAtlas2Based',
    forceAtlas2Based: {
      gravitationalConstant: -60,
      centralGravity: 0.008,
      springLength: 110,
      springConstant: 0.09,
      damping: 0.5,
      avoidOverlap: 0.4,
    },
    // vis stops the simulation once every node drops below minVelocity, so the
    // tab does not burn a core in the background after the layout settles.
    maxVelocity: 40,
    minVelocity: 0.9,
    timestep: 0.5,
    adaptiveTimestep: true,
    stabilization: false,
  },
  interaction: {
    hover: true,
    tooltipDelay: 150,
    hideEdgesOnDrag: true,
    keyboard: false,
    navigationButtons: false,
    multiselect: false,
  },
}

export function GraphView({ version }: { version: number }) {
  const [graph, setGraph] = useState<NodeLink | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [seedSize, setSeedSize] = useState(SEED_DEFAULT)
  const [expandK, setExpandK] = useState(EXPAND_DEFAULT)
  const [visible, setVisible] = useState<Set<string>>(new Set())
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [notice, setNotice] = useState<string | null>(null)

  const netRef = useRef<VisNetwork | null>(null)
  const wrapRef = useRef<HTMLDivElement | null>(null)
  const fitPendingRef = useRef(true)

  useEffect(() => {
    let cancelled = false
    api
      .graph()
      .then((parsed) => {
        if (cancelled) return
        if (!parsed || !Array.isArray(parsed.nodes) || !Array.isArray(parsed.links)) {
          setError('graph.json.gz is not a node-link graph (missing nodes/links arrays). Re-run `csdd graph build`.')
          setGraph(null)
          return
        }
        setGraph(parsed as NodeLink)
        setError(null)
      })
      .catch(() => {
        if (!cancelled) setError('No graph yet. Run `csdd graph build` to generate docs/graph/graph.json.gz.')
      })
    return () => {
      cancelled = true
    }
  }, [version])

  const index = useMemo(() => (graph ? buildIndex(graph) : null), [graph])
  const seed = useMemo(() => (index ? seedNodes(index, seedSize) : new Set<string>()), [index, seedSize])

  // Reseeding is a reset: the expansion state was relative to the old seed.
  useEffect(() => {
    setVisible(new Set(seed))
    setSelectedId(null)
    setNotice(null)
    fitPendingRef.current = true
  }, [seed])

  useEffect(() => () => netRef.current?.destroy(), [])

  // react-graph-vis pins autoResize to false, so the canvas never follows its
  // container. Drive it from the wrapper instead.
  useEffect(() => {
    const wrap = wrapRef.current
    if (!wrap || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      netRef.current?.setSize('100%', '100%')
      netRef.current?.redraw()
    })
    ro.observe(wrap)
    return () => ro.disconnect()
  }, [])

  // Mirror `visible` so event handlers and the expand/collapse callbacks can read
  // the current set without re-subscribing. Never mutated, only reassigned.
  const visibleRef = useRef(visible)
  visibleRef.current = visible

  const hiddenCount = useCallback(
    (id: string, vis: Set<string>) => {
      const nbrs = index?.neighbors.get(id)
      if (!nbrs) return 0
      let n = 0
      for (const x of nbrs) if (!vis.has(x)) n++
      return n
    },
    [index],
  )

  const expand = useCallback(
    (id: string, limit: number) => {
      if (!index) return
      const prev = visibleRef.current
      const nbrs = index.neighbors.get(id)
      if (!nbrs) return
      const fresh = nbrs.filter((x) => !prev.has(x)) // already sorted by importance
      if (fresh.length === 0) return
      const room = MAX_VISIBLE - prev.size
      if (room <= 0) {
        setNotice(`Reached the ${MAX_VISIBLE}-node ceiling. Collapse or reset to explore further.`)
        return
      }
      const take = fresh.slice(0, Math.min(limit, room))
      setNotice(
        take.length < fresh.length
          ? `Added the ${take.length} most important of ${fresh.length} neighbours.`
          : null,
      )
      fitPendingRef.current = false
      setVisible(new Set([...prev, ...take]))
    },
    [index],
  )

  // Drop neighbours that this node alone is holding on screen. Seed nodes and
  // anything reachable from another visible node stay put.
  const collapse = useCallback(
    (id: string) => {
      if (!index) return
      const prev = visibleRef.current
      const next = new Set(prev)
      for (const n of index.neighbors.get(id) ?? []) {
        if (!prev.has(n) || seed.has(n) || n === id) continue
        const others = (index.neighbors.get(n) ?? []).filter((x) => x !== id && prev.has(x))
        if (others.length === 0) next.delete(n)
      }
      setNotice(null)
      setVisible(next)
    },
    [index, seed],
  )

  const focusNow = useCallback((id: string) => {
    netRef.current?.selectNodes([id])
    netRef.current?.focus(id, { scale: 1.2, animation: { duration: 400, easingFunction: 'easeInOutQuad' } })
  }, [])

  // Focus a node from the search box or the connections list, pulling it onto
  // the canvas first if it is not there yet.
  const pendingFocus = useRef<string | null>(null)
  const reveal = useCallback(
    (id: string) => {
      const prev = visibleRef.current
      setSelectedId(id)
      if (prev.has(id)) {
        focusNow(id)
        return
      }
      // vis throws RangeError when selecting an id that is not in its DataSet,
      // and the node only lands there once the re-render patches it. Defer.
      pendingFocus.current = id
      fitPendingRef.current = false
      setVisible(new Set([...prev, id]))
    },
    [focusNow],
  )

  useEffect(() => {
    const id = pendingFocus.current
    if (!id || !visible.has(id)) return
    pendingFocus.current = null
    focusNow(id)
  }, [visible, focusNow])

  // Handlers are registered once by react-graph-vis (its shouldComponentUpdate
  // only re-binds when the `events` object identity changes), so read live state
  // through a ref rather than closing over it.
  const live = useRef({ expand, expandK })
  live.current = { expand, expandK }

  const events = useMemo(
    () => ({
      selectNode: ({ nodes }: VisEventParams) => {
        const id = nodes[0]
        if (!id) return
        setSelectedId(id)
        live.current.expand(id, live.current.expandK)
      },
      deselectNode: () => setSelectedId(null),
      doubleClick: ({ nodes }: VisEventParams) => {
        if (nodes[0]) live.current.expand(nodes[0], EXPAND_ALL_CAP)
      },
      stabilized: () => {
        if (!fitPendingRef.current) return
        fitPendingRef.current = false
        netRef.current?.fit({ animation: { duration: 400, easingFunction: 'easeInOutQuad' } })
      },
    }),
    [],
  )

  // Rebuilt whenever the graph reloads; entries are reused across renders so the
  // node diff stays stable (see tooltip()).
  const tips = useMemo(() => new Map<string, HTMLElement>(), [index])

  const visGraph = useMemo(() => {
    if (!index) return { nodes: [], edges: [] }
    const nodes = [...visible].map((id) => {
      const n = index.byId.get(id)!
      const s = index.score.get(id) ?? 1
      const hidden = hiddenCount(id, visible)
      const c = colorFor(n.file_type)
      const label = n.label.length > 26 ? n.label.slice(0, 25) + '…' : n.label
      let tip = tips.get(id)
      if (!tip) tips.set(id, (tip = tooltip(n, s)))
      return {
        id,
        label: hidden > 0 ? `${label}  +${hidden}` : label,
        title: tip,
        value: s,
        borderWidth: id === selectedId ? 3 : hidden > 0 ? 2 : 1,
        // A dashed ring means "there is more behind me" — click to pull it in.
        shapeProperties: { borderDashes: hidden > 0 ? [4, 3] : false },
        color: {
          background: c,
          border: id === selectedId ? '#f0a23b' : c,
          highlight: { background: c, border: '#f0a23b' },
          hover: { background: c, border: '#ffce8a' },
        },
      }
    })
    const edges = index.links
      .filter((l) => visible.has(l.source) && visible.has(l.target))
      .map((l) => ({ id: l.key, from: l.source, to: l.target, title: l.relation }))
    return { nodes, edges }
  }, [index, visible, selectedId, hiddenCount, tips])

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!index || q.length < 2) return []
    const out: GNode[] = []
    for (const id of index.ranked) {
      const n = index.byId.get(id)!
      if (n.label.toLowerCase().includes(q) || id.toLowerCase().includes(q)) out.push(n)
      if (out.length >= 12) break
    }
    return out
  }, [index, query])

  const selected = selectedId && index ? (index.byId.get(selectedId) ?? null) : null
  const selectedHidden = selectedId ? hiddenCount(selectedId, visible) : 0

  const connections = useMemo(() => {
    if (!index || !selectedId) return []
    return (index.linksOf.get(selectedId) ?? []).map((l) => {
      const out = l.source === selectedId
      const other = out ? l.target : l.source
      return { key: l.key, out, relation: l.relation, other, label: index.byId.get(other)?.label ?? other }
    })
  }, [index, selectedId])

  const legend = useMemo(() => {
    if (!index) return []
    const seen = new Map<string, number>()
    for (const id of visible) {
      const t = index.byId.get(id)!.file_type
      seen.set(t, (seen.get(t) ?? 0) + 1)
    }
    return [...seen.entries()].sort((a, b) => b[1] - a[1])
  }, [index, visible])

  if (error) {
    return (
      <div className="empty">
        <h2>Graph</h2>
        <p className="muted">{error}</p>
      </div>
    )
  }

  if (!index) {
    return (
      <div className="empty">
        <h2>Graph</h2>
        <p className="muted">Loading graph…</p>
      </div>
    )
  }

  return (
    <div className="graph-view">
      <div className="graph-canvas">
        <div className="graph-toolbar">
          <div className="graph-search">
            <input
              placeholder={`search ${index.ranked.length} nodes…`}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && matches[0]) {
                  reveal(matches[0].id)
                  setQuery('')
                }
                if (e.key === 'Escape') setQuery('')
              }}
            />
            {matches.length > 0 && (
              <ul className="graph-matches">
                {matches.map((m) => (
                  <li key={m.id}>
                    <button
                      onClick={() => {
                        reveal(m.id)
                        setQuery('')
                      }}
                    >
                      <span className="graph-dot" style={{ background: colorFor(m.file_type) }} />
                      {m.label}
                      <span className="muted"> · {m.file_type}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <label className="graph-ctl">
            top
            <input
              type="range"
              min={10}
              max={80}
              step={2}
              value={seedSize}
              onChange={(e) => setSeedSize(Number(e.target.value))}
            />
            <b>{seedSize}</b>
          </label>

          <label className="graph-ctl">
            expand
            <input
              type="range"
              min={3}
              max={24}
              value={expandK}
              onChange={(e) => setExpandK(Number(e.target.value))}
            />
            <b>{expandK}</b>
          </label>

          <button
            className="graph-btn"
            onClick={() => netRef.current?.fit({ animation: { duration: 400, easingFunction: 'easeInOutQuad' } })}
          >
            Fit
          </button>
          <button
            className="graph-btn"
            onClick={() => {
              setVisible(new Set(seed))
              setSelectedId(null)
              setNotice(null)
              netRef.current?.unselectAll()
              fitPendingRef.current = true
            }}
          >
            Reset
          </button>
        </div>

        {/* The stage is what vis sizes against, so the toolbar above must not
            overlay it — otherwise `fit()` centres nodes underneath the controls. */}
        <div className="graph-stage" ref={wrapRef}>
          {notice && <div className="graph-notice">{notice}</div>}

          <Graph
            graph={visGraph}
            options={OPTIONS}
            events={events}
            // Without it the component mints a uuid for the container id, dragging
            // in react-graph-vis's ancient uuid@2 dependency at runtime.
            identifier="csdd-graph"
            style={{ width: '100%', height: '100%' }}
            getNetwork={(net) => {
              netRef.current = net
              // Defeat react-graph-vis's defaultsDeep (see OPTIONS): setOptions is
              // the only path that actually overrides its hardcoded defaults.
              net.setOptions(OPTIONS)
            }}
          />

          <div className="graph-legend">
            {legend.map(([t, n]) => (
              <span key={t}>
                <span className="graph-dot" style={{ background: colorFor(t) }} />
                {t} <b>{n}</b>
              </span>
            ))}
          </div>
        </div>
      </div>

      <aside className="graph-panel">
        <h3>Graph</h3>
        <p className="muted">
          {index.ranked.length} nodes · {index.links.length} edges
          <br />
          showing {visible.size} · {visGraph.edges.length} edges
        </p>

        {selected ? (
          <div>
            <strong>{selected.label}</strong>
            <div className="muted" style={{ margin: '4px 0 8px' }}>
              {selected.file_type}
              {selected.source_file ? ` · ${selected.source_file}` : ''}
              {selected.source_location ? ` ${selected.source_location}` : ''}
            </div>
            {selected.description && <p className="muted">{selected.description}</p>}

            <div className="graph-actions">
              <button className="graph-btn" disabled={selectedHidden === 0} onClick={() => expand(selected.id, expandK)}>
                Expand +{Math.min(expandK, selectedHidden)}
              </button>
              <button
                className="graph-btn"
                disabled={selectedHidden === 0}
                onClick={() => expand(selected.id, EXPAND_ALL_CAP)}
              >
                Expand all ({selectedHidden})
              </button>
              <button className="graph-btn" onClick={() => collapse(selected.id)}>
                Collapse
              </button>
            </div>

            <h4>{connections.length} connections</h4>
            <ul className="graph-conns">
              {connections.slice(0, 60).map((c) => (
                <li key={c.key}>
                  <button onClick={() => reveal(c.other)} title={c.other}>
                    <span className={visible.has(c.other) ? 'graph-arrow on' : 'graph-arrow'}>
                      {c.out ? '→' : '←'}
                    </span>
                    <span className="muted">{c.relation}</span> {c.label}
                  </button>
                </li>
              ))}
              {connections.length > 60 && <li className="muted">…and {connections.length - 60} more</li>}
            </ul>
          </div>
        ) : (
          <>
            <p className="muted">
              Seeded with the most important connected core of {seedSize} nodes, ranked by node type and weighted
              degree.
            </p>
            <p className="muted">
              Click a node to pull in its {expandK} most important neighbours. A dashed ring means it still hides
              some. Double-click expands as far as the cap allows.
            </p>
          </>
        )}
      </aside>
    </div>
  )
}
