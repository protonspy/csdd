import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DataSet } from 'vis-data'
import { Network } from 'vis-network'
import type { Edge, Node, Options } from 'vis-network'
import { api } from '../api'
import { cssVar } from '../theme'
import { useTheme } from '../useTheme'

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

// Node colours live in tokens.css as --g-<kind>, stepped per theme, and are read
// back here as resolved values: vis paints on a canvas, which is outside the
// cascade and cannot use var().
//
// Twenty kinds is far past what hue can separate, so this is a *recognition*
// palette rather than a categorical one — the label under each node and the
// legend carry identity, and the colour is a memory aid.
type Palette = Record<string, string>

const NODE_KINDS = [
  'spec', 'requirement', 'criterion', 'design', 'interface', 'flow', 'task',
  'steering', 'skill', 'agent', 'mcp', 'code_ref', 'wiki_page', 'raw_source',
  'tech', 'code', 'plan', 'feat', 'adr', 'term',
]

function readPalette(): Palette {
  const p: Palette = { fallback: cssVar('--g-fallback') }
  for (const kind of NODE_KINDS) p[kind] = cssVar(`--g-${kind.replace(/_/g, '-')}`, p.fallback)
  return p
}

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
// One element per node, cached, so re-rendering never rebuilds the DOM.
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

// The two DataSet item shapes. vis types `id` as optional; ours never is.
type VisNodeItem = Node & { id: string }
type VisEdgeItem = Edge & { id: string; from: string; to: string }

// The mutable half of a node item — label (it carries the +N hidden count),
// selection ring, and the dashed border that means "more behind me". Comparing
// this is what keeps a re-render from rewriting nodes that did not change.
function signatureOf(n: VisNodeItem): string {
  const dashes = (n.shapeProperties as { borderDashes?: unknown } | undefined)?.borderDashes
  const border = (n.color as { border?: string } | undefined)?.border
  const bg = (n.color as { background?: string } | undefined)?.background
  return `${n.label}|${n.borderWidth}|${JSON.stringify(dashes)}|${border ?? ''}|${bg ?? ''}`
}

// Deterministic 0..1 from an id (FNV-1a), so replaying the same expansion puts
// a node in the same place — a reset must not reshuffle the picture.
function hash01(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return ((h >>> 0) % 10000) / 10000
}

// Place a node about to enter the canvas beside whatever pulled it in. Without
// this vis drops it at a random point in a range that grows with node count, and
// the springs that yank it back are the energy that keeps the layout swirling.
function born(net: Network, origins: Map<string, string>, item: VisNodeItem): VisNodeItem {
  const from = origins.get(item.id)
  origins.delete(item.id)
  if (!from) return item
  let at: { x: number; y: number } | undefined
  try {
    at = net.getPositions([from])[from]
  } catch {
    return item // the parent is gone from the network — let vis place it
  }
  if (!at) return item
  const angle = hash01(item.id) * Math.PI * 2
  const radius = SPAWN_MIN_R + hash01(`${item.id}r`) * (SPAWN_MAX_R - SPAWN_MIN_R)
  return { ...item, x: Math.round(at.x + Math.cos(angle) * radius), y: Math.round(at.y + Math.sin(angle) * radius) }
}

// How long the layout is allowed to move after a change before it is frozen.
// This is a ceiling, not a schedule: a settled layout stops on its own (the
// `stabilized` event) long before this fires. It exists because a force layout
// is not guaranteed to converge — with `avoidOverlap` on and a few hundred
// nodes, one node jittering above `minVelocity` is enough to keep the whole
// simulation running, and the picture drifts and rotates forever.
const SETTLE_CEILING_MS = 4000

// Where a newly expanded node is born, relative to the node that pulled it in.
const SPAWN_MIN_R = 70
const SPAWN_MAX_R = 150

/** Full options for construction: the static half plus the themed colours. */
function mergedOptions(): Options {
  const t = themedOptions()
  return {
    ...OPTIONS,
    nodes: { ...OPTIONS.nodes, font: { ...(OPTIONS.nodes?.font as object), ...(t.nodes?.font as object) } },
    edges: { ...OPTIONS.edges, ...t.edges },
  }
}

/** The parts of the options that carry colour, resolved from the active theme. */
function themedOptions(): Options {
  return {
    nodes: { font: { color: cssVar('--ink', '#e6ecf5') } },
    edges: {
      color: {
        color: cssVar('--g-edge', '#394456'),
        highlight: cssVar('--accent', '#f0a23b'),
        hover: cssVar('--accent-hi', '#ffce8a'),
        opacity: 0.6,
      },
    },
  }
}

const OPTIONS: Options = {
  autoResize: false, // we drive resize from a ResizeObserver on the stage
  height: '100%',
  width: '100%',
  nodes: {
    shape: 'dot',
    borderWidth: 1,
    scaling: { min: 7, max: 32, label: { enabled: true, min: 11, max: 17, drawThreshold: 5 } },
    font: { size: 12, face: 'system-ui', strokeWidth: 0 },
  },
  edges: {
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
      // Low central gravity lets a repulsion-dominated cloud expand and orbit
      // instead of settling. It has to be strong enough to bound the layout.
      centralGravity: 0.02,
      springLength: 110,
      springConstant: 0.09,
      damping: 0.5,
      avoidOverlap: 0.4,
    },
    // vis stops the simulation once every node drops below minVelocity, so the
    // tab does not burn a core in the background after the layout settles.
    // That condition is not guaranteed to be reached — see SETTLE_CEILING_MS.
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
  // Everything painted on the canvas is resolved from tokens, so a theme change
  // has to re-read them: the palette feeds the node items, and the chrome
  // (edges, label ink) is pushed straight at the network.
  const theme = useTheme()
  const palette = useMemo(() => readPalette(), [theme])
  const accent = useMemo(() => cssVar('--accent', '#f0a23b'), [theme])
  const accentHi = useMemo(() => cssVar('--accent-hi', '#ffce8a'), [theme])

  const [graph, setGraph] = useState<NodeLink | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [seedSize, setSeedSize] = useState(SEED_DEFAULT)
  const [expandK, setExpandK] = useState(EXPAND_DEFAULT)
  const [visible, setVisible] = useState<Set<string>>(new Set())
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [notice, setNotice] = useState<string | null>(null)

  const netRef = useRef<Network | null>(null)
  const nodesRef = useRef<DataSet<VisNodeItem> | null>(null)
  const edgesRef = useRef<DataSet<VisEdgeItem> | null>(null)
  const stageRef = useRef<HTMLDivElement | null>(null)
  const fitPendingRef = useRef(true)
  // id → the already-visible node that pulled it in. Consumed once, when the
  // node is born, to place it beside its parent instead of at a random point.
  const originRef = useRef(new Map<string, string>())
  // id → signature of the item last pushed, so a re-render updates only the
  // nodes that actually changed rather than rewriting the whole DataSet.
  const sigRef = useRef(new Map<string, string>())
  const settleRef = useRef<number | undefined>(undefined)
  // Freezing has to be idempotent: disabling physics calls vis's own
  // stopSimulation, which re-emits `stabilized` — straight back into the handler
  // that froze it. Without this the two would trade timers forever.
  const frozenRef = useRef(false)

  // The layout moves while it has somewhere to go, then it is frozen.
  //
  // Freezing is what makes an expansion terminate. A force layout is not
  // guaranteed to converge: vis stops only when EVERY node is below
  // `minVelocity`, and with `avoidOverlap` on a few hundred nodes there is
  // almost always one that is not — so the simulation keeps running and the
  // whole picture slowly drifts and rotates. The `stabilized` event handles the
  // happy path; the timer is the ceiling for when it never arrives.
  const freeze = useCallback(() => {
    if (settleRef.current !== undefined) {
      window.clearTimeout(settleRef.current)
      settleRef.current = undefined
    }
    if (frozenRef.current) return
    frozenRef.current = true
    netRef.current?.setOptions({ physics: { enabled: false } })
  }, [])

  const reheat = useCallback(() => {
    const net = netRef.current
    if (!net) return
    frozenRef.current = false
    net.setOptions({ physics: { enabled: true } })
    net.startSimulation()
    if (settleRef.current !== undefined) window.clearTimeout(settleRef.current)
    settleRef.current = window.setTimeout(freeze, SETTLE_CEILING_MS)
  }, [freeze])

  // Network event handlers are bound once, so they read live state through this
  // rather than closing over a stale render.
  const live = useRef<{ expand: (id: string, limit: number) => void; expandK: number; freeze: () => void }>({
    expand: () => {},
    expandK: EXPAND_DEFAULT,
    freeze: () => {},
  })

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

  // One network for the life of the view. It is created once the graph has
  // loaded (before that the stage is not mounted) and destroyed on the way out —
  // vis holds a canvas, a render loop and DOM listeners that nothing else frees.
  // Mirrors exactly when the stage is rendered below: a load error replaces the
  // whole view, so the network must be torn down rather than left holding a
  // container that is no longer in the document.
  const ready = !!index && !error
  useEffect(() => {
    const el = stageRef.current
    if (!ready || !el) return

    const nodes = new DataSet<VisNodeItem>()
    const edges = new DataSet<VisEdgeItem>()
    const net = new Network(el, { nodes, edges }, mergedOptions())
    netRef.current = net
    nodesRef.current = nodes
    edgesRef.current = edges

    net.on('selectNode', (p: { nodes: (string | number)[] }) => {
      const id = p.nodes[0]
      if (id === undefined) return
      setSelectedId(String(id))
      live.current.expand(String(id), live.current.expandK)
    })
    net.on('deselectNode', () => setSelectedId(null))
    net.on('doubleClick', (p: { nodes: (string | number)[] }) => {
      const id = p.nodes[0]
      if (id !== undefined) live.current.expand(String(id), EXPAND_ALL_CAP)
    })
    // `stabilized` is emitted from inside a physics tick; changing options there
    // re-enters the solver, so hop out of the tick first.
    net.on('stabilized', () => {
      window.setTimeout(() => {
        live.current.freeze()
        if (!fitPendingRef.current) return
        fitPendingRef.current = false
        netRef.current?.fit({ animation: { duration: 400, easingFunction: 'easeInOutQuad' } })
      }, 0)
    })

    return () => {
      if (settleRef.current !== undefined) window.clearTimeout(settleRef.current)
      settleRef.current = undefined
      frozenRef.current = false
      net.destroy()
      netRef.current = null
      nodesRef.current = null
      edgesRef.current = null
      sigRef.current.clear()
      originRef.current.clear()
    }
  }, [ready])

  // autoResize is off (it polls); the canvas follows its container from here.
  useEffect(() => {
    const el = stageRef.current
    if (!ready || !el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      netRef.current?.setSize('100%', '100%')
      netRef.current?.redraw()
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [ready])

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
      // Remember what pulled each one in. A node born at a random point across
      // the canvas is what injects the energy that keeps the layout swirling;
      // born beside its parent, the expansion settles in well under a second.
      for (const x of take) originRef.current.set(x, id)
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

  live.current = { expand, expandK, freeze }

  // Rebuilt whenever the graph reloads; entries are reused across renders so a
  // node's tooltip element is built once (see tooltip()).
  const tips = useMemo(() => new Map<string, HTMLElement>(), [index])

  const visGraph = useMemo<{ nodes: VisNodeItem[]; edges: VisEdgeItem[] }>(() => {
    if (!index) return { nodes: [], edges: [] }
    const nodes = [...visible].map((id) => {
      const n = index.byId.get(id)!
      const s = index.score.get(id) ?? 1
      const hidden = hiddenCount(id, visible)
      const c = palette[n.file_type] ?? palette.fallback
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
          border: id === selectedId ? accent : c,
          highlight: { background: c, border: accent },
          hover: { background: c, border: accentHi },
        },
      }
    })
    const edges = index.links
      .filter((l) => visible.has(l.source) && visible.has(l.target))
      .map((l) => ({ id: l.key, from: l.source, to: l.target, title: l.relation }))
    return { nodes, edges }
  }, [index, visible, selectedId, hiddenCount, tips, palette, accent, accentHi])

  // Canvas chrome follows the theme. Node fills ride along through visGraph:
  // their signature includes the background, so every node is updated once.
  useEffect(() => {
    netRef.current?.setOptions(themedOptions())
  }, [theme])

  // Reconcile the computed view into vis's DataSets.
  //
  // Order matters, because vis resolves edges to node objects as the data
  // changes: an edge whose endpoint is leaving must go before the node, and a
  // node must arrive before the edges that reference it. Either inversion leaves
  // vis holding a reference to something that is not there.
  //
  // Only what actually changed is written. Rewriting the whole DataSet on every
  // render would restart the solver on every click.
  useEffect(() => {
    const net = netRef.current
    const nodesDs = nodesRef.current
    const edgesDs = edgesRef.current
    if (!net || !nodesDs || !edgesDs) return

    const sigs = sigRef.current
    const wantNodeIds = new Set(visGraph.nodes.map((n) => n.id))
    const haveNodeIds = new Set(nodesDs.getIds().map(String))
    const wantEdgeIds = new Set(visGraph.edges.map((e) => e.id))
    const haveEdgeIds = new Set(edgesDs.getIds().map(String))

    const edgesGone = [...haveEdgeIds].filter((id) => !wantEdgeIds.has(id))
    const nodesGone = [...haveNodeIds].filter((id) => !wantNodeIds.has(id))
    const nodesNew = visGraph.nodes.filter((n) => !haveNodeIds.has(n.id))
    const edgesNew = visGraph.edges.filter((e) => !haveEdgeIds.has(e.id))
    const nodesChanged = visGraph.nodes.filter((n) => haveNodeIds.has(n.id) && sigs.get(n.id) !== signatureOf(n))

    if (edgesGone.length) edgesDs.remove(edgesGone)
    if (nodesGone.length) {
      nodesDs.remove(nodesGone)
      for (const id of nodesGone) sigs.delete(id)
    }
    if (nodesNew.length) nodesDs.add(nodesNew.map((n) => born(net, originRef.current, n)))
    if (edgesNew.length) edgesDs.add(edgesNew)
    // Carries no x/y, so an update never yanks a node back to where it started.
    if (nodesChanged.length) nodesDs.update(nodesChanged)

    for (const n of visGraph.nodes) sigs.set(n.id, signatureOf(n))

    // Only a change in shape needs the solver. A selection ring does not, and
    // with physics already frozen vis will not restart it on its own.
    if (nodesNew.length || nodesGone.length || edgesNew.length || edgesGone.length) reheat()
    // `ready` is a dependency because a rebuilt network starts with empty
    // DataSets: without it a recovered load error would leave a blank canvas.
  }, [visGraph, reheat, ready])

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
                      <span className="graph-dot" style={{ background: palette[m.file_type] ?? palette.fallback }} />
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
        <div className="graph-stage">
          {/* vis empties whatever container it is handed, so it gets one of its
              own: the notice and the legend are siblings, not children. The
              container is absolutely filled rather than height:100% — a
              percentage height inside a flex item with no definite height
              resolves to auto, which is a zero-height canvas. */}
          <div className="graph-net" ref={stageRef} />

          {notice && <div className="graph-notice">{notice}</div>}

          <div className="graph-legend">
            {legend.map(([t, n]) => (
              <span key={t}>
                <span className="graph-dot" style={{ background: palette[t] ?? palette.fallback }} />
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
