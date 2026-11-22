import { agentLabel } from '../../lib/agentDefs'
import { useAgentDefs } from '../../store/agentDefs'
import { activeRoot } from '../../store/activeRoot'
import { BrowserOpenURL } from '../../wails/runtime'
import { Dropdown } from '../../components/ui/Dropdown'
import {
  agentText,
  BOARD_STATUSES,
  contextLine,
  canOpenTask,
  delegateIssues,
  dependencyLines,
  dependencyMarker,
  groupTasks,
  jointHint,
  OLD_FORMAT_NOTE,
  RENAME_ACTION,
  reposText,
  statusLabel,
  taskPill,
  taskRepo,
  togetherMarker,
  type ProjectData,
  type ItemInfo,
  type TaskInfo,
} from './projects'
import { addContext, cancelStart, delegateItem, itemKey, openContext, openItemFile, openItemTask, renameOldConfig, setStatus, useProjects } from './store'
import { Button } from '@flint-ide/ui'
import { MarkerBadge, TaskStateBadge } from '../../components/tasks/badges'

/** The note on a project in the Backlog preview's format, with its one action (the panel and the board). */
export function OldFormatNote({ folder }: { folder: string }) {
  return (
    <div className="project-untrusted project-old-format" role="status">
      <MarkerBadge tone="warn">Old format</MarkerBadge>
      <span className="muted">{OLD_FORMAT_NOTE}</span>
      <Button variant="ghost" size="sm" onClick={() => void renameOldConfig(folder)} title="Flint asks first; the file's content stays as it is">
        {RENAME_ACTION}…
      </Button>
    </div>
  )
}

/** The project's context file: Open when it exists, Add Project Context… when it does not (panel and board). */
export function ProjectContext({ project }: { project: ProjectData }) {
  const line = contextLine(project.context, project.trusted && !project.oldFormat)
  return (
    <span className="project-context" title={line.hint}>
      <span className="muted">Context:</span>
      <span className={line.problem ? 'error' : line.action === 'open' ? '' : 'muted'}>{line.text}</span>
      {line.action === 'open' && (
        <Button variant="ghost" size="sm" onClick={() => openContext(project)} title={`Open ${project.context.path}`}>Open</Button>
      )}
      {line.action === 'add' && (
        <Button variant="ghost" size="sm" onClick={() => void addContext(project.path)} title="Write PROJECT.md from a template and open it">Add Project Context…</Button>
      )}
    </span>
  )
}

/** Title, the failed and dependency markers, and the priority. */
export function ItemTitle({ item, items }: { item: ItemInfo; items: readonly ItemInfo[] }) {
  const dep = dependencyMarker(item, items)
  return (
    <>
      <span className="file-name" title={item.body ? `${item.title}\n\n${item.body}` : item.title}>{item.title}</span>
      {item.failed && <TaskStateBadge state="failed" title="An agent task of this item failed: the item stays in doing">failed</TaskStateBadge>}
      {dep && <MarkerBadge tone={dep.cls} title={dep.hint}>{dep.label}</MarkerBadge>}
      <span className={`project-prio ${item.priority}`} title={`${item.priority} priority`}>{item.priority}</span>
    </>
  )
}

/** The agent (with its engine) and the repositories of an item. */
export function ItemMeta({ item, project }: { item: ItemInfo; project: ProjectData }) {
  const defined = useAgentDefs((s) => s.defined)
  const agent = agentText(item, project.defaultAgent, (n) => agentLabel(n, defined))
  const together = togetherMarker(item)
  return (
    <div className="project-meta muted">
      <span title={item.agentProblem || `Runs with ${agent}`} className={item.agentProblem ? 'error' : ''}>✦ {agent}</span>
      <span title={item.together ? 'The repositories one agent works in together' : 'The repositories Delegate starts an agent task in'}>{reposText(item)}</span>
      {together && <MarkerBadge tone={together.cls} title={together.hint}>{together.label}</MarkerBadge>}
    </div>
  )
}

/** What an item waits for, its dependency problem and its waiting start (with the reason it did not start). */
export function ItemDependencies({ item, items }: { item: ItemInfo; items: readonly ItemInfo[] }) {
  const lines = dependencyLines(item, items)
  if (lines.length === 0) return null
  return (
    <div className="project-deps">
      {lines.map((l) => (
        <span key={l.kind} className={`project-dep ${l.cls}`} title={l.text}>{l.text}</span>
      ))}
    </div>
  )
}

/** One line per agent task of an item: its state, repository, Open and the pull request; a joint group stands together. */
export function ItemTasks({ tasks }: { tasks: TaskInfo[] }) {
  if (tasks.length === 0) return null
  return (
    <div className="project-tasks">
      {groupTasks(tasks).map((row) =>
        row.joint ? (
          <div key={`joint:${row.joint}`} className="project-joint">
            <div className="project-joint-head muted" title={jointHint(row.tasks)}>one agent · {row.tasks.length} repositories</div>
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
    <div className="project-task">
      <TaskStateBadge state={pill.cls} title={t.gone ? `${pill.hint} (the task was removed)` : pill.hint}>{pill.label}</TaskStateBadge>
      <span className="project-task-repo muted" title={t.branch ? `${t.title}\n${t.branch}` : taskRepo(t)}>{taskRepo(t)}</span>
      {t.joint && t.lead && <MarkerBadge title="The lead of the group: the one chat runs in this task's worktree">lead</MarkerBadge>}
      {canOpenTask(t, all) && (
        <Button variant="ghost" size="sm" onClick={() => void openItemTask(t)} title={t.joint && !t.lead ? "Open the group's chat (in the lead task's folder)" : 'Open the folder and the agent chat'}>Open</Button>
      )}
      {t.pullRequest && <Button variant="ghost" size="sm" onClick={() => BrowserOpenURL(t.pullRequest)} title={t.pullRequest}>PR ↗</Button>}
    </div>
  )
}

/** Delegate, Open and the status menu: the actions of a row and of a card. */
export function ItemActions({ project, item }: { project: ProjectData; item: ItemInfo }) {
  const busy = useProjects((s) => !!s.busy[itemKey(project.path, item.id)])
  const [issue] = delegateIssues(item, project.repositories, activeRoot())
  if (project.oldFormat) {
    return (
      <div className="task-actions project-actions">
        <Button variant="ghost" size="sm" onClick={() => openItemFile(item)} title={item.path}>Open</Button>
      </div>
    )
  }
  const hint = issue
    ? `${issue.message}${issue.fix === 'locate' ? ' Delegate asks for the folder.' : issue.fix === 'attach' ? ' Delegate adds it.' : issue.fix === 'trust' ? ' Delegate asks first.' : ''}`
    : item.together
      ? 'Start one agent that works in all of the repositories of the item, in one chat'
      : item.waiting
        ? 'The item waits to start by itself: Delegate it now if its dependencies are done'
        : 'Start one agent task per repository of the item'
  return (
    <div className="task-actions project-actions">
      <Button variant="ghost" size="sm" disabled={busy} onClick={() => void delegateItem(project.path, item.id)} title={hint}>Delegate</Button>
      {item.waiting && (
        <Button variant="ghost" size="sm" disabled={busy} onClick={() => void cancelStart(project.path, item.id)} title="Don't start this item by itself when its dependencies are done">Cancel start</Button>
      )}
      <Button variant="ghost" size="sm" onClick={() => openItemFile(item)} title={item.path}>Open</Button>
      <Dropdown
        minWidth={140}
        items={BOARD_STATUSES.map((s) => ({
          label: statusLabel(s),
          checked: s === item.status,
          disabled: busy,
          onSelect: () => item.status !== s && void setStatus(project.path, item.id, s),
        }))}
        trigger={(t) => (
          <Button variant="ghost" size="sm" {...t} title="Set the item's status">Status ⌄</Button>
        )}
      />
    </div>
  )
}
