import { useMemo, useState } from 'react'
import type { SpecCard } from '../types'
import { ProgressBar, PhasePill } from './bits'

type StatusFilter = 'all' | 'active' | 'done'
const PAGE_SIZE = 12

// A spec counts as completed once every one of its tasks is checked off.
function isCompleted(s: SpecCard): boolean {
  return s.tasks.total > 0 && s.tasks.done >= s.tasks.total
}

// SpecsPage is the dedicated Specs area: a searchable, status-filterable,
// paginated grid of spec cards. Scales to many specs without a giant scroll.
export function SpecsPage({ specs, onOpen }: { specs: SpecCard[] | null; onOpen: (feature: string) => void }) {
  const [status, setStatus] = useState<StatusFilter>('all')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(0)

  const all = specs ?? []

  const counts = useMemo(() => {
    let done = 0
    for (const s of all) if (isCompleted(s)) done++
    return { all: all.length, done, active: all.length - done }
  }, [all])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return all.filter((s) => {
      if (status === 'done' && !isCompleted(s)) return false
      if (status === 'active' && isCompleted(s)) return false
      if (q && !s.feature.toLowerCase().includes(q)) return false
      return true
    })
  }, [all, status, query])

  // Clamp the page when a filter shrinks the result set.
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const safePage = Math.min(page, pageCount - 1)
  const start = safePage * PAGE_SIZE
  const shown = filtered.slice(start, start + PAGE_SIZE)

  const setFilter = (f: StatusFilter) => {
    setStatus(f)
    setPage(0)
  }

  if (!specs) return <div className="empty">Loading workspace…</div>
  if (all.length === 0) {
    return (
      <div className="empty">
        <h2>No specs yet</h2>
        <p className="muted">
          Create one with <code>csdd spec init &lt;feature&gt;</code>, then generate its requirements.
        </p>
      </div>
    )
  }

  return (
    <div className="specs-page">
      <div className="specs-page-bar">
        <h1>Specs</h1>
        <div className="filter-pills">
          {(
            [
              ['all', 'All', counts.all],
              ['active', 'In progress', counts.active],
              ['done', 'Completed', counts.done],
            ] as [StatusFilter, string, number][]
          ).map(([id, label, n]) => (
            <button key={id} className={`filter-pill ${status === id ? 'active' : ''}`} onClick={() => setFilter(id)}>
              {label} <span className="filter-pill-n">{n}</span>
            </button>
          ))}
        </div>
        <input
          className="specs-search"
          placeholder="Filter by name…"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setPage(0)
          }}
        />
      </div>

      {filtered.length === 0 ? (
        <div className="empty small">No specs match this filter.</div>
      ) : (
        <>
          <div className="specs-grid">
            {shown.map((s) => (
              <SpecCardItem key={s.feature} spec={s} onClick={() => onOpen(s.feature)} />
            ))}
          </div>
          <Pager
            page={safePage}
            pageCount={pageCount}
            start={start}
            shown={shown.length}
            total={filtered.length}
            onPrev={() => setPage((p) => Math.max(0, p - 1))}
            onNext={() => setPage((p) => Math.min(pageCount - 1, p + 1))}
          />
        </>
      )}
    </div>
  )
}

function SpecCardItem({ spec, onClick }: { spec: SpecCard; onClick: () => void }) {
  const done = isCompleted(spec)
  return (
    <button className="spec-card" onClick={onClick}>
      <div className="spec-card-head">
        <span className="spec-card-name">{spec.feature}</span>
        {done ? (
          <span className="badge ok" title="all tasks complete">
            done
          </span>
        ) : spec.ready ? (
          <span className="badge ready" title="ready for implementation">
            ready
          </span>
        ) : spec.issues > 0 ? (
          <span className="badge warn" title={`${spec.issues} validation issue(s)`}>
            {spec.issues}
          </span>
        ) : (
          <span className="badge muted">active</span>
        )}
      </div>
      <div className="spec-card-meta">
        <PhasePill phase={spec.phase} />
        {spec.tasks.total > 0 && (
          <span className="muted small">
            {spec.tasks.done}/{spec.tasks.total} tasks
          </span>
        )}
      </div>
      {spec.tasks.total > 0 && <ProgressBar pct={spec.tasks.pct} />}
    </button>
  )
}

function Pager({
  page,
  pageCount,
  start,
  shown,
  total,
  onPrev,
  onNext,
}: {
  page: number
  pageCount: number
  start: number
  shown: number
  total: number
  onPrev: () => void
  onNext: () => void
}) {
  if (total === 0) return null
  return (
    <div className="pager">
      <span className="pager-range">
        {start + 1}–{start + shown} of {total}
      </span>
      <div className="pager-btns">
        <button className="pager-btn" disabled={page === 0} onClick={onPrev}>
          ‹ Prev
        </button>
        <span className="pager-page">
          {page + 1} / {pageCount}
        </span>
        <button className="pager-btn" disabled={page >= pageCount - 1} onClick={onNext}>
          Next ›
        </button>
      </div>
    </div>
  )
}
