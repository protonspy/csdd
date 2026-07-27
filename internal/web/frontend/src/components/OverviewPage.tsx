import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { Overview, PlanSummary, SpecCard, TestReport } from '../types'
import { KpiRow, Panel, ProgressBar, Stat, toneFor } from './bits'
import { href } from '../router'
import { PlanStateBadge } from './PlansView'

// The one screen that answers "where does this workspace stand". Everything on
// it is a link to the page that owns the number — this is a landing, not a
// dashboard to sit on.
//
// It reports only what the API already knows. The gate strip from the design
// draft (graph analyze / wiki lint / codewiki lint) is deliberately absent:
// those verdicts live in the CLI and no endpoint serves them yet, and a gate
// panel that guesses is worse than no gate panel.
export function OverviewPage({ overview, version }: { overview: Overview | null; version: number }) {
  const [tests, setTests] = useState<TestReport | null>(null)
  const [plans, setPlans] = useState<PlanSummary[]>([])

  useEffect(() => {
    api.tests().then(setTests).catch(() => setTests(null))
    api.plans().then((p) => setPlans(p ?? [])).catch(() => setPlans([]))
  }, [version])

  const specs = overview?.specs ?? []
  const roll = useMemo(() => rollUp(specs), [specs])

  if (!overview) return <div className="empty">Loading workspace…</div>

  if (specs.length === 0 && plans.length === 0) {
    return (
      <div className="empty">
        <h2>Nothing to show yet</h2>
        <p className="muted">
          This workspace has no specs and no plans. Start one with{' '}
          <code>csdd spec init &lt;feature&gt;</code>, or plan a multi-feature initiative with the{' '}
          <code>/prd</code> skill.
        </p>
      </div>
    )
  }

  const t = tests?.tests ?? null
  const cov = tests?.coverage ?? null

  return (
    <div className="overview">
      <header className="overview-head">
        <div style={{ minWidth: 0 }}>
          <h1>{workspaceName(overview.root)}</h1>
          <p className="page-sub">
            {specs.length} spec{specs.length === 1 ? '' : 's'}
            {plans.length > 0 && ` · ${plans.length} plan${plans.length === 1 ? '' : 's'}`}
            {roll.issues > 0 && ` · ${roll.issues} validation issue${roll.issues === 1 ? '' : 's'}`}
          </p>
        </div>
      </header>

      <section className="hero-card">
        <div className="hero">
          <span className="hero-value">{roll.total > 0 ? `${roll.pct}%` : '—'}</span>
          <span className="hero-side">
            of {roll.total} task{roll.total === 1 ? '' : 's'} checked
            <br />
            <span className="muted small">
              across {specs.length} spec{specs.length === 1 ? '' : 's'}
            </span>
          </span>
        </div>
        {plans.length > 0 && (
          <div className="hero-meters">
            {plans.map((p) => {
              const pct = p.feats > 0 ? Math.round((p.done * 100) / p.feats) : 0
              return (
                <div className="meter-row" key={p.slug}>
                  <span className="meter-row-label">{p.name || p.slug}</span>
                  <span className="meter-row-value">
                    {p.done}/{p.feats}
                  </span>
                  <ProgressBar pct={pct} />
                </div>
              )
            })}
          </div>
        )}
      </section>

      <KpiRow>
        <Stat
          label="Specs completed"
          value={roll.done}
          unit={`/${specs.length}`}
          foot={`${roll.active} in progress`}
          href="#/specs"
        />
        <Stat
          label="Ready to implement"
          value={roll.ready}
          foot={roll.ready === 0 ? 'none past the tasks gate' : 'approved through tasks'}
          href="#/specs"
        />
        {t && (
          <Stat
            label="Tests passing"
            value={t.passed.toLocaleString()}
            unit={`/${t.total.toLocaleString()}`}
            foot={t.failed > 0 ? `${t.failed} failing` : 'all green'}
            href="#/tests"
          />
        )}
        {cov && (
          <Stat
            label="Coverage"
            value={cov.pct.toFixed(1)}
            unit="%"
            foot={`${cov.covered.toLocaleString()} of ${cov.lines.toLocaleString()} lines`}
            href="#/tests"
          />
        )}
        <Stat
          label="Validation issues"
          value={roll.issues}
          foot={roll.issues === 0 ? 'every spec validates' : 'across all specs'}
          href="#/specs"
        />
      </KpiRow>

      <div className="overview-cols">
        <Panel title="Specs in progress" right={<a className="badge muted" href="#/specs">all specs</a>}>
          {roll.inProgress.length === 0 ? (
            <p className="muted small" style={{ margin: 0 }}>
              Nothing in flight — every spec with tasks is complete.
            </p>
          ) : (
            roll.inProgress.slice(0, 6).map((s) => (
              <a className="mini-row" key={s.feature} href={href('specs', s.feature)}>
                <span className={`state-dot ${s.ready ? 'ready' : ''}`} title={s.phase} />
                <span className="mini-row-name">{s.feature}</span>
                <span className="mini-row-spacer" />
                <span className="muted small">
                  {s.tasks.total > 0 ? `${s.tasks.done}/${s.tasks.total}` : s.phase}
                </span>
              </a>
            ))
          )}
        </Panel>

        {plans.length > 0 && (
          <Panel title="Plans" right={<a className="badge muted" href="#/plans">all plans</a>}>
            {plans.map((p) => (
              <a className="mini-row" key={p.slug} href={href('plans', p.slug)}>
                <span className={`state-dot ${p.complete ? 'done' : 'running'}`} />
                <span className="mini-row-name">{p.name || p.slug}</span>
                <span className="mini-row-spacer" />
                <PlanStateBadge approved={p.approved} drift={p.drift} complete={p.complete} />
              </a>
            ))}
          </Panel>
        )}

        {t && t.failed > 0 && (
          <Panel title={`Failing tests · ${t.failed}`} right={<a className="badge muted" href="#/tests">tests</a>}>
            {(tests?.tests?.failures ?? []).slice(0, 5).map((f, i) => (
              <div className="mini-row" key={`${f.suite}-${f.name}-${i}`}>
                <span className="state-dot blocked" />
                <span className="mini-row-name">{f.name}</span>
                <span className="mini-row-spacer" />
                <span className="muted small">{f.suite}</span>
              </div>
            ))}
          </Panel>
        )}
      </div>

      {cov && (
        <Panel title="Coverage" right={<span className="muted small">{cov.format}</span>}>
          <div className="meter-row">
            <span className="meter-row-label">{cov.source || 'workspace'}</span>
            <span className="meter-row-value">{cov.pct.toFixed(1)}%</span>
            <ProgressBar pct={cov.pct} tone={toneFor(cov.pct)} />
          </div>
        </Panel>
      )}
    </div>
  )
}

interface RollUp {
  total: number
  checked: number
  pct: number
  done: number
  active: number
  ready: number
  issues: number
  inProgress: SpecCard[]
}

// A spec counts as completed once every one of its tasks is checked off — the
// same rule the Specs page uses, so the two screens never disagree.
function rollUp(specs: SpecCard[]): RollUp {
  let total = 0
  let checked = 0
  let done = 0
  let ready = 0
  let issues = 0
  const inProgress: SpecCard[] = []
  for (const s of specs) {
    total += s.tasks.total
    checked += s.tasks.done
    issues += s.issues
    if (s.ready) ready++
    if (s.tasks.total > 0 && s.tasks.done >= s.tasks.total) done++
    else inProgress.push(s)
  }
  return {
    total,
    checked,
    pct: total > 0 ? Math.round((checked * 100) / total) : 0,
    done,
    active: specs.length - done,
    ready,
    issues,
    inProgress,
  }
}

/** Last segment of the workspace root — the project's own name. */
function workspaceName(root: string): string {
  const parts = root.replace(/[/\\]+$/, '').split(/[/\\]/)
  return parts[parts.length - 1] || root
}
