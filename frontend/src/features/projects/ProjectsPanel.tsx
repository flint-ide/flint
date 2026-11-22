import { useEffect, type ReactNode } from 'react'
import { Dropdown } from '../../components/ui/Dropdown'
import { Select } from '../../components/ui/Select'
import { ProjectDialogs } from './Dialogs'
import { ItemActions, ItemDependencies, ItemMeta, ItemTasks, ItemTitle, OldFormatNote, ProjectContext } from './ItemParts'
import {
  allReposResolve,
  canClone,
  canClose,
  cloneTitle,
  groupByStatus,
  OLD_FORMAT_HINT,
  PANEL_STATUSES,
  repoIssue,
  repoMarkers,
  statusLabel,
  type ProjectData,
  type ProjectInfo,
  type CloneState,
  type ProblemInfo,
  type RepoView,
} from './projects'
import {
  askAboutProblem,
  cancelClone,
  closeProject,
  followProjects,
  loadProject,
  newProject,
  openProject,
  openBoard,
  openNewItem,
  openPlan,
  repoActions,
  selectProject,
  toggleGroup,
  trustProject,
  useProjects,
} from './store'
import { Button, IconButton } from '@flint-ide/ui'
import { CountBadge, MarkerBadge } from '../../components/tasks/badges'
import { Ellipsis, Plus, RefreshCw } from '@flint-ide/ui/icons'

/** The Projects panel: the picker, the items by status and the problems of the selected project. */
export function ProjectsPanel() {
  const { infos, selected, data, collapsed, clones } = useProjects()
  useEffect(() => followProjects(() => [useProjects.getState().selected]), [])
  const info = infos?.find((i) => i.path === selected)
  const loaded = data[selected]
  const project = loaded?.project ?? null
  const oldFormat = !!(project?.oldFormat ?? info?.oldFormat)

  return (
    <div className="panel projects-panel">
      <div className="panel-header">
        <span className="panel-title">Projects</span>
        <IconButton variant="ghost" size="sm" aria-label="New Item" title={oldFormat ? OLD_FORMAT_HINT : 'New Item…'} disabled={!info || oldFormat} onClick={() => openNewItem(selected)}>
          <Plus />
        </IconButton>
        <IconButton variant="ghost" size="sm" aria-label="Refresh" title="Refresh" disabled={!info} onClick={() => void loadProject(selected)}>
          <RefreshCw />
        </IconButton>
        <Dropdown
          align="end"
          minWidth={190}
          items={[
            { label: 'Open Project…', onSelect: () => void openProject() },
            { label: 'New Project…', onSelect: () => void newProject() },
            { label: 'Plan with Agent…', disabled: !info || oldFormat, hint: oldFormat ? 'old format' : undefined, onSelect: () => openPlan(selected) },
            { label: 'Show Board', disabled: !info, onSelect: () => info && openBoard(info) },
            { type: 'separator' },
            { label: 'Close Project', disabled: !canClose(info), hint: info?.workspace ? 'workspace folder' : undefined, onSelect: () => info && void closeProject(info) },
          ]}
          trigger={(t) => (
            <IconButton variant="ghost" size="sm" {...t} aria-label="Project actions" title="Open, create or close a project">
              <Ellipsis />
            </IconButton>
          )}
        />
      </div>
      {infos && infos.length > 0 && (
        <div className="project-picker">
          <Select
            block
            size="sm"
            value={selected}
            title={selected}
            options={infos.map((i) => ({
              value: i.path,
              label: i.name,
              hint: i.error || [i.oldFormat ? 'old format' : '', i.trusted ? '' : 'Restricted Mode', i.workspace ? 'workspace folder' : i.path].filter(Boolean).join(' · '),
            }))}
            onChange={selectProject}
          />
        </div>
      )}
      <div className="tree scroll">
        {info?.error && <div className="placeholder error">{info.error}</div>}
        {loaded?.error && !info?.error && <div className="placeholder error">{loaded.error}</div>}
        {info && oldFormat && <OldFormatNote folder={info.path} />}
        {info && !(project?.trusted ?? info.trusted) && <Untrusted info={info} />}
        {project && (
          <div className="project-context-row">
            <ProjectContext project={project} />
          </div>
        )}
        {project && project.repositories.length > 0 && <Repositories project={project} collapsed={collapsed} clone={clones[project.path]} />}
        {project && <Groups project={project} collapsed={collapsed} />}
        {infos?.length === 0 && (
          <div className="placeholder muted">
            <span>
              No project yet. A project is a folder of work items (its backlog, as Markdown files) for one or more repositories. <b>Delegate</b> turns an item
              into agent tasks, one per repository, and the item&apos;s status follows them.
            </span>
            <span>
              <Button variant="link" size="sm" onClick={() => void openProject()}>Open Project…</Button> ·{' '}
              <Button variant="link" size="sm" onClick={() => void newProject()}>New Project…</Button>
            </span>
          </div>
        )}
        {project && !oldFormat && project.items.length === 0 && project.problems.length === 0 && (
          <div className="placeholder muted">
            The backlog is empty. <Button variant="link" size="sm" onClick={() => openNewItem(selected)}>New Item…</Button> writes one to items/;{' '}
            <Button variant="link" size="sm" onClick={() => openPlan(selected)}>Plan with Agent…</Button> has an agent split a goal into items.
          </div>
        )}
        {project && project.problems.length > 0 && <Problems project={project} />}
      </div>
      <ProjectDialogs host="panel" />
    </div>
  )
}

/** A collapsible group header, operable with the mouse and with Enter or Space. */
function GroupHeader({ open, onToggle, children }: { open: boolean; onToggle: () => void; children: ReactNode }) {
  return (
    <div
      className="tree-row static root-row project-group-head"
      role="button"
      tabIndex={0}
      aria-expanded={open}
      onClick={onToggle}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onToggle()
        }
      }}
    >
      <span className="twisty">{open ? '▾' : '▸'}</span>
      {children}
    </div>
  )
}

/** The note on a project whose folder is not trusted: agents don't read it, and Delegate asks first. */
function Untrusted({ info }: { info: ProjectInfo }) {
  return (
    <div className="project-untrusted" role="status">
      <MarkerBadge tone="warn">Restricted Mode</MarkerBadge>
      <span className="muted">
        This project&apos;s folder is not trusted: agents can&apos;t read it with get_project, and Delegate shows you each item before it sends it.
      </span>
      <Button variant="ghost" size="sm" onClick={() => void trustProject(info)} title={info.path}>Trust Folder…</Button>
    </div>
  )
}

/** The repositories of flint-project.json with their checkouts here; open while one can't be used by Delegate. */
function Repositories({ project, collapsed, clone }: { project: ProjectData; collapsed: Record<string, boolean>; clone: CloneState | undefined }) {
  const repos = project.repositories
  // A running clone keeps the group open until the user closes it: its progress is shown here.
  const open = collapsed.repositories === undefined ? !allReposResolve(repos) || !!clone : !collapsed.repositories
  const waiting = repos.filter((r) => repoIssue(r) !== null).length
  return (
    <div className="project-group project-repos">
      <GroupHeader open={open} onToggle={() => toggleGroup('repositories', !open)}>
        <span className="dir-name">Repositories</span>
        <CountBadge title={waiting ? `${waiting} can't be used by Delegate yet` : 'Every repository has a checkout here'}>
          {waiting ? `${waiting}/${repos.length}` : repos.length}
        </CountBadge>
      </GroupHeader>
      {open && clone && <CloneProgress folder={project.path} clone={clone} />}
      {open && repos.map((r) => <RepoRow key={r.name} folder={project.path} repo={r} cloning={!!clone} />)}
    </div>
  )
}

/** The clone that runs for the project: git's latest progress line and Cancel. */
function CloneProgress({ folder, clone }: { folder: string; clone: CloneState }) {
  return (
    <div className="project-clone" role="status">
      <span className="project-clone-title" title={clone.destination}>{cloneTitle(clone)}</span>
      {clone.progress && <span className="project-clone-line muted" title={clone.progress}>{clone.progress}</span>}
      <div className="task-actions">
        <Button variant="ghost" size="sm" onClick={() => void cancelClone(folder)} title="Stop git; what it made is removed">Cancel</Button>
      </div>
    </div>
  )
}

function RepoRow({ folder, repo, cloning }: { folder: string; repo: RepoView; cloning: boolean }) {
  const issue = repoIssue(repo)
  return (
    <div className="task project-item project-repo">
      <div className="tree-row static task-row" title={repo.url}>
        <span className="file-name">{repo.name}</span>
        {repoMarkers(repo).map((m) => (
          <MarkerBadge key={m.label} tone={m.cls} title={m.hint}>{m.label}</MarkerBadge>
        ))}
      </div>
      <div className="project-meta muted">
        <span title={repo.url}>{repo.url}</span>
      </div>
      <div className="project-meta muted">
        <span title={repo.folder || undefined} className={repo.folder ? '' : 'error'}>{repo.folder || 'no checkout on this computer'}</span>
      </div>
      <div className="task-actions project-actions">
        <Button variant="ghost" size="sm" onClick={() => repoActions.locate(folder, repo)} title="Choose the folder of your checkout of this repository">Locate…</Button>
        {canClone(repo) && (
          <Button
            variant="ghost"
            size="sm"
            disabled={cloning}
            onClick={() => repoActions.clone(folder, repo)}
            title={cloning ? 'A clone of this project is running' : `Clone ${repo.url} into a folder you choose (asks again before it runs)`}
          >
            Clone…
          </Button>
        )}
        {issue?.fix === 'attach' && (
          <Button variant="ghost" size="sm" onClick={() => repoActions.attach(folder, repo)} title={`Add ${repo.folder} to the workspace`}>Add to Workspace</Button>
        )}
        {issue?.fix === 'trust' && (
          <Button variant="ghost" size="sm" onClick={() => repoActions.trust(folder, repo)} title={`Trust ${repo.folder}`}>Trust Folder…</Button>
        )}
      </div>
    </div>
  )
}

function Groups({ project, collapsed }: { project: ProjectData; collapsed: Record<string, boolean> }) {
  const groups = groupByStatus(project.items)
  return (
    <>
      {PANEL_STATUSES.map((status) => {
        const items = groups[status]
        if (items.length === 0) return null
        const open = !collapsed[status]
        return (
          <div key={status} className="project-group">
            <GroupHeader open={open} onToggle={() => toggleGroup(status, !open)}>
              <span className="dir-name">{statusLabel(status)}</span>
              <CountBadge>{items.length}</CountBadge>
            </GroupHeader>
            {open &&
              items.map((item) => (
                <div key={item.id} className="task project-item">
                  <div className="tree-row static task-row" title={item.path}>
                    <ItemTitle item={item} items={project.items} />
                  </div>
                  <ItemMeta item={item} project={project} />
                  <ItemDependencies item={item} items={project.items} />
                  <ItemTasks tasks={item.tasks} />
                  <ItemActions project={project} item={item} />
                </div>
              ))}
          </div>
        )
      })}
    </>
  )
}

/** Files Flint could not use, each with the hand-off to the agent. */
function Problems({ project }: { project: ProjectData }) {
  return (
    <div className="project-problems">
      <div className="tree-row static root-row">
        <span className="dir-name">Problems</span>
        <CountBadge>{project.problems.length}</CountBadge>
      </div>
      {project.problems.map((p: ProblemInfo, i) => (
        <div key={`${p.file}:${i}`} className="project-problem">
          <span className="project-problem-file" title={p.file}>{p.file}</span>
          <span className="task-error">{p.reason}</span>
          <div className="task-actions">
            <Button variant="ghost" size="sm" onClick={() => askAboutProblem(project, p)} title="Put the problem in an agent chat">✦ Ask the agent</Button>
          </div>
        </div>
      ))}
    </div>
  )
}
