import { useEffect, useState, type DragEvent } from 'react'
import type { ContributedTab } from '../../lib/features'
import { useEditor } from '../../store/editor'
import { ProjectDialogs } from './Dialogs'
import { ItemCard, OldFormatNote, ProjectContext } from './ItemParts'
import {
  boardProblem,
  boardTitle,
  BOARD_STATUSES,
  coerceBoardData,
  DRAG_TYPE,
  dragPayload,
  dropStatus,
  groupByStatus,
  OLD_FORMAT_HINT,
  parseDrop,
  statusLabel,
  type ProjectData,
  type ItemInfo,
  type Status,
} from './projects'
import { followProjects, loadProject, openProject, openNewItem, openPlan, refreshProjects, setStatus, useProjects } from './store'
import { Button, IconButton, ViewToolbar } from '@flint-ide/ui'
import { CountBadge, MarkerBadge } from '../../components/tasks/badges'
import { Plus, RefreshCw } from '@flint-ide/ui/icons'

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
          <Button variant="ghost" size="sm" onClick={() => void openProject().then(refreshProjects)}>Open Project…</Button>
        </p>
      </div>
    )
  }
  const project = loaded?.project
  if (!project) return <p className="muted empty">{loaded?.error || 'Loading the project…'}</p>
  return (
    <div className="project-board">
      <ViewToolbar aria-label="Project board">
        <span className="project-board-title">{project.name}</span>
        {!project.trusted && (
          <MarkerBadge tone="warn" title="The project's folder is not trusted: agents can't read it with get_project, and Delegate shows you each item before it sends it">
            Restricted Mode
          </MarkerBadge>
        )}
        <span className="muted truncate" title={project.path}>{project.path}</span>
        <ProjectContext project={project} />
        <span className="fui-grow" />
        {/* The title is on a wrapper: a disabled kit button takes no hover, and the hint says why. */}
        <span title={project.oldFormat ? OLD_FORMAT_HINT : 'New Item…'}>
          <Button variant="ghost" size="sm" disabled={project.oldFormat} onClick={() => openNewItem(folder, 'board')}>
            <Plus data-icon="inline-start" />
            New Item…
          </Button>
        </span>
        <span title={project.oldFormat ? OLD_FORMAT_HINT : 'Have an agent split a goal into items (the prompt goes into a chat draft)'}>
        <Button
          variant="ghost"
          size="sm"
          disabled={project.oldFormat}
          onClick={() => openPlan(folder, 'board')}
        >
          ✦ Plan with Agent…
        </Button>
        </span>
        <IconButton variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onClick={() => void loadProject(folder)}>
          <RefreshCw />
        </IconButton>
      </ViewToolbar>
      {project.oldFormat && <OldFormatNote folder={folder} />}
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
            <CountBadge>{groups[status].length}</CountBadge>
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
    <ItemCard
      project={project}
      item={item}
      frame="card"
      dragging={dragging}
      draggable={!project.oldFormat}
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, dragPayload(project.path, item))
        e.dataTransfer.effectAllowed = 'move'
        setDragging(true)
      }}
      onDragEnd={() => setDragging(false)}
    />
  )
}
