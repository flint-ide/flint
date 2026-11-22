import { useEffect } from 'react'
import { Dropdown } from '../../components/ui/Dropdown'
import { Select } from '../../components/ui/Select'
import { ProjectDialogs } from './Dialogs'
import { ItemCard, OldFormatNote, ProjectContext } from './ItemParts'
import { repoNotes } from './cardParts'
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
import { Button, IconButton, PanelHeader, TaskCard, TreeRow, TreeSection } from '@flint-ide/ui'
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
      <PanelHeader
        title="Projects"
        actions={
          <>
            {/* The title is on a wrapper: a disabled kit button takes no hover, and the hint says why. */}
            <span title={oldFormat ? OLD_FORMAT_HINT : 'New Item…'}>
              <IconButton variant="ghost" size="sm" aria-label="New Item" disabled={!info || oldFormat} onClick={() => openNewItem(selected)}>
                <Plus />
              </IconButton>
            </span>
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
          </>
        }
      />
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
      <TreeSection
        title="Repositories"
        open={open}
        onOpenChange={(next) => toggleGroup('repositories', next)}
        count={
          <CountBadge title={waiting ? `${waiting} can't be used by Delegate yet` : 'Every repository has a checkout here'}>
            {waiting ? `${waiting}/${repos.length}` : repos.length}
          </CountBadge>
        }
      >
        {clone && <CloneProgress folder={project.path} clone={clone} />}
        {repos.map((r) => <RepoRow key={r.name} folder={project.path} repo={r} cloning={!!clone} />)}
      </TreeSection>
    </div>
  )
}

/** The clone that runs for the project: git's latest progress line and Cancel. */
function CloneProgress({ folder, clone }: { folder: string; clone: CloneState }) {
  return (
    <div className="project-clone" role="status">
      <TaskCard
        frame="bare"
        label={<span title={clone.destination}>{cloneTitle(clone)}</span>}
        notes={[!!clone.progress && { text: clone.progress, title: clone.progress, mono: true }]}
        actions={<Button variant="ghost" size="sm" onClick={() => void cancelClone(folder)} title="Stop git; what it made is removed">Cancel</Button>}
      />
    </div>
  )
}

function RepoRow({ folder, repo, cloning }: { folder: string; repo: RepoView; cloning: boolean }) {
  const issue = repoIssue(repo)
  return (
    <TaskCard
      tooltip={repo.url}
      label={repo.name}
      badges={repoMarkers(repo).map((m) => (
        <MarkerBadge key={m.label} tone={m.cls} title={m.hint}>{m.label}</MarkerBadge>
      ))}
      notes={repoNotes(repo)}
      actions={
        <>
          <Button variant="ghost" size="sm" onClick={() => repoActions.locate(folder, repo)} title="Choose the folder of your checkout of this repository">Locate…</Button>
          {canClone(repo) && (
            <span title={cloning ? 'A clone of this project is running' : `Clone ${repo.url} into a folder you choose (asks again before it runs)`}>
              <Button variant="ghost" size="sm" disabled={cloning} onClick={() => repoActions.clone(folder, repo)}>
                Clone…
              </Button>
            </span>
          )}
          {issue?.fix === 'attach' && (
            <Button variant="ghost" size="sm" onClick={() => repoActions.attach(folder, repo)} title={`Add ${repo.folder} to the workspace`}>Add to Workspace</Button>
          )}
          {issue?.fix === 'trust' && (
            <Button variant="ghost" size="sm" onClick={() => repoActions.trust(folder, repo)} title={`Trust ${repo.folder}`}>Trust Folder…</Button>
          )}
        </>
      }
    />
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
          <TreeSection
            key={status}
            className="project-group"
            title={statusLabel(status)}
            count={items.length}
            open={open}
            onOpenChange={(next) => toggleGroup(status, next)}
          >
            {items.map((item) => (
              <ItemCard key={item.id} project={project} item={item} tooltip={item.path} />
            ))}
          </TreeSection>
        )
      })}
    </>
  )
}

/** Files Flint could not use, each with the hand-off to the agent. */
function Problems({ project }: { project: ProjectData }) {
  return (
    <div className="project-problems">
      <TreeRow emphasis="group" label="Problems" badge={project.problems.length} />
      {project.problems.map((p: ProblemInfo, i) => (
        <TaskCard
          key={`${p.file}:${i}`}
          label={<span className="fui-mono" title={p.file}>{p.file}</span>}
          notes={[{ text: p.reason, tone: 'danger' }]}
          actions={<Button variant="ghost" size="sm" onClick={() => askAboutProblem(project, p)} title="Put the problem in an agent chat">✦ Ask the agent</Button>}
        />
      ))}
    </div>
  )
}
