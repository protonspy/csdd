import { useEffect, useRef, useState } from 'react'
import mermaid from 'mermaid'

mermaid.initialize({ startOnLoad: false, theme: 'dark', securityLevel: 'strict' })

let counter = 0

// Mermaid renders a diagram from its source. If rendering fails (e.g. invalid
// syntax, or mermaid unavailable), it gracefully falls back to the raw code.
export function Mermaid({ chart }: { chart: string }) {
  const [svg, setSvg] = useState('')
  const [error, setError] = useState(false)
  const idRef = useRef(`mmd-${counter++}`)

  useEffect(() => {
    let cancelled = false
    mermaid
      .render(idRef.current, chart)
      .then(({ svg }) => {
        if (!cancelled) {
          setSvg(svg)
          setError(false)
        }
      })
      .catch(() => {
        if (!cancelled) setError(true)
      })
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
