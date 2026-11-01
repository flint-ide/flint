import { useEffect } from 'react'
import { formatChanges, stateInfo, taskActions, type TaskView } from '../../lib/agentTasks'
import {
  askAgentAboutTask,
  createTaskPullRequest,
  followTasks,
  mergeTask,
  openNewTask,
  openTask,
  openTaskFileDiff,
  removeTask,
  toggleChanges,
  useTasks,
  type TaskChanges,
} from '../../store/agentTasks'
import { BrowserOpenURL } from '../../wails/runtime'
import { NewTaskForm } from './NewTaskForm'

const basename = (p: string) => p.slice(p.lastIndexOf('/') + 1)

/** One row per agent task: state, branch, changes and actions. */
export function TasksPanel() {
  const { tasks, changes, expanded, busy, error, newTaskOpen } = useTasks()
  useEffect(() => followTasks(), [])

  return (
    <div className="panel tasks-panel">
      <div className="panel-header">
        <span className="panel-title">Agent Tasks</span>
        <button className="icon" title="New Agent Task…" onClick={openNewTask}>＋</button>
      </div>
      <div className="tree scroll">
        {error && <div className="placeholder error">{error}</div>}
        {tasks.map((t) => (
          <TaskRow key={t.id} t={t} changes={changes[t.id]} open={!!expanded[t.id]} busy={!!busy[t.id]} />
        ))}
        {!error && tasks.length === 0 && (
          <div className="placeholder muted">
            No agent tasks. <button className="link" onClick={openNewTask}>New Agent Task…</button> gives an agent its own branch and
            worktree, so several can work in parallel.
          </div>
        )}
      </div>
      {newTaskOpen && <NewTaskForm />}
    </div>
  )
}

interface RowProps {
  t: TaskView
  changes: TaskChanges | undefined
  open: boolean
  busy: boolean
}

function TaskRow({ t, changes, open, busy }: RowProps) {
  const s = stateInfo(t.state)
  const a = taskActions(t)
  const status = t.stale ? 'worktree gone' : t.merged ? 'merged' : formatChanges(changes)
  return (
    <div className="task">
      <div className="tree-row static task-row" title={`${t.prompt}\n\n${t.worktree}`} onClick={() => a.diff && toggleChanges(t)}>
        <span className="twisty">{a.diff ? (open ? '▾' : '▸') : ' '}</span>
        <span className={`task-state ${t.stale ? 'stale' : s.state}`} title={t.stale ? 'The worktree folder is gone' : t.error || s.hint}>
          {t.stale ? 'stale' : s.label}
        </span>
        <span className="file-name">{t.title}</span>
      </div>
      <div className="task-meta muted">
        <span title={`${t.branch} from ${t.base} · ${t.agent}`}>⎇ {t.branch}</span>
        <span>{status}</span>
        {t.ahead > 0 && !t.merged && <span title={`Commits not in ${t.base}`}>↑{t.ahead}</span>}
      </div>
      {t.state === 'failed' && t.error && <div className="task-error">{t.error}</div>}
      <div className="task-actions">
        {a.open && <button className="link" disabled={busy} onClick={() => void openTask(t)} title="Open the folder and the agent chat">Open</button>}
        {a.diff && <button className="link" onClick={() => toggleChanges(t)}>{open ? 'Hide diff' : 'Show diff'}</button>}
        {a.askAgent && <button className="link" onClick={() => void askAgentAboutTask(t)} title="Put the error in the task's chat for the agent">✦ Ask agent</button>}
        {a.merge && <button className="link" disabled={busy} onClick={() => void mergeTask(t)} title={`Merge into ${t.base} where it is checked out`}>Merge</button>}
        {a.pr && <button className="link" disabled={busy} onClick={() => void createTaskPullRequest(t)} title="Push the branch and create a pull request with gh">Create PR</button>}
        {t.pullRequest && <button className="link" onClick={() => BrowserOpenURL(t.pullRequest)} title={t.pullRequest}>PR ↗</button>}
        <span className="spacer" />
        <button className="icon" disabled={busy} title="Remove the task" onClick={() => void removeTask(t)}>✕</button>
      </div>
      {open &&
        changes?.files?.map((f) => (
          <div
            key={f.path}
            className="tree-row static change-row indent-2"
            title={f.origPath ? `${f.origPath} → ${f.path}` : f.path}
            onClick={() => openTaskFileDiff(t, f)}
          >
            <span className={`file-name${f.untracked ? ' change-untracked' : ''}`}>{basename(f.path)}</span>
            <span className="muted path-hint">{f.path.includes('/') ? f.path.slice(0, f.path.lastIndexOf('/')) : ''}</span>
            <span className="task-lines">{f.binary ? 'bin' : `+${f.added} −${f.deleted}`}</span>
          </div>
        ))}
      {open && changes?.truncated && <div className="muted indent-2 task-more">…and {changes.count - changes.files.length} more files</div>}
    </div>
  )
}
