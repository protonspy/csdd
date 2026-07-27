import type { ReactNode } from 'react'

/** Severity of a percentage, for a meter fill. */
export type Tone = '' | 'good' | 'warn' | 'bad'

export function toneFor(pct: number): Tone {
  if (pct >= 80) return 'good'
  if (pct >= 50) return 'warn'
  return 'bad'
}

export function ProgressBar({ pct, tone = '' }: { pct: number; tone?: Tone }) {
  return (
    <div className={`progress ${tone}`.trim()} aria-label={`${Math.round(pct)}% complete`}>
      <div className="progress-fill" style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} />
    </div>
  )
}

// covColor maps a coverage/pass percentage to a status colour, for the few
// places that colour a mark (never text) directly.
export function covColor(pct: number): string {
  if (pct >= 80) return 'var(--good)'
  if (pct >= 50) return 'var(--warning)'
  return 'var(--critical)'
}

export function PhasePill({ phase }: { phase: string }) {
  const tone = phase.endsWith('approved')
    ? 'ok'
    : phase.endsWith('generated')
      ? 'mid'
      : phase === '(unreadable)'
        ? 'bad'
        : 'dim'
  return <span className={`pill ${tone}`}>{phase || 'init'}</span>
}

/**
 * A badge is a glyph plus a word. The glyph comes from CSS, so a caller only
 * picks the tone — and `muted`, the one tone with no status colour, gets no
 * glyph because it has no state to explain.
 */
export function Badge({
  tone = 'muted',
  title,
  children,
}: {
  tone?: 'ok' | 'ready' | 'warn' | 'attention' | 'info' | 'accent' | 'muted'
  title?: string
  children: ReactNode
}) {
  return (
    <span className={`badge ${tone}`} title={title}>
      {children}
    </span>
  )
}

/**
 * Stat tile: label · value · optional unit and footnote. The value keeps the
 * font's proportional figures — `tabular-nums` gives every digit the width of a
 * zero, which reads loose at display sizes; it belongs in columns, not here.
 */
export function Stat({
  label,
  value,
  unit,
  foot,
  href,
}: {
  label: string
  value: ReactNode
  unit?: string
  foot?: ReactNode
  href?: string
}) {
  const body = (
    <>
      <span className="stat-label">{label}</span>
      <span className="stat-value">
        {value}
        {unit && <span className="stat-unit">{unit}</span>}
      </span>
      {foot && <span className="stat-foot">{foot}</span>}
    </>
  )
  return href ? (
    <a className="stat stat-link" href={href}>
      {body}
    </a>
  ) : (
    <div className="stat">{body}</div>
  )
}

export function KpiRow({ children }: { children: ReactNode }) {
  return <div className="kpi-row">{children}</div>
}

/** A titled surface. `right` sits at the far end of the header. */
export function Panel({
  title,
  right,
  flush,
  children,
}: {
  title: string
  right?: ReactNode
  flush?: boolean
  children: ReactNode
}) {
  return (
    <section className="panel">
      <header className="panel-head">
        <h2>{title}</h2>
        <span className="panel-head-spacer" />
        {right}
      </header>
      <div className={`panel-body ${flush ? 'flush' : ''}`.trim()}>{children}</div>
    </section>
  )
}
