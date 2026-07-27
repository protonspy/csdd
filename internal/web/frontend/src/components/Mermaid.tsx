import { useEffect, useRef, useState } from 'react'
import mermaid from 'mermaid'
import { useTheme } from '../useTheme'

// suppressErrorRendering makes render() throw on a parse error instead of
// drawing Mermaid's "Syntax error in text" graphic, so our raw-source fallback
// takes over.
//
// Mermaid's theme is global and applied at render time, not per diagram, so it
// is (re-)initialised inside the effect below and every mounted diagram redraws
// when the theme changes.
const BASE_CONFIG = {
  startOnLoad: false,
  securityLevel: 'strict' as const,
  suppressErrorRendering: true,
}

let counter = 0

// Mermaid renders a diagram from its source. Invalid diagrams (e.g. an unfilled
// template Boundary Map with `<placeholder>` tokens) gracefully fall back to the
// raw source rather than showing Mermaid's error graphic.
export function Mermaid({ chart }: { chart: string }) {
  const [svg, setSvg] = useState('')
  const [error, setError] = useState(false)
  const idRef = useRef(`mmd-${counter++}`)
  const theme = useTheme()

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      try {
        mermaid.initialize({ ...BASE_CONFIG, theme: theme === 'dark' ? 'dark' : 'default' })
        // parse with suppressErrors returns false (no throw) on invalid input.
        const ok = await mermaid.parse(chart, { suppressErrors: true })
        if (ok === false) {
          if (!cancelled) setError(true)
          return
        }
        const { svg: out } = await mermaid.render(idRef.current, chart)
        if (!cancelled) {
          setSvg(out)
          setError(false)
        }
      } catch {
        if (!cancelled) setError(true)
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [chart, theme])

  if (error) {
    return (
      <pre className="codeblock mermaid-fallback">
        <code>{chart}</code>
      </pre>
    )
  }
  return <div className="mermaid-diagram" dangerouslySetInnerHTML={{ __html: svg }} />
}
