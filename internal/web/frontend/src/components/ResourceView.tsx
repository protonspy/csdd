import { useEffect, useState } from 'react'
import type { Artifact, FileContent } from '../types'
import type { ResourceKind } from '../resources'
import { resourceLabel } from '../resources'
import { api } from '../api'
import { Markdown } from './Markdown'

// stripFrontmatter removes a leading `---\n…\n---` YAML block so the rendered
// body doesn't show raw frontmatter (its fields are surfaced in the header).
function stripFrontmatter(text: string): string {
  const m = /^﻿?---\r?\n[\s\S]*?\r?\n---\r?\n?/.exec(text)
  return m ? text.slice(m[0].length) : text
}

// ResourceView renders an agent / skill / steering file: a metadata header
// (description, tools, inclusion) plus the rendered markdown body. Read-only,
// refetched on the live version bump.
export function ResourceView({
  resource,
  artifact,
  version,
}: {
  resource: ResourceKind
  artifact: Artifact
  version: number
}) {
  const [file, setFile] = useState<FileContent | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setErr(null)
    setFile(null)
    api
      .file(artifact.path)
      .then(setFile)
      .catch((e) => setErr(String(e)))
  }, [artifact.path, version])

  const name = artifact.name.replace(/\.md$/, '')

  return (
    <div className="resource-view">
      <div className="resource-view-header">
        <div className="resource-view-title">
          <span className="badge muted">{resourceLabel(resource)}</span>
          <h1>{name}</h1>
        </div>
        {artifact.description && <p className="resource-view-desc">{artifact.description}</p>}
        <div className="resource-view-meta">
          {artifact.inclusion && (
            <span className="meta-tag">
              inclusion: <b>{artifact.inclusion}</b>
            </span>
          )}
          {artifact.tools && (
            <span className="meta-tag">
              tools: <b>{artifact.tools}</b>
            </span>
          )}
          {artifact.model && (
            <span className="meta-tag">
              model: <b>{artifact.model}</b>
            </span>
          )}
          {artifact.effort && (
            <span className="meta-tag">
              effort: <b>{artifact.effort}</b>
            </span>
          )}
          <span className="meta-tag muted">{artifact.path}</span>
        </div>
      </div>
      <div className="resource-view-body">
        {err ? (
          <div className="banner error">{err}</div>
        ) : !file ? (
          <div className="empty">Loading…</div>
        ) : (
          <Markdown text={stripFrontmatter(file.text)} />
        )}
      </div>
    </div>
  )
}
