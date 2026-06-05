import type { TaskPhase, Task } from '../types'

export function TaskBoard({ phases }: { phases: TaskPhase[] }) {
  if (!phases.length) return <div className="empty">No tasks parsed yet.</div>
  return (
    <div className="board">
      {phases.map((p) => {
        const total = p.tasks.length
        const done = p.tasks.filter((t) => t.done).length
        return (
          <div className="board-col" key={p.name}>
            <div className="board-col-head">
              <span className="col-name">{p.name}</span>
              <span className="muted small">
                {done}/{total}
              </span>
            </div>
            <div className="board-cards">
              {p.tasks.map((t, i) => (
                <TaskCard key={`${t.id}-${i}`} task={t} />
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function TaskCard({ task }: { task: Task }) {
  return (
    <div className={`task-card ${task.done ? 'done' : ''} ${task.indent > 0 ? 'sub' : 'major'}`}>
      <div className="task-top">
        <span className={`check ${task.done ? 'on' : ''}`}>{task.done ? '✓' : ''}</span>
        <span className="task-id">{task.id}</span>
        {task.tdd && <span className={`tdd ${task.tdd.toLowerCase()}`}>{task.tdd}</span>}
        {task.parallel && (
          <span className="chip-p" title="parallel-capable">
            P
          </span>
        )}
      </div>
      <div className="task-title">{task.title}</div>
      {(task.boundary || task.requirements?.length || task.depends?.length) && (
        <div className="task-tags">
          {task.boundary && <span className="tag boundary">{task.boundary}</span>}
          {task.requirements?.map((r) => (
            <span className="tag req" key={`r${r}`} title="requirement">
              {r}
            </span>
          ))}
          {task.depends?.map((d) => (
            <span className="tag dep" key={`d${d}`} title="depends on">
              →{d}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
