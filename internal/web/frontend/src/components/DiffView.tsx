import type { SpecDiff, DiffFile, DiffHunk } from '../types'

// statusTone maps a change status to a pill tone (colors defined in styles.css).
const statusTone: Record<string, string> = {
  added: 'ok',
  modified: 'mid',
  deleted: 'bad',
  renamed: 'info',
}

export function DiffView({ diff }: { diff: SpecDiff }) {
  return (
    <div className="diff-view">
      <div className="card diff-summary">
        <div className="card-title">Changes</div>
        <div className="diff-meta">
          <code className="diff-base">{diff.base}</code>
          <span className="muted"> → working tree</span>
          <span className="diff-stat add">+{diff.summary.additions}</span>
          <span className="diff-stat del">−{diff.summary.deletions}</span>
          <span className="muted small">
            {diff.summary.files} file{diff.summary.files === 1 ? '' : 's'}
          </span>
        </div>
        {diff.truncated && (
          <div className="muted small">Some files were truncated at the line cap — see the full diff in git.</div>
        )}
        <div className="muted small">updated {diff.updatedAt}</div>
      </div>

      {diff.files.length === 0 ? (
        <div className="muted small">No changes against {diff.base}.</div>
      ) : (
        diff.files.map((f) => <FileBlock key={(f.oldPath ?? '') + '→' + f.path} file={f} />)
      )}
    </div>
  )
}

function FileBlock({ file }: { file: DiffFile }) {
  const tone = statusTone[file.status] ?? 'mid'
  return (
    <details className="diff-file" open>
      <summary className="diff-file-head">
        <span className={`pill ${tone}`}>{file.status}</span>
        <span className="diff-path">
          {file.oldPath ? (
            <>
              <span className="muted">{file.oldPath}</span> → {file.path}
            </>
          ) : (
            file.path
          )}
        </span>
        <span className="diff-counts">
          <span className="add">+{file.additions}</span> <span className="del">−{file.deletions}</span>
        </span>
        {file.binary && <span className="badge muted">binary</span>}
        {file.truncated && <span className="badge warn">truncated</span>}
      </summary>
      {file.binary ? (
        <div className="muted small diff-binary">Binary file — no line-level diff.</div>
      ) : (
        (file.hunks ?? []).map((h, i) => <Hunk key={i} hunk={h} />)
      )}
    </details>
  )
}

function Hunk({ hunk }: { hunk: DiffHunk }) {
  return (
    <div className="diff-hunk">
      <div className="diff-line hunk-head">
        <span className="diff-gutter" />
        <span className="diff-gutter" />
        <span className="diff-sign" />
        <span className="diff-text">
          @@ -{hunk.oldStart},{hunk.oldLines} +{hunk.newStart},{hunk.newLines} @@
          {hunk.header ? <span className="muted"> {hunk.header}</span> : null}
        </span>
      </div>
      {hunk.lines.map((ln, i) => (
        <div key={i} className={`diff-line ${ln.type}`}>
          <span className="diff-gutter">{ln.old || ''}</span>
          <span className="diff-gutter">{ln.new || ''}</span>
          <span className="diff-sign">{ln.type === 'add' ? '+' : ln.type === 'del' ? '−' : ' '}</span>
          <span className="diff-text">{ln.text || ' '}</span>
        </div>
      ))}
    </div>
  )
}

export function DiffHint({ feature }: { feature: string }) {
  return (
    <div className="empty">
      <h2>No file diff recorded yet</h2>
      <p className="muted">
        Record this spec's changes as a structured, auditable diff (base → working tree). It
        appears here, and refreshes live, on the next change:
      </p>
      <pre className="codeblock" style={{ textAlign: 'left', maxWidth: 560, margin: '12px auto' }}>
        <code>{`# Diff against the auto-detected base (origin/main, main, …)
npx @protonspy/csdd spec diff-report ${feature}

# Or against an explicit base ref
npx @protonspy/csdd spec diff-report ${feature} --base main`}</code>
      </pre>
    </div>
  )
}
