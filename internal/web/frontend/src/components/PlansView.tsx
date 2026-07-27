import { useEffect, useState } from 'react'
import { api } from '../api'
import type { PlanSummary, PlanDetail, PlanFeat, MilestoneProgress } from '../types'
import { ProgressBar, Stat } from './bits'
import { href, useRoute } from '../router'
import { RefList } from './Ref'
import { slugifyAnchor, useFlashTarget } from '../useFlashTarget'

// PlansView is the read-only Plans area: a list of plans with approval/drift
// badges, and a per-plan view rendering the feat table with derived status,
// milestone progress, blocked flags, and the run journal (log.md).
//
// Which plan is open comes from the route, not from local state, so a plan is a
// URL that survives a reload and can be pasted into a review.
export function PlansView({ slug, version }: { slug: string | null; version: number }) {
  const [plans, setPlans] = useState<PlanSummary[] | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const feat = useRoute().query.feat ?? null

  useEffect(() => {
    setErr(null)
    api.plans().then(setPlans).catch((e) => setErr(String(e)))
  }, [version])

  if (slug) return <PlanDetailView slug={slug} version={version} feat={feat} />
  if (err) return <div className="banner error">{err}</div>
  if (!plans) return <div className="empty">Loading…</div>
  if (plans.length === 0) return <NoPlans />

  return (
    <div className="plans-view">
      <div className="spec-header">
        <h1>Plans</h1>
      </div>
      <div className="cards">
        {plans.map((p) => (
          <a key={p.slug} className="card plan-card" href={href('plans', p.slug)}>
            <div className="card-title">
              {p.name || p.slug} <PlanStateBadge approved={p.approved} drift={p.drift} complete={p.complete} />
            </div>
            <div className="muted small">{p.slug}</div>
            <div className="stat-grid">
              <Stat label="feats" value={p.feats} />
              <Stat label="done" value={p.done} unit={`/${p.feats}`} />
            </div>
            <ProgressBar pct={p.feats > 0 ? Math.round((p.done * 100) / p.feats) : 0} />
          </a>
        ))}
      </div>
    </div>
  )
}

function PlanDetailView({ slug, version, feat }: { slug: string; version: number; feat: string | null }) {
  const [d, setD] = useState<PlanDetail | null>(null)
  const [journal, setJournal] = useState<string>('')
  const [err, setErr] = useState<string | null>(null)
  // A feat:<slug> citation lands here; flash the row it meant.
  useFlashTarget(feat, [d])

  useEffect(() => {
    setErr(null)
    api.plan(slug).then(setD).catch((e) => setErr(String(e)))
    // The run journal is fetched through the hardened /api/file route so it
    // inherits the host guard and secret redaction.
    api
      .file(`docs/plans/${slug}/log.md`)
      .then((f) => setJournal(f.text ?? ''))
      .catch(() => setJournal(''))
  }, [slug, version])

  if (err) return <div className="banner error">{err}</div>
  if (!d) return <div className="empty">Loading…</div>

  return (
    <div className="plan-detail">
      <div className="spec-header">
        <a className="link-btn" href="#/plans">
          ← Plans
        </a>
        <h1>
          {d.name || d.slug} <PlanStateBadge approved={d.approved} drift={d.drift} complete={d.complete} />
        </h1>
      </div>

      {d.milestones.length > 0 && (
        <div className="card">
          <div className="card-title">Milestones</div>
          {d.milestones.map((m) => (
            <MilestoneRow key={m.name || '—'} m={m} />
          ))}
        </div>
      )}

      <div className="card">
        <div className="card-title">Feats ({d.feats.length})</div>
        <div className="feat-table">
          {d.feats.map((f) => (
            <FeatRow key={f.slug} f={f} />
          ))}
        </div>
      </div>

      {journal.trim() !== '' && (
        <div className="card">
          <div className="card-title">Run journal (log.md)</div>
          <pre className="codeblock">
            <code>{journal}</code>
          </pre>
        </div>
      )}
    </div>
  )
}

function FeatRow({ f }: { f: PlanFeat }) {
  return (
    <div className="feat-row" id={`t-${slugifyAnchor(f.slug)}`} title={f.objective}>
      <span className="feat-num muted">{f.num}</span>
      <span className="feat-slug">
        {f.slug}
        {f.parallel && <span className="badge muted" title="may run in parallel with milestone siblings">P</span>}
      </span>
      <span className="feat-objective muted">{f.objective}</span>
      <StateBadge state={f.state} />
      <span className="feat-progress muted small">
        {f.tasks_total > 0 ? `${f.tasks_checked}/${f.tasks_total} tasks` : ''}
      </span>
      {/* The citation column: what this feat says it depends on, resolved. */}
      <span className="feat-refs">
        <RefList tokens={f.refs} empty="" />
      </span>
    </div>
  )
}

function MilestoneRow({ m }: { m: MilestoneProgress }) {
  const pct = m.total > 0 ? Math.round((m.done * 100) / m.total) : 0
  return (
    <div className="cov-bar-row">
      <span className="feat-slug">{m.name || '—'}</span>
      <ProgressBar pct={pct} />
      <span className="muted small">
        {m.done}/{m.total}
      </span>
    </div>
  )
}

/**
 * One badge for the plan's state, in priority order: drift first because it is
 * a problem, then complete because it is the terminal state and says more than
 * "approved", then approval. Showing complete *and* approved together would be
 * two badges saying one thing.
 */
export function PlanStateBadge({
  approved,
  drift,
  complete,
}: {
  approved: boolean
  drift: boolean
  complete?: boolean
}) {
  if (drift) return <span className="badge warn">drift</span>
  if (complete) return <span className="badge ok">complete</span>
  if (approved) return <span className="badge ready">approved</span>
  return <span className="badge muted">draft</span>
}

function StateBadge({ state }: { state: string }) {
  const tone =
    state === 'done' ? 'ready' : state === 'implementing' || state === 'ready' ? 'info' : 'muted'
  return <span className={`badge ${tone}`}>{state}</span>
}

function NoPlans() {
  return (
    <div className="empty">
      <h2>No plans yet</h2>
      <p className="muted">
        A plan decomposes an initiative into feats (each becomes one spec). Author one with the
        <code> /prd </code> skill, or scaffold directly:
      </p>
      <pre className="codeblock" style={{ textAlign: 'left', maxWidth: 480, margin: '12px auto' }}>
        <code>{`npx @protonspy/csdd plan init <slug>
# author the feat table, then:
npx @protonspy/csdd plan validate <slug>
npx @protonspy/csdd plan approve <slug>`}</code>
      </pre>
    </div>
  )
}
