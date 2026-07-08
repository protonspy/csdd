import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Mermaid } from './Mermaid'

// Renders spec/wiki markdown. Fenced ```mermaid blocks become live diagrams;
// other fenced blocks become styled code; inline code stays inline. The default
// <pre> wrapper is flattened so our custom code renderer owns block layout.
//
// When onWikiLink is set, links with a `wiki:<slug>` href (WikiView rewrites
// [[wikilinks]] into these) are intercepted and routed in-app instead of
// navigating; a `wiki:` href with no slug renders as a dead "broken" link. Passing
// nothing leaves link handling at react-markdown's default (SpecView's behavior).
export function Markdown({ text, onWikiLink }: { text: string; onWikiLink?: (slug: string) => void }) {
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
          ...(onWikiLink
            ? {
                a({ href, children }: { href?: string; children?: ReactNode }) {
                  if (href?.startsWith('wiki:')) {
                    const slug = href.slice('wiki:'.length)
                    if (!slug) {
                      return (
                        <span className="wikilink broken" title="no such page">
                          {children}
                        </span>
                      )
                    }
                    return (
                      <a
                        className="wikilink"
                        href="#"
                        onClick={(e) => {
                          e.preventDefault()
                          onWikiLink(slug)
                        }}
                      >
                        {children}
                      </a>
                    )
                  }
                  return (
                    <a href={href} target="_blank" rel="noreferrer">
                      {children}
                    </a>
                  )
                },
              }
            : {}),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
