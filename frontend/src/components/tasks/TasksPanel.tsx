import { useEffect } from 'react'
import { attemptLabels, formatChanges, formatTaskUsage, groupRows, groupSummary, stateInfo, taskActions, type TaskView } from '../../lib/agentTasks'
import {
  askAgentAboutTask,
  createTaskPullRequest,
  followTasks,
  keepTask,
  mergeTask,
  openCompare,
  openNewTask,
  openTask,
  openTaskFileDiff,
  removeTask,
  startQueuedNow,
  toggleChanges,
  useTasks,
  type TaskChanges,
} from '../../store/agentTasks'
import { BrowserOpenURL } from '../../wails/runtime'
import { NewTaskForm } from './NewTaskForm'
import { Icon } from '../ui/Icon'
import { TaskCI, TaskCIFix, TaskReviewComments } from './TaskCI'
import { CompareView } from './CompareView'

const basename = (p: string) => p.slice(p.lastIndexOf('/') + 1)

/** One row per agent task: state, branch, changes and actions; the tasks of a group stand together. */
export function TasksPanel() {
  const { tasks, changes, expanded, busy, error, newTaskOpen, compare } = useTasks()
  useEffect(() => followTasks(), [])

  return (
    <div className="panel tasks-panel">
      <div className="panel-header">
        <span className="panel-title">Agent Tasks</span>
        <button className="icon" title="New Agent Task…" onClick={openNewTask}>＋</button>
      </div>
      <div className="tree scroll">
        {error && <div className="placeholder error">{error}</div>}
        {groupRows(tasks).map((row) => {
          const labels = row.group ? attemptLabels(row.tasks) : {}
          const rows = row.tasks.map((t) => (
            <TaskRow key={t.id} t={t} changes={changes[t.id]} open={!!expanded[t.id]} busy={!!busy[t.id]} label={labels[t.id]} />
          ))
          if (!row.group) return rows
          return (
            <div key={row.group} className="task-group">
              <div className="task-group-head" title={row.tasks[0].prompt}>
                <span className="file-name">{row.tasks[0].title}</span>
                <span className="muted">{groupSummary(row.tasks)}</span>
                <button className="link" onClick={() => void openCompare(row.group)} title="Compare what each task changed, file by file">
                  Compare
                </button>
              </div>
              {rows}
            </div>
          )
        })}
        {!error && tasks.length === 0 && (
          <div className="placeholder muted">
            No agent tasks. <button className="link" onClick={openNewTask}>New Agent Task…</button> gives an agent its own branch and
            worktree, so several can work in parallel.
          </div>
        )}
      </div>
      {newTaskOpen && <NewTaskForm />}
      {compare && <CompareView />}
    </div>
  )
}

interface RowProps {
  t: TaskView
  changes: TaskChanges | undefined
  open: boolean
  busy: boolean
  /** In a group: the agent (and attempt) instead of the shared title. */
  label?: string
}

function TaskRow({ t, changes, open, busy, label }: RowProps) {
  const s = stateInfo(t.state)
  const a = taskActions(t)
  const status = t.stale ? 'worktree gone' : t.merged ? 'merged' : t.state === 'queued' ? '' : formatChanges(changes)
  const usage = formatTaskUsage(t)
  // In a group, Merge and Create PR choose the winner: they offer to remove the others after.
  const keep = !!label
  return (
    <div className="task">
      <div className="tree-row static task-row" title={`${t.prompt}\n\n${t.worktree}`} onClick={() => a.diff && toggleChanges(t)}>
        <span className="twisty">{a.diff ? (open ? '▾' : '▸') : ' '}</span>
        <span className={`task-state ${t.stale ? 'stale' : s.state}`} title={t.stale ? 'The worktree folder is gone' : t.error || s.hint}>
          {t.stale ? 'stale' : s.label}
        </span>
        <span className="file-name">{label ?? t.title}</span>
      </div>
      <div className="task-meta muted">
        <span title={`${t.branch} from ${t.base} · ${t.agent}`}><Icon name="branch" size={12} /> {t.branch}</span>
        <span>{status}</span>
        {t.ahead > 0 && !t.merged && <span title={`Commits not in ${t.base}`}>↑{t.ahead}</span>}
        {usage && <span title="Tokens (input and output) and cost of the task's chats so far">{usage}</span>}
        <TaskCI t={t} />
      </div>
      {t.state === 'failed' && t.error && <div className="task-error">{t.error}</div>}
      <div className="task-actions">
        {a.startNow && <button className="link" disabled={busy} onClick={() => void startQueuedNow(t)} title="Start this task now, although the limit of working tasks is reached">Start now</button>}
        {a.open && <button className="link" disabled={busy} onClick={() => void openTask(t)} title="Open the folder and the agent chat">Open</button>}
        {a.diff && <button className="link" onClick={() => toggleChanges(t)}>{open ? 'Hide diff' : 'Show diff'}</button>}
        {a.askAgent && <button className="link" onClick={() => void askAgentAboutTask(t)} title="Put the error in the task's chat for the agent">✦ Ask agent</button>}
        {a.merge && <button className="link" disabled={busy} onClick={() => void (keep ? keepTask(t, 'merge') : mergeTask(t))} title={`Merge into ${t.base} where it is checked out${keep ? ', then offer to remove the other tasks of the comparison' : ''}`}>{keep ? 'Keep: Merge' : 'Merge'}</button>}
        {a.pr && <button className="link" disabled={busy} onClick={() => void (keep ? keepTask(t, 'pr') : createTaskPullRequest(t))} title={`Push the branch and create a pull request on GitHub (asks first)${keep ? ', then offer to remove the other tasks of the comparison' : ''}`}>{keep ? 'Keep: PR' : 'Create PR'}</button>}
        {t.pullRequest && <button className="link" onClick={() => BrowserOpenURL(t.pullRequest)} title={t.pullRequest}>PR ↗</button>}
        <TaskCIFix t={t} />
        <TaskReviewComments t={t} />
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
