import { useEffect, useState, type DragEvent } from 'react'
import type { ContributedTab } from '../../lib/features'
import { useEditor } from '../../store/editor'
import { ProjectDialogs } from './Dialogs'
import { ItemActions, ItemDependencies, ItemMeta, ItemTasks, ItemTitle, ProjectContext } from './ItemParts'
import {
  boardProblem,
  boardTitle,
  BOARD_STATUSES,
  coerceBoardData,
  DRAG_TYPE,
  dragPayload,
  dropStatus,
  groupByStatus,
  parseDrop,
  statusLabel,
  type ProjectData,
  type ItemInfo,
  type Status,
} from './projects'
import { followProjects, loadProject, openProject, openNewItem, openPlan, refreshProjects, setStatus, useProjects } from './store'

/** The Board tab: one column per status; a card can be dragged to another column or moved with its Status menu. */
export function BoardView({ tab, active }: { tab: ContributedTab; active: boolean }) {
  const board = coerceBoardData(tab.data)
  const folder = board?.folder ?? ''
  const infos = useProjects((s) => s.infos)
  const loaded = useProjects((s) => s.data[folder])
  useEffect(() => (folder ? followProjects(() => [folder]) : undefined), [folder])
  const info = infos?.find((i) => i.path === folder)
  useEffect(() => {
    if (info) useEditor.getState().retitleTab(tab.id, boardTitle(info.name))
  }, [info, tab.id])

  if (!board) return <p className="muted empty">This board's data is not valid: close the tab and open the board again with Show Project Board.</p>
  const problem = boardProblem(folder, infos)
  if (problem) {
    return (
      <div className="project-board-message">
        <p className="error">{problem}</p>
        <p>
          <button className="link" onClick={() => void openProject().then(refreshProjects)}>Open Project…</button>
        </p>
      </div>
    )
  }
  const project = loaded?.project
  if (!project) return <p className="muted empty">{loaded?.error || 'Loading the project…'}</p>
  return (
    <div className="project-board">
      <div className="project-board-head">
        <span className="project-board-title">{project.name}</span>
        {!project.trusted && (
          <span className="project-marker warn" title="The project's folder is not trusted: agents can't read it with get_project, and Delegate shows you each item before it sends it">
            Restricted Mode
          </span>
        )}
        <span className="muted truncate" title={project.path}>{project.path}</span>
        <ProjectContext project={project} />
        <span className="spacer" />
        <button className="icon" title="New Item…" onClick={() => openNewItem(folder, 'board')}>＋ New Item…</button>
        <button className="icon" title="Have an agent split a goal into items (the prompt goes into a chat draft)" onClick={() => openPlan(folder, 'board')}>✦ Plan with Agent…</button>
        <button className="icon" title="Refresh" onClick={() => void loadProject(folder)}>↻</button>
      </div>
      {loaded?.error && <div className="project-board-error error">{loaded.error}</div>}
      <Columns project={project} />
      {project.problems.length > 0 && (
        <div className="project-board-problems muted" title={project.problems.map((p) => `${p.file}: ${p.reason}`).join('\n')}>
          {project.problems.length} {project.problems.length === 1 ? 'file has' : 'files have'} problems: see the Projects panel.
        </div>
      )}
      {active && <ProjectDialogs host="board" />}
    </div>
  )
}

function Columns({ project }: { project: ProjectData }) {
  const groups = groupByStatus(project.items)
  const [over, setOver] = useState<Status | null>(null)
  const drop = (e: DragEvent, to: Status) => {
    e.preventDefault()
    setOver(null)
    const d = parseDrop(e.dataTransfer.getData(DRAG_TYPE), project.path)
    const status = d && dropStatus(d.status, to)
    if (d && status) void setStatus(project.path, d.id, status)
  }
  return (
    <div className="project-columns">
      {BOARD_STATUSES.map((status) => (
        <section
          key={status}
          className={`project-column${over === status ? ' over' : ''}`}
          aria-label={statusLabel(status)}
          onDragOver={(e) => {
            if (!e.dataTransfer.types.includes(DRAG_TYPE)) return
            e.preventDefault()
            e.dataTransfer.dropEffect = 'move'
            setOver(status)
          }}
          onDragLeave={(e) => !e.currentTarget.contains(e.relatedTarget as Node | null) && setOver((o) => (o === status ? null : o))}
          onDrop={(e) => drop(e, status)}
        >
          <header className="project-column-head">
            <span>{statusLabel(status)}</span>
            <span className="badge">{groups[status].length}</span>
          </header>
          <div className="project-column-body scroll">
            {groups[status].map((item) => (
              <Card key={item.id} project={project} item={item} />
            ))}
            {groups[status].length === 0 && <div className="project-column-empty muted">Drop an item here</div>}
          </div>
        </section>
      ))}
    </div>
  )
}

function Card({ project, item }: { project: ProjectData; item: ItemInfo }) {
  const [dragging, setDragging] = useState(false)
  return (
    <div
      className={`project-card${dragging ? ' dragging' : ''}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, dragPayload(project.path, item))
        e.dataTransfer.effectAllowed = 'move'
        setDragging(true)
      }}
      onDragEnd={() => setDragging(false)}
    >
      <div className="project-card-title">
        <ItemTitle item={item} items={project.items} />
      </div>
      <ItemMeta item={item} project={project} />
      <ItemDependencies item={item} items={project.items} />
      <ItemTasks tasks={item.tasks} />
      <ItemActions project={project} item={item} />
    </div>
  )
}
