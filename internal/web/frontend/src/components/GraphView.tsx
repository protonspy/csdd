import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'

// GraphView renders docs/graph/graph.json — the csdd knowledge graph — through
// the same read-only /api/file route as every other file (host guard + auth +
// secret redaction, PR #43). It never writes; the CLI is the only author of the
// graph (R6.3). Layout is a dependency-free canvas force simulation so the tab
// stays self-contained.

interface GNode {
  id: string
  label: string
  file_type: string
  source_file?: string
}
interface GLink {
  source: string
  target: string
  relation: string
}
interface NodeLink {
  nodes: GNode[]
  links: GLink[]
}

interface Sim extends GNode {
  x: number
  y: number
  vx: number
  vy: number
  deg: number
}

const PALETTE: Record<string, string> = {
  spec: '#6C8EBF', requirement: '#B85450', criterion: '#D79B00', design: '#9673A6',
  interface: '#82B366', flow: '#3A9CA6', task: '#D6B656', steering: '#647687',
  skill: '#4C8C5A', agent: '#B46EAB', mcp: '#8C6D46', code_ref: '#999999',
  wiki_page: '#5A7DBE', raw_source: '#B0B0B0', tech: '#C97B2C', code: '#4E79A7',
}
const colorFor = (t: string) => PALETTE[t] ?? '#888'

export function GraphView({ version }: { version: number }) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const [graph, setGraph] = useState<NodeLink | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [selected, setSelected] = useState<GNode | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .file('docs/graph/graph.json')
      .then((fc) => {
        if (cancelled) return
        try {
          setGraph(JSON.parse(fc.text) as NodeLink)
          setError(null)
        } catch (e) {
          setError('graph.json is not valid JSON: ' + String(e))
        }
      })
      .catch(() => {
        if (!cancelled) setError('No graph yet. Run `csdd graph build` to generate docs/graph/graph.json.')
      })
    return () => {
      cancelled = true
    }
  }, [version])

  const model = useMemo(() => {
    if (!graph) return null
    const byId: Record<string, Sim> = {}
    const nodes: Sim[] = graph.nodes.map((n, i) => {
      const s: Sim = { ...n, x: Math.cos(i) * 220 + 400, y: Math.sin(i * 1.7) * 220 + 300, vx: 0, vy: 0, deg: 0 }
      byId[n.id] = s
      return s
    })
    const links = graph.links.filter((l) => byId[l.source] && byId[l.target])
    links.forEach((l) => {
      byId[l.source].deg++
      byId[l.target].deg++
    })
    return { nodes, links, byId }
  }, [graph])

  useEffect(() => {
    if (!model) return
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    let raf = 0
    const { nodes, links, byId } = model

    const step = () => {
      for (let i = 0; i < nodes.length; i++) {
        const a = nodes[i]
        for (let j = i + 1; j < nodes.length; j++) {
          const b = nodes[j]
          const dx = a.x - b.x
          const dy = a.y - b.y
          const d2 = dx * dx + dy * dy + 0.01
          const rep = 1400 / d2
          const d = Math.sqrt(d2)
          a.vx += (dx / d) * rep
          a.vy += (dy / d) * rep
          b.vx -= (dx / d) * rep
          b.vy -= (dy / d) * rep
        }
      }
      for (const l of links) {
        const a = byId[l.source]
        const b = byId[l.target]
        const dx = b.x - a.x
        const dy = b.y - a.y
        const d = Math.sqrt(dx * dx + dy * dy) + 0.01
        const k = (d - 90) * 0.01
        a.vx += (dx / d) * k
        a.vy += (dy / d) * k
        b.vx -= (dx / d) * k
        b.vy -= (dy / d) * k
      }
      const cx = canvas.clientWidth / 2
      const cy = canvas.clientHeight / 2
      for (const n of nodes) {
        n.vx += (cx - n.x) * 0.002
        n.vy += (cy - n.y) * 0.002
        n.vx *= 0.85
        n.vy *= 0.85
        n.x += n.vx
        n.y += n.vy
      }
    }

    const draw = () => {
      const dpr = window.devicePixelRatio || 1
      canvas.width = canvas.clientWidth * dpr
      canvas.height = canvas.clientHeight * dpr
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, canvas.clientWidth, canvas.clientHeight)
      ctx.globalAlpha = 0.3
      ctx.strokeStyle = '#8888'
      for (const l of links) {
        const a = byId[l.source]
        const b = byId[l.target]
        ctx.beginPath()
        ctx.moveTo(a.x, a.y)
        ctx.lineTo(b.x, b.y)
        ctx.stroke()
      }
      ctx.globalAlpha = 1
      const f = filter.toLowerCase()
      for (const n of nodes) {
        const dim = f !== '' && !n.label.toLowerCase().includes(f)
        ctx.globalAlpha = dim ? 0.15 : 1
        const r = 4 + Math.min(8, n.deg)
        ctx.beginPath()
        ctx.arc(n.x, n.y, r, 0, 7)
        ctx.fillStyle = colorFor(n.file_type)
        ctx.fill()
        if (selected && n.id === selected.id) {
          ctx.lineWidth = 2
          ctx.strokeStyle = '#111'
          ctx.stroke()
        }
      }
      ctx.globalAlpha = 1
    }

    const loop = () => {
      step()
      draw()
      raf = requestAnimationFrame(loop)
    }
    loop()
    return () => cancelAnimationFrame(raf)
  }, [model, filter, selected])

  const onClick = (e: React.MouseEvent<HTMLCanvasElement>) => {
    if (!model) return
    const rect = e.currentTarget.getBoundingClientRect()
    const mx = e.clientX - rect.left
    const my = e.clientY - rect.top
    let best: Sim | null = null
    let bd = 1e9
    for (const n of model.nodes) {
      const d = (n.x - mx) ** 2 + (n.y - my) ** 2
      if (d < bd) {
        bd = d
        best = n
      }
    }
    setSelected(bd < 400 && best ? best : null)
  }

  const connections = useMemo(() => {
    if (!graph || !selected) return []
    const out: string[] = []
    for (const l of graph.links) {
      if (l.source === selected.id) out.push(`→ ${l.relation} ${l.target}`)
      else if (l.target === selected.id) out.push(`← ${l.relation} ${l.source}`)
    }
    return out
  }, [graph, selected])

  if (error) {
    return (
      <div className="empty">
        <h2>Graph</h2>
        <p className="muted">{error}</p>
      </div>
    )
  }

  return (
    <div className="graph-view" style={{ display: 'flex', height: '100%', minHeight: 0 }}>
      <div style={{ flex: 1, position: 'relative', minWidth: 0 }}>
        <input
          placeholder="filter nodes by label…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{ position: 'absolute', top: 8, left: 8, zIndex: 2, padding: '4px 8px' }}
        />
        <canvas
          ref={canvasRef}
          onClick={onClick}
          style={{ width: '100%', height: '100%', display: 'block' }}
        />
      </div>
      <aside style={{ width: 300, overflow: 'auto', padding: '12px 14px', borderLeft: '1px solid #8883', fontSize: 13 }}>
        <h3 style={{ marginTop: 0 }}>Graph</h3>
        <p className="muted">{graph ? `${graph.nodes.length} nodes · ${graph.links.length} edges` : 'loading…'}</p>
        {selected ? (
          <div>
            <strong>{selected.label}</strong>
            <div className="muted" style={{ margin: '4px 0' }}>
              {selected.file_type}
              {selected.source_file ? ` · ${selected.source_file}` : ''}
            </div>
            {connections.map((c, i) => (
              <div key={i} style={{ margin: '2px 0' }}>
                {c}
              </div>
            ))}
          </div>
        ) : (
          <p className="muted">Click a node to inspect its connections.</p>
        )}
      </aside>
    </div>
  )
}
