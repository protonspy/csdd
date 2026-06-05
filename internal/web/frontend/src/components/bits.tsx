export function ProgressBar({ pct }: { pct: number }) {
  return (
    <div className="progress" aria-label={`${pct}% complete`}>
      <div className="progress-fill" style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} />
    </div>
  )
}

// covColor maps a coverage/pass percentage to a status color.
export function covColor(pct: number): string {
  if (pct >= 80) return 'var(--ok)'
  if (pct >= 50) return 'var(--mid)'
  return 'var(--bad)'
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
