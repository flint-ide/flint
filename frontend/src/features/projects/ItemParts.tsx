import type { ComponentProps } from 'react'
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
import { dependencyNotes, priorityColor } from './cardParts'
import { addContext, cancelStart, delegateItem, itemKey, openContext, openItemFile, openItemTask, renameOldConfig, setStatus, useProjects } from './store'
import { Badge, Button, ListGroup, ListItem, TaskCard } from '@flint-ide/ui'
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

type ItemCardProps = Omit<ComponentProps<typeof TaskCard>, 'label'> & { project: ProjectData; item: ItemInfo }

/** One work item, in the Projects panel ('row') and on the board ('card'): title, markers, agent and repositories, dependencies, tasks, actions. */
export function ItemCard({ project, item, ...rest }: ItemCardProps) {
  const defined = useAgentDefs((s) => s.defined)
  const agent = agentText(item, project.defaultAgent, (n) => agentLabel(n, defined))
  const together = togetherMarker(item)
  const dep = dependencyMarker(item, project.items)
  return (
    <TaskCard
      {...rest}
      label={<span title={item.body ? `${item.title}\n\n${item.body}` : item.title}>{item.title}</span>}
      badges={
        (item.failed || dep) && (
          <>
            {item.failed && <TaskStateBadge state="failed" title="An agent task of this item failed: the item stays in doing">failed</TaskStateBadge>}
            {dep && <MarkerBadge tone={dep.cls} title={dep.hint}>{dep.label}</MarkerBadge>}
          </>
        )
      }
      trailing={
        <Badge size="sm" color={priorityColor(item.priority)} badgeStyle="light" title={`${item.priority} priority`}>
          {item.priority}
        </Badge>
      }
      meta={[
        <span title={item.agentProblem || `Runs with ${agent}`} className={item.agentProblem ? 'fui-danger' : undefined}>✦ {agent}</span>,
        <span title={item.together ? 'The repositories one agent works in together' : 'The repositories Delegate starts an agent task in'}>{reposText(item)}</span>,
        together && <MarkerBadge tone={together.cls} title={together.hint}>{together.label}</MarkerBadge>,
      ]}
      notes={dependencyNotes(dependencyLines(item, project.items))}
      actions={<ItemActions project={project} item={item} />}
    >
      {item.tasks.length > 0 && <ItemTasks tasks={item.tasks} />}
    </TaskCard>
  )
}

/** One line per agent task of an item: its state, repository, Open and the pull request; a joint group stands together. */
function ItemTasks({ tasks }: { tasks: TaskInfo[] }) {
  return groupTasks(tasks).map((row) =>
    row.joint ? (
      <ListGroup key={`joint:${row.joint}`} meta={`one agent · ${row.tasks.length} repositories`} tooltip={jointHint(row.tasks)}>
        {row.tasks.map((t) => (
          <TaskLine key={t.id} t={t} all={tasks} />
        ))}
      </ListGroup>
    ) : (
      <TaskLine key={row.tasks[0].id} t={row.tasks[0]} all={tasks} />
    ),
  )
}

function TaskLine({ t, all }: { t: TaskInfo; all: TaskInfo[] }) {
  const pill = taskPill(t)
  const open = canOpenTask(t, all)
  return (
    <ListItem
      compact
      leading={<TaskStateBadge state={pill.cls} title={t.gone ? `${pill.hint} (the task was removed)` : pill.hint}>{pill.label}</TaskStateBadge>}
      label={<span className="fui-muted" title={t.branch ? `${t.title}\n${t.branch}` : taskRepo(t)}>{taskRepo(t)}</span>}
      badges={t.joint && t.lead && <MarkerBadge title="The lead of the group: the one chat runs in this task's worktree">lead</MarkerBadge>}
      showActions="always"
      actions={
        (open || t.pullRequest) && (
          <>
            {open && (
              <Button variant="ghost" size="sm" onClick={() => void openItemTask(t)} title={t.joint && !t.lead ? "Open the group's chat (in the lead task's folder)" : 'Open the folder and the agent chat'}>Open</Button>
            )}
            {t.pullRequest && <Button variant="ghost" size="sm" onClick={() => BrowserOpenURL(t.pullRequest)} title={t.pullRequest}>PR ↗</Button>}
          </>
        )
      }
    />
  )
}

/** Delegate, Open and the status menu: the actions of a row and of a card. */
function ItemActions({ project, item }: { project: ProjectData; item: ItemInfo }) {
  const busy = useProjects((s) => !!s.busy[itemKey(project.path, item.id)])
  const [issue] = delegateIssues(item, project.repositories, activeRoot())
  if (project.oldFormat) {
    return <Button variant="ghost" size="sm" onClick={() => openItemFile(item)} title={item.path}>Open</Button>
  }
  const hint = issue
    ? `${issue.message}${issue.fix === 'locate' ? ' Delegate asks for the folder.' : issue.fix === 'attach' ? ' Delegate adds it.' : issue.fix === 'trust' ? ' Delegate asks first.' : ''}`
    : item.together
      ? 'Start one agent that works in all of the repositories of the item, in one chat'
      : item.waiting
        ? 'The item waits to start by itself: Delegate it now if its dependencies are done'
        : 'Start one agent task per repository of the item'
  return (
    <>
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
    </>
  )
}
