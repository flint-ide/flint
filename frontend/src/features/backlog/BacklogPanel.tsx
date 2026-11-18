import { useEffect, type ReactNode } from 'react'
import { Dropdown } from '../../components/ui/Dropdown'
import { Select } from '../../components/ui/Select'
import { BacklogDialogs } from './Dialogs'
import { ItemActions, ItemDependencies, ItemMeta, ItemTasks, ItemTitle } from './ItemParts'
import {
  allReposResolve,
  canClone,
  canClose,
  cloneTitle,
  groupByStatus,
  PANEL_STATUSES,
  repoIssue,
  repoMarkers,
  statusLabel,
  type BacklogData,
  type BacklogInfo,
  type CloneState,
  type ProblemInfo,
  type RepoView,
} from './backlog'
import {
  askAboutProblem,
  cancelClone,
  closeBacklog,
  followBacklogs,
  loadBacklog,
  newBacklog,
  openBacklog,
  openBoard,
  openNewItem,
  openPlan,
  repoActions,
  selectBacklog,
  toggleGroup,
  trustBacklog,
  useBacklog,
} from './store'

/** The Backlog panel: the picker, the items by status and the problems of the selected backlog. */
export function BacklogPanel() {
  const { infos, selected, data, collapsed, clones } = useBacklog()
  useEffect(() => followBacklogs(() => [useBacklog.getState().selected]), [])
  const info = infos?.find((i) => i.path === selected)
  const loaded = data[selected]
  const backlog = loaded?.backlog ?? null

  return (
    <div className="panel backlog-panel">
      <div className="panel-header">
        <span className="panel-title">Backlog</span>
        <button className="icon" title="New Item…" disabled={!info} onClick={() => openNewItem(selected)}>＋</button>
        <button className="icon" title="Refresh" disabled={!info} onClick={() => void loadBacklog(selected)}>↻</button>
        <Dropdown
          align="end"
          minWidth={190}
          items={[
            { label: 'Open Backlog…', onSelect: () => void openBacklog() },
            { label: 'New Backlog…', onSelect: () => void newBacklog() },
            { label: 'Plan with Agent…', disabled: !info, onSelect: () => openPlan(selected) },
            { label: 'Show Board', disabled: !info, onSelect: () => info && openBoard(info) },
            { type: 'separator' },
            { label: 'Close Backlog', disabled: !canClose(info), hint: info?.workspace ? 'workspace folder' : undefined, onSelect: () => info && void closeBacklog(info) },
          ]}
          trigger={(t) => (
            <button {...t} className="icon" title="Open, create or close a backlog">⋯</button>
          )}
        />
      </div>
      {infos && infos.length > 0 && (
        <div className="backlog-picker">
          <Select
            block
            size="sm"
            value={selected}
            title={selected}
            options={infos.map((i) => ({
              value: i.path,
              label: i.name,
              hint: i.error || [i.trusted ? '' : 'Restricted Mode', i.workspace ? 'workspace folder' : i.path].filter(Boolean).join(' · '),
            }))}
            onChange={selectBacklog}
          />
        </div>
      )}
      <div className="tree scroll">
        {info?.error && <div className="placeholder error">{info.error}</div>}
        {loaded?.error && !info?.error && <div className="placeholder error">{loaded.error}</div>}
        {info && !(backlog?.trusted ?? info.trusted) && <Untrusted info={info} />}
        {backlog && backlog.repositories.length > 0 && <Repositories backlog={backlog} collapsed={collapsed} clone={clones[backlog.path]} />}
        {backlog && <Groups backlog={backlog} collapsed={collapsed} />}
        {infos?.length === 0 && (
          <div className="placeholder muted">
            <span>
              No backlog yet. A backlog is a folder of work items (Markdown files) for one or more repositories. <b>Delegate</b> turns an item
              into agent tasks, one per repository, and the item&apos;s status follows them.
            </span>
            <span>
              <button className="link" onClick={() => void openBacklog()}>Open Backlog…</button> ·{' '}
              <button className="link" onClick={() => void newBacklog()}>New Backlog…</button>
            </span>
          </div>
        )}
        {backlog && backlog.items.length === 0 && backlog.problems.length === 0 && (
          <div className="placeholder muted">
            No items. <button className="link" onClick={() => openNewItem(selected)}>New Item…</button> writes one to items/;{' '}
            <button className="link" onClick={() => openPlan(selected)}>Plan with Agent…</button> has an agent split a goal into items.
          </div>
        )}
        {backlog && backlog.problems.length > 0 && <Problems backlog={backlog} />}
      </div>
      <BacklogDialogs host="panel" />
    </div>
  )
}

/** A collapsible group header, operable with the mouse and with Enter or Space. */
function GroupHeader({ open, onToggle, children }: { open: boolean; onToggle: () => void; children: ReactNode }) {
  return (
    <div
      className="tree-row static root-row backlog-group-head"
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

/** The note on a backlog whose folder is not trusted: agents don't read it, and Delegate asks first. */
function Untrusted({ info }: { info: BacklogInfo }) {
  return (
    <div className="backlog-untrusted" role="status">
      <span className="backlog-marker warn">Restricted Mode</span>
      <span className="muted">
        This backlog&apos;s folder is not trusted: agents can&apos;t read it with get_backlog, and Delegate shows you each item before it sends it.
      </span>
      <button className="link" onClick={() => void trustBacklog(info)} title={info.path}>Trust Folder…</button>
    </div>
  )
}

/** The repositories of flint-backlog.json with their checkouts here; open while one can't be used by Delegate. */
function Repositories({ backlog, collapsed, clone }: { backlog: BacklogData; collapsed: Record<string, boolean>; clone: CloneState | undefined }) {
  const repos = backlog.repositories
  // A running clone keeps the group open until the user closes it: its progress is shown here.
  const open = collapsed.repositories === undefined ? !allReposResolve(repos) || !!clone : !collapsed.repositories
  const waiting = repos.filter((r) => repoIssue(r) !== null).length
  return (
    <div className="backlog-group backlog-repos">
      <GroupHeader open={open} onToggle={() => toggleGroup('repositories', !open)}>
        <span className="dir-name">Repositories</span>
        <span className="badge" title={waiting ? `${waiting} can't be used by Delegate yet` : 'Every repository has a checkout here'}>
          {waiting ? `${waiting}/${repos.length}` : repos.length}
        </span>
      </GroupHeader>
      {open && clone && <CloneProgress folder={backlog.path} clone={clone} />}
      {open && repos.map((r) => <RepoRow key={r.name} folder={backlog.path} repo={r} cloning={!!clone} />)}
    </div>
  )
}

/** The clone that runs for the backlog: git's latest progress line and Cancel. */
function CloneProgress({ folder, clone }: { folder: string; clone: CloneState }) {
  return (
    <div className="backlog-clone" role="status">
      <span className="backlog-clone-title" title={clone.destination}>{cloneTitle(clone)}</span>
      {clone.progress && <span className="backlog-clone-line muted" title={clone.progress}>{clone.progress}</span>}
      <div className="task-actions">
        <button className="link" onClick={() => void cancelClone(folder)} title="Stop git; what it made is removed">Cancel</button>
      </div>
    </div>
  )
}

function RepoRow({ folder, repo, cloning }: { folder: string; repo: RepoView; cloning: boolean }) {
  const issue = repoIssue(repo)
  return (
    <div className="task backlog-item backlog-repo">
      <div className="tree-row static task-row" title={repo.url}>
        <span className="file-name">{repo.name}</span>
        {repoMarkers(repo).map((m) => (
          <span key={m.label} className={`backlog-marker ${m.cls}`} title={m.hint}>{m.label}</span>
        ))}
      </div>
      <div className="backlog-meta muted">
        <span title={repo.url}>{repo.url}</span>
      </div>
      <div className="backlog-meta muted">
        <span title={repo.folder || undefined} className={repo.folder ? '' : 'error'}>{repo.folder || 'no checkout on this computer'}</span>
      </div>
      <div className="task-actions backlog-actions">
        <button className="link" onClick={() => repoActions.locate(folder, repo)} title="Choose the folder of your checkout of this repository">Locate…</button>
        {canClone(repo) && (
          <button
            className="link"
            disabled={cloning}
            onClick={() => repoActions.clone(folder, repo)}
            title={cloning ? 'A clone of this backlog is running' : `Clone ${repo.url} into a folder you choose (asks again before it runs)`}
          >
            Clone…
          </button>
        )}
        {issue?.fix === 'attach' && (
          <button className="link" onClick={() => repoActions.attach(folder, repo)} title={`Add ${repo.folder} to the workspace`}>Add to Workspace</button>
        )}
        {issue?.fix === 'trust' && (
          <button className="link" onClick={() => repoActions.trust(folder, repo)} title={`Trust ${repo.folder}`}>Trust Folder…</button>
        )}
      </div>
    </div>
  )
}

function Groups({ backlog, collapsed }: { backlog: BacklogData; collapsed: Record<string, boolean> }) {
  const groups = groupByStatus(backlog.items)
  return (
    <>
      {PANEL_STATUSES.map((status) => {
        const items = groups[status]
        if (items.length === 0) return null
        const open = !collapsed[status]
        return (
          <div key={status} className="backlog-group">
            <GroupHeader open={open} onToggle={() => toggleGroup(status, !open)}>
              <span className="dir-name">{statusLabel(status)}</span>
              <span className="badge">{items.length}</span>
            </GroupHeader>
            {open &&
              items.map((item) => (
                <div key={item.id} className="task backlog-item">
                  <div className="tree-row static task-row" title={item.path}>
                    <ItemTitle item={item} items={backlog.items} />
                  </div>
                  <ItemMeta item={item} backlog={backlog} />
                  <ItemDependencies item={item} items={backlog.items} />
                  <ItemTasks tasks={item.tasks} />
                  <ItemActions backlog={backlog} item={item} />
                </div>
              ))}
          </div>
        )
      })}
    </>
  )
}

/** Files Flint could not use, each with the hand-off to the agent. */
function Problems({ backlog }: { backlog: BacklogData }) {
  return (
    <div className="backlog-problems">
      <div className="tree-row static root-row">
        <span className="dir-name">Problems</span>
        <span className="badge">{backlog.problems.length}</span>
      </div>
      {backlog.problems.map((p: ProblemInfo, i) => (
        <div key={`${p.file}:${i}`} className="backlog-problem">
          <span className="backlog-problem-file" title={p.file}>{p.file}</span>
          <span className="task-error">{p.reason}</span>
          <div className="task-actions">
            <button className="link" onClick={() => askAboutProblem(backlog, p)} title="Put the problem in an agent chat">✦ Ask the agent</button>
          </div>
        </div>
      ))}
    </div>
  )
}
