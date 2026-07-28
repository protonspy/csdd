import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Mermaid } from './Mermaid'
import { Ref } from './Ref'

// Renders spec/wiki markdown. Fenced ```mermaid blocks become live diagrams;
// other fenced blocks become styled code; inline code stays inline. The default
// <pre> wrapper is flattened so our custom code renderer owns block layout.
//
// Links with a `ref:<token>` href become citation chips. A caller opts in by
// running its source through `rewriteRefTokens` first — that is what turns a
// citation written mid-sentence into a resolved, hoverable link without the
// author writing any markup.
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
          a({ href, children }: { href?: string; children?: ReactNode }) {
            if (href?.startsWith('ref:')) {
              return <Ref token={href.slice('ref:'.length)} />
            }
            // An in-app route (the hash router's `#/…`) is navigation, not an
            // outbound link: it stays in this tab.
            if (href?.startsWith('#')) {
              return <a href={href}>{children}</a>
            }
            return (
              <a href={href} target="_blank" rel="noreferrer">
                {children}
              </a>
            )
          },
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
