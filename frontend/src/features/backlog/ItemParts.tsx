import { agentLabel } from '../../lib/agentDefs'
import { useAgentDefs } from '../../store/agentDefs'
import { activeRoot } from '../../store/activeRoot'
import { BrowserOpenURL } from '../../wails/runtime'
import { Dropdown } from '../../components/ui/Dropdown'
import {
  agentText,
  BOARD_STATUSES,
  canOpenTask,
  delegateIssues,
  dependencyLines,
  dependencyMarker,
  groupTasks,
  jointHint,
  reposText,
  statusLabel,
  taskPill,
  taskRepo,
  togetherMarker,
  type BacklogData,
  type ItemInfo,
  type TaskInfo,
} from './backlog'
import { cancelStart, delegateItem, itemKey, openItemFile, openItemTask, setStatus, useBacklog } from './store'

/** Title, the failed and dependency markers, and the priority. */
export function ItemTitle({ item, items }: { item: ItemInfo; items: readonly ItemInfo[] }) {
  const dep = dependencyMarker(item, items)
  return (
    <>
      <span className="file-name" title={item.body ? `${item.title}\n\n${item.body}` : item.title}>{item.title}</span>
      {item.failed && <span className="task-state failed" title="An agent task of this item failed: the item stays in doing">failed</span>}
      {dep && <span className={`backlog-marker ${dep.cls}`} title={dep.hint}>{dep.label}</span>}
      <span className={`backlog-prio ${item.priority}`} title={`${item.priority} priority`}>{item.priority}</span>
    </>
  )
}

/** The agent (with its engine) and the repositories of an item. */
export function ItemMeta({ item, backlog }: { item: ItemInfo; backlog: BacklogData }) {
  const defined = useAgentDefs((s) => s.defined)
  const agent = agentText(item, backlog.defaultAgent, (n) => agentLabel(n, defined))
  const together = togetherMarker(item)
  return (
    <div className="backlog-meta muted">
      <span title={item.agentProblem || `Runs with ${agent}`} className={item.agentProblem ? 'error' : ''}>✦ {agent}</span>
      <span title={item.together ? 'The repositories one agent works in together' : 'The repositories Delegate starts an agent task in'}>{reposText(item)}</span>
      {together && <span className={`backlog-marker ${together.cls}`} title={together.hint}>{together.label}</span>}
    </div>
  )
}

/** What an item waits for, its dependency problem and its waiting start (with the reason it did not start). */
export function ItemDependencies({ item, items }: { item: ItemInfo; items: readonly ItemInfo[] }) {
  const lines = dependencyLines(item, items)
  if (lines.length === 0) return null
  return (
    <div className="backlog-deps">
      {lines.map((l) => (
        <span key={l.kind} className={`backlog-dep ${l.cls}`} title={l.text}>{l.text}</span>
      ))}
    </div>
  )
}

/** One line per agent task of an item: its state, repository, Open and the pull request; a joint group stands together. */
export function ItemTasks({ tasks }: { tasks: TaskInfo[] }) {
  if (tasks.length === 0) return null
  return (
    <div className="backlog-tasks">
      {groupTasks(tasks).map((row) =>
        row.joint ? (
          <div key={`joint:${row.joint}`} className="backlog-joint">
            <div className="backlog-joint-head muted" title={jointHint(row.tasks)}>one agent · {row.tasks.length} repositories</div>
            {row.tasks.map((t) => (
              <TaskLine key={t.id} t={t} all={tasks} />
            ))}
          </div>
        ) : (
          <TaskLine key={row.tasks[0].id} t={row.tasks[0]} all={tasks} />
        ),
      )}
    </div>
  )
}

function TaskLine({ t, all }: { t: TaskInfo; all: TaskInfo[] }) {
  const pill = taskPill(t)
  return (
    <div className="backlog-task">
      <span className={`task-state ${pill.cls}`} title={t.gone ? `${pill.hint} (the task was removed)` : pill.hint}>{pill.label}</span>
      <span className="backlog-task-repo muted" title={t.branch ? `${t.title}\n${t.branch}` : taskRepo(t)}>{taskRepo(t)}</span>
      {t.joint && t.lead && <span className="backlog-marker" title="The lead of the group: the one chat runs in this task's worktree">lead</span>}
      {canOpenTask(t, all) && (
        <button className="link" onClick={() => void openItemTask(t)} title={t.joint && !t.lead ? "Open the group's chat (in the lead task's folder)" : 'Open the folder and the agent chat'}>Open</button>
      )}
      {t.pullRequest && <button className="link" onClick={() => BrowserOpenURL(t.pullRequest)} title={t.pullRequest}>PR ↗</button>}
    </div>
  )
}

/** Delegate, Open and the status menu: the actions of a row and of a card. */
export function ItemActions({ backlog, item }: { backlog: BacklogData; item: ItemInfo }) {
  const busy = useBacklog((s) => !!s.busy[itemKey(backlog.path, item.id)])
  const [issue] = delegateIssues(item, backlog.repositories, activeRoot())
  const hint = issue
    ? `${issue.message}${issue.fix === 'locate' ? ' Delegate asks for the folder.' : issue.fix === 'attach' ? ' Delegate adds it.' : issue.fix === 'trust' ? ' Delegate asks first.' : ''}`
    : item.together
      ? 'Start one agent that works in all of the repositories of the item, in one chat'
      : item.waiting
        ? 'The item waits to start by itself: Delegate it now if its dependencies are done'
        : 'Start one agent task per repository of the item'
  return (
    <div className="task-actions backlog-actions">
      <button className="link" disabled={busy} onClick={() => void delegateItem(backlog.path, item.id)} title={hint}>Delegate</button>
      {item.waiting && (
        <button className="link" disabled={busy} onClick={() => void cancelStart(backlog.path, item.id)} title="Don't start this item by itself when its dependencies are done">Cancel start</button>
      )}
      <button className="link" onClick={() => openItemFile(item)} title={item.path}>Open</button>
      <Dropdown
        minWidth={140}
        items={BOARD_STATUSES.map((s) => ({
          label: statusLabel(s),
          checked: s === item.status,
          disabled: busy,
          onSelect: () => item.status !== s && void setStatus(backlog.path, item.id, s),
        }))}
        trigger={(t) => (
          <button {...t} className="link" title="Set the item's status">Status ⌄</button>
        )}
      />
    </div>
  )
}
