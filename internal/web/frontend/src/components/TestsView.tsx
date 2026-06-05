import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Coverage, TestReport, TestSummary } from '../types'
import { ProgressBar, covColor } from './bits'

export function TestsView({ version }: { version: number }) {
  const [rep, setRep] = useState<TestReport | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    setErr(null)
    api.tests().then(setRep).catch((e) => setErr(String(e)))
  }, [version])

  if (err) return <div className="banner error">{err}</div>
  if (!rep) return <div className="empty">Loading…</div>

  const coverage = rep.coverage
  const tests = rep.tests
  if (!coverage && !tests) return <NoReports />

  return (
    <div className="tests-view">
      <div className="spec-header">
        <h1>Tests &amp; Coverage</h1>
      </div>
      {rep.sources.length > 0 && <div className="muted small">from {rep.sources.join(' · ')}</div>}

      <div className="cards">
        {tests && <TestsCard t={tests} />}
        {coverage && <CoverageCard c={coverage} />}
      </div>

      {coverage && coverage.files.length > 0 && <CoverageTable c={coverage} />}
      {tests && (tests.failures?.length ?? 0) > 0 && <Failures failures={tests.failures!} />}
    </div>
  )
}

function TestsCard({ t }: { t: TestSummary }) {
  const passRate = t.total > 0 ? Math.round((t.passed * 100) / t.total) : 0
  return (
    <div className="card">
      <div className="card-title">
        Tests {t.failed > 0 ? <span className="badge warn">{t.failed} failed</span> : <span className="badge ready">passing</span>}
      </div>
      <div className="stat-grid">
        <Stat label="total" value={t.total} />
        <Stat label="passed" value={t.passed} tone="ok" />
        <Stat label="failed" value={t.failed} tone={t.failed > 0 ? 'bad' : undefined} />
        <Stat label="skipped" value={t.skipped} />
      </div>
      <div className="cov-bar-row">
        <ProgressBar pct={passRate} />
        <span className="muted small">{passRate}% pass</span>
      </div>
      {t.durationSec > 0 && <div className="muted small">{t.durationSec.toFixed(2)}s</div>}
    </div>
  )
}

function CoverageCard({ c }: { c: Coverage }) {
  return (
    <div className="card">
      <div className="card-title">
        Coverage <span className="badge muted">{c.format}</span>
      </div>
      <div className="cov-headline">
        <span className="cov-pct" style={{ color: covColor(c.pct) }}>
          {c.pct.toFixed(1)}%
        </span>
        <span className="muted small">
          {c.covered}/{c.lines} lines
        </span>
      </div>
      <ProgressBar pct={c.pct} />
    </div>
  )
}

function CoverageTable({ c }: { c: Coverage }) {
  const rows = c.files.slice(0, 200)
  return (
    <div className="card">
      <div className="card-title">By file ({c.files.length})</div>
      <div className="cov-table">
        {rows.map((f) => (
          <div className="cov-row" key={f.path} title={f.path}>
            <span className="cov-file">{f.path}</span>
            <span className="cov-mini">
              <span className="cov-mini-fill" style={{ width: `${f.pct}%`, background: covColor(f.pct) }} />
            </span>
            <span className="cov-num" style={{ color: covColor(f.pct) }}>
              {f.pct.toFixed(0)}%
            </span>
          </div>
        ))}
      </div>
      {c.files.length > rows.length && (
        <div className="muted small">… {c.files.length - rows.length} more (lowest coverage shown first)</div>
      )}
    </div>
  )
}

function Failures({ failures }: { failures: { suite: string; name: string; message: string }[] }) {
  return (
    <div className="card">
      <div className="card-title">
        Failures <span className="badge warn">{failures.length}</span>
      </div>
      <ul className="fail-list">
        {failures.map((f, i) => (
          <li key={i}>
            <div className="fail-name">
              {f.suite && <span className="muted">{f.suite} › </span>}
              {f.name}
            </div>
            {f.message && <div className="fail-msg">{f.message}</div>}
          </li>
        ))}
      </ul>
    </div>
  )
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: 'ok' | 'bad' }) {
  const color = tone === 'ok' ? 'var(--ok)' : tone === 'bad' ? 'var(--bad)' : undefined
  return (
    <div className="stat">
      <div className="stat-val" style={color ? { color } : undefined}>
        {value}
      </div>
      <div className="stat-label">{label}</div>
    </div>
  )
}

function NoReports() {
  return (
    <div className="empty">
      <h2>No test or coverage reports found</h2>
      <p className="muted">
        The dashboard reads generic report files you generate (it never runs your tests). Produce
        one, then it appears here on the next change:
      </p>
      <pre className="codeblock" style={{ textAlign: 'left', maxWidth: 560, margin: '12px auto' }}>
        <code>{`# Tests (JUnit XML)
pytest --junitxml=junit.xml
jest --reporters=default --reporters=jest-junit
gotestsum --junitfile=junit.xml

# Coverage (lcov or Cobertura)
jest --coverage --coverageReporters=lcov     # coverage/lcov.info
pytest --cov --cov-report=xml                # coverage.xml
coverage lcov                                # lcov.info`}</code>
      </pre>
    </div>
  )
}

