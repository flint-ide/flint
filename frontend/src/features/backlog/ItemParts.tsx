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
  reposText,
  statusLabel,
  taskPill,
  taskRepo,
  type BacklogData,
  type ItemInfo,
  type TaskInfo,
} from './backlog'
import { delegateItem, itemKey, openItemFile, openItemTask, setStatus, useBacklog } from './store'

/** Title, priority and the failed marker. */
export function ItemTitle({ item }: { item: ItemInfo }) {
  return (
    <>
      <span className="file-name" title={item.body ? `${item.title}\n\n${item.body}` : item.title}>{item.title}</span>
      {item.failed && <span className="task-state failed" title="An agent task of this item failed: the item stays in doing">failed</span>}
      <span className={`backlog-prio ${item.priority}`} title={`${item.priority} priority`}>{item.priority}</span>
    </>
  )
}

/** The agent (with its engine) and the repositories of an item. */
export function ItemMeta({ item, backlog }: { item: ItemInfo; backlog: BacklogData }) {
  const defined = useAgentDefs((s) => s.defined)
  const agent = agentText(item, backlog.defaultAgent, (n) => agentLabel(n, defined))
  return (
    <div className="backlog-meta muted">
      <span title={item.agentProblem || `Runs with ${agent}`} className={item.agentProblem ? 'error' : ''}>✦ {agent}</span>
      <span title="The repositories Delegate starts an agent task in">{reposText(item)}</span>
    </div>
  )
}

/** One line per agent task of an item: its state, repository, Open and the pull request. */
export function ItemTasks({ tasks }: { tasks: TaskInfo[] }) {
  if (tasks.length === 0) return null
  return (
    <div className="backlog-tasks">
      {tasks.map((t) => {
        const pill = taskPill(t)
        return (
          <div key={t.id} className="backlog-task">
            <span className={`task-state ${pill.cls}`} title={t.gone ? `${pill.hint} (the task was removed)` : pill.hint}>{pill.label}</span>
            <span className="backlog-task-repo muted" title={t.branch ? `${t.title}\n${t.branch}` : taskRepo(t)}>{taskRepo(t)}</span>
            {canOpenTask(t) && <button className="link" onClick={() => void openItemTask(t)} title="Open the folder and the agent chat">Open</button>}
            {t.pullRequest && <button className="link" onClick={() => BrowserOpenURL(t.pullRequest)} title={t.pullRequest}>PR ↗</button>}
          </div>
        )
      })}
    </div>
  )
}

/** Delegate, Open and the status menu: the actions of a row and of a card. */
export function ItemActions({ backlog, item }: { backlog: BacklogData; item: ItemInfo }) {
  const busy = useBacklog((s) => !!s.busy[itemKey(backlog.path, item.id)])
  const [issue] = delegateIssues(item, backlog.repositories, activeRoot())
  const hint = issue
    ? `${issue.message}${issue.fix === 'locate' ? ' Delegate asks for the folder.' : issue.fix === 'attach' ? ' Delegate adds it.' : issue.fix === 'trust' ? ' Delegate asks first.' : ''}`
    : 'Start one agent task per repository of the item'
  return (
    <div className="task-actions backlog-actions">
      <button className="link" disabled={busy} onClick={() => void delegateItem(backlog.path, item.id)} title={hint}>Delegate</button>
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
