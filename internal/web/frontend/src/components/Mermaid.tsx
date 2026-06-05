import { useEffect, useRef, useState } from 'react'
import mermaid from 'mermaid'

// suppressErrorRendering makes render() throw on a parse error instead of
// drawing Mermaid's "Syntax error in text" graphic, so our raw-source fallback
// takes over.
mermaid.initialize({
  startOnLoad: false,
  theme: 'dark',
  securityLevel: 'strict',
  suppressErrorRendering: true,
})

let counter = 0

// Mermaid renders a diagram from its source. Invalid diagrams (e.g. an unfilled
// template Boundary Map with `<placeholder>` tokens) gracefully fall back to the
// raw source rather than showing Mermaid's error graphic.
export function Mermaid({ chart }: { chart: string }) {
  const [svg, setSvg] = useState('')
  const [error, setError] = useState(false)
  const idRef = useRef(`mmd-${counter++}`)

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      try {
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
  }, [chart])

  if (error) {
    return (
      <pre className="codeblock mermaid-fallback">
        <code>{chart}</code>
      </pre>
    )
  }
  return <div className="mermaid-diagram" dangerouslySetInnerHTML={{ __html: svg }} />
}
