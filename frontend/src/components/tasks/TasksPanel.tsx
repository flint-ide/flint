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
import { MarkerBadge, TaskStateBadge } from './badges'
import { TaskCIFix, TaskReviewComments, useTaskCIBadge } from './TaskCI'
import { CompareView } from './CompareView'
import { Button, IconButton, ListGroup, PanelHeader, TaskCard, TreeRow } from '@flint-ide/ui'
import { Plus } from '@flint-ide/ui/icons'

const basename = (p: string) => p.slice(p.lastIndexOf('/') + 1)

/** One row per agent task: state, branch, changes and actions; the tasks of a group stand together. */
export function TasksPanel() {
  const { tasks, changes, expanded, busy, error, newTaskOpen, compare } = useTasks()
  useEffect(() => followTasks(), [])

  return (
    <div className="panel tasks-panel">
      <PanelHeader
        title="Agent Tasks"
        actions={
          <IconButton variant="ghost" size="sm" aria-label="New Agent Task" title="New Agent Task…" onClick={openNewTask}>
            <Plus />
          </IconButton>
        }
      />
      <div className="tree scroll">
        {error && <div className="placeholder error">{error}</div>}
        {groupRows(tasks).map((row) => {
          const labels = row.group ? attemptLabels(row.tasks) : {}
          const rows = row.tasks.map((t) => (
            <TaskRow key={t.id} t={t} changes={changes[t.id]} open={!!expanded[t.id]} busy={!!busy[t.id]} label={labels[t.id]} heading={row.joint ? basename(t.checkout) : undefined} />
          ))
          if (row.joint) {
            const lead = row.tasks[0]
            return (
              <ListGroup
                key={`joint:${row.joint}`}
                stacked
                label={lead.title}
                meta={`one agent · ${row.tasks.length} repositories · ${groupSummary(row.tasks)}`}
                tooltip={`${lead.prompt}\n\nOne agent works in the worktrees of all ${row.tasks.length} tasks; its chat is in the lead's.`}
              >
                {rows}
              </ListGroup>
            )
          }
          if (!row.group) return rows
          return (
            <ListGroup
              key={row.group}
              label={row.tasks[0].title}
              meta={groupSummary(row.tasks)}
              tooltip={row.tasks[0].prompt}
              actions={
                <Button variant="ghost" size="sm" onClick={() => void openCompare(row.group)} title="Compare what each task changed, file by file">
                  Compare
                </Button>
              }
            >
              {rows}
            </ListGroup>
          )
        })}
        {!error && tasks.length === 0 && (
          <div className="placeholder muted">
            No agent tasks. <Button variant="link" size="sm" onClick={openNewTask}>New Agent Task…</Button> gives an agent its own branch and
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
  /** In a joint group: the repository's folder, as the tasks of the group share one title. */
  heading?: string
}

function TaskRow({ t, changes, open, busy, label, heading }: RowProps) {
  const s = stateInfo(t.state)
  const a = taskActions(t)
  const ci = useTaskCIBadge(t)
  const status = t.stale ? 'worktree gone' : t.merged ? 'merged' : t.state === 'queued' ? '' : formatChanges(changes)
  const usage = formatTaskUsage(t)
  return (
    <TaskCard
      tooltip={`${t.prompt}\n\n${t.worktree}`}
      onOpen={a.diff ? () => toggleChanges(t) : undefined}
      twisty={a.diff ? (open ? 'open' : 'closed') : 'none'}
      status={
        <TaskStateBadge state={t.stale ? 'stale' : s.state} title={t.stale ? 'The worktree folder is gone' : t.error || t.blocked || s.hint}>
          {t.stale ? 'stale' : s.label}
        </TaskStateBadge>
      }
      label={label ?? heading ?? t.title}
      badges={t.joint && <JointMarker lead={t.lead} />}
      ci={ci}
      meta={[
        <span className="fui-mono" title={`${t.branch} from ${t.base} · ${t.agent}`}><Icon name="branch" size={12} /> {t.branch}</span>,
        status,
        t.ahead > 0 && !t.merged && <span title={`Commits not in ${t.base}`}>↑{t.ahead}</span>,
        usage && <span title="Tokens (input and output) and cost of the task's chats so far">{usage}</span>,
      ]}
      notes={[
        t.state === 'failed' && !!t.error && { text: t.error, tone: 'danger' },
        t.state === 'queued' && !!t.blocked && { text: t.blocked, tone: 'danger' },
      ]}
      actions={<TaskActions t={t} open={open} busy={busy} keep={!!label} />}
    >
      {open &&
        changes?.files?.map((f) => (
          <TreeRow
            key={f.path}
            depth={2}
            title={f.origPath ? `${f.origPath} → ${f.path}` : f.path}
            onClick={() => openTaskFileDiff(t, f)}
            label={<span className={f.untracked ? 'change-untracked' : undefined}>{basename(f.path)}</span>}
            file
            hint={f.path.includes('/') ? f.path.slice(0, f.path.lastIndexOf('/')) : ''}
            badge={<span className="fui-mono fui-muted">{f.binary ? 'bin' : `+${f.added} −${f.deleted}`}</span>}
          />
        ))}
      {open && changes?.truncated && <TreeRow depth={2} label={<span className="fui-muted">…and {changes.count - changes.files.length} more files</span>} />}
    </TaskCard>
  )
}

function JointMarker({ lead }: { lead: boolean }) {
  return lead ? (
    <MarkerBadge title="One agent works in the worktrees of this task's group; its chat is in this task's worktree">lead</MarkerBadge>
  ) : (
    <MarkerBadge title="No chat of its own: the group's one agent works here from the lead's chat. Open shows that chat">lead's chat</MarkerBadge>
  )
}

/** The task's buttons. `keep`: in a group, Merge and Create PR choose the winner and offer to remove the others after. */
function TaskActions({ t, open, busy, keep }: { t: TaskView; open: boolean; busy: boolean; keep: boolean }) {
  const a = taskActions(t)
  return (
    <>
      {a.startNow && <Button variant="ghost" size="sm" disabled={busy} onClick={() => void startQueuedNow(t)} title="Start this task now, although the limit of working tasks is reached">Start now</Button>}
      {a.open && <Button variant="ghost" size="sm" disabled={busy} onClick={() => void openTask(t)} title={t.joint && !t.lead ? "Open the group's chat (in the lead task's folder)" : 'Open the folder and the agent chat'}>Open</Button>}
      {a.diff && <Button variant="ghost" size="sm" onClick={() => toggleChanges(t)}>{open ? 'Hide diff' : 'Show diff'}</Button>}
      {a.askAgent && <Button variant="ghost" size="sm" onClick={() => void askAgentAboutTask(t)} title="Put the error in the task's chat for the agent">✦ Ask agent</Button>}
      {a.merge && <Button variant="ghost" size="sm" disabled={busy} onClick={() => void (keep ? keepTask(t, 'merge') : mergeTask(t))} title={`Merge into ${t.base} where it is checked out${keep ? ', then offer to remove the other tasks of the comparison' : ''}`}>{keep ? 'Keep: Merge' : 'Merge'}</Button>}
      {a.pr && <Button variant="ghost" size="sm" disabled={busy} onClick={() => void (keep ? keepTask(t, 'pr') : createTaskPullRequest(t))} title={`Push the branch and create a pull request on GitHub (asks first)${keep ? ', then offer to remove the other tasks of the comparison' : ''}`}>{keep ? 'Keep: PR' : 'Create PR'}</Button>}
      {t.pullRequest && <Button variant="ghost" size="sm" onClick={() => BrowserOpenURL(t.pullRequest)} title={t.pullRequest}>PR ↗</Button>}
      <TaskCIFix t={t} />
      <TaskReviewComments t={t} />
      <span className="fui-grow" />
      <Button variant="ghost" size="sm" disabled={busy} title="Remove the task" onClick={() => void removeTask(t)}>
        Remove
      </Button>
    </>
  )
}
