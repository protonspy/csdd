export function ProgressBar({ pct }: { pct: number }) {
  return (
    <div className="progress" aria-label={`${pct}% complete`}>
      <div className="progress-fill" style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} />
    </div>
  )
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
