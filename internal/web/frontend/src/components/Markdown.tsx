import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Mermaid } from './Mermaid'

// Renders spec markdown. Fenced ```mermaid blocks become live diagrams; other
// fenced blocks become styled code; inline code stays inline. The default <pre>
// wrapper is flattened so our custom code renderer owns block layout.
export function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          pre: ({ children }) => <>{children}</>,
          code({ className, children }) {
            const raw = String(children ?? '').replace(/\n$/, '')
            const match = /language-(\w+)/.exec(className || '')
            const isBlock = !!match || raw.includes('\n')
            if (isBlock && match?.[1] === 'mermaid') {
              return <Mermaid chart={raw} />
            }
            if (isBlock) {
              return (
                <pre className="codeblock">
                  <code className={className}>{raw}</code>
                </pre>
              )
            }
            return <code className="inline-code">{children}</code>
          },
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
