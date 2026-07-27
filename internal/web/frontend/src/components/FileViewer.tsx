import { useEffect, useState } from 'react'
import Editor from '@monaco-editor/react'
import { api } from '../api'
import type { FileContent } from '../types'
import { useTheme } from '../useTheme'

// Read-only Monaco viewer (VS Code's editor) for any workspace file. Refetches
// on the live version bump so an open file reflects on-disk edits.
export function FileViewer({ path, version }: { path: string; version: number }) {
  const [file, setFile] = useState<FileContent | null>(null)
  const [err, setErr] = useState<string | null>(null)
  // Monaco paints its own chrome and cannot read CSS custom properties.
  const theme = useTheme()

  useEffect(() => {
    setErr(null)
    api.file(path).then(setFile).catch((e) => setErr(String(e)))
  }, [path, version])

  return (
    <div className="file-viewer">
      <div className="file-header">
        <span className="file-path">{path}</span>
        <span className="badge muted">read-only</span>
      </div>
      <div className="editor-wrap">
        {err ? (
          <div className="banner error">{err}</div>
        ) : (
          <Editor
            theme={theme === 'dark' ? 'vs-dark' : 'vs'}
            path={path}
            language={file?.lang}
            value={file?.text ?? ''}
            loading={<div className="empty">Loading editor…</div>}
            options={{
              readOnly: true,
              domReadOnly: true,
              minimap: { enabled: false },
              fontSize: 13,
              lineNumbers: 'on',
              scrollBeyondLastLine: false,
              wordWrap: 'on',
              renderWhitespace: 'selection',
              automaticLayout: true,
            }}
          />
        )}
      </div>
    </div>
  )
}
