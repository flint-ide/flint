import { useEffect, useState, type DragEvent } from 'react'
import type { ContributedTab } from '../../lib/features'
import { useEditor } from '../../store/editor'
import { BacklogDialogs } from './Dialogs'
import { ItemActions, ItemMeta, ItemTasks, ItemTitle } from './ItemParts'
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
  type BacklogData,
  type ItemInfo,
  type Status,
} from './backlog'
import { followBacklogs, loadBacklog, openBacklog, openNewItem, refreshBacklogs, setStatus, useBacklog } from './store'

/** The Board tab: one column per status; a card can be dragged to another column or moved with its Status menu. */
export function BoardView({ tab, active }: { tab: ContributedTab; active: boolean }) {
  const board = coerceBoardData(tab.data)
  const folder = board?.folder ?? ''
  const infos = useBacklog((s) => s.infos)
  const loaded = useBacklog((s) => s.data[folder])
  useEffect(() => (folder ? followBacklogs(() => [folder]) : undefined), [folder])
  const info = infos?.find((i) => i.path === folder)
  useEffect(() => {
    if (info) useEditor.getState().retitleTab(tab.id, boardTitle(info.name))
  }, [info, tab.id])

  if (!board) return <p className="muted empty">This board's data is not valid: close the tab and open the board again with Show Backlog Board.</p>
  const problem = boardProblem(folder, infos)
  if (problem) {
    return (
      <div className="backlog-board-message">
        <p className="error">{problem}</p>
        <p>
          <button className="link" onClick={() => void openBacklog().then(refreshBacklogs)}>Open Backlog…</button>
        </p>
      </div>
    )
  }
  const backlog = loaded?.backlog
  if (!backlog) return <p className="muted empty">{loaded?.error || 'Loading the backlog…'}</p>
  return (
    <div className="backlog-board">
      <div className="backlog-board-head">
        <span className="backlog-board-title">{backlog.name}</span>
        {!backlog.trusted && (
          <span className="backlog-marker warn" title="The backlog's folder is not trusted: agents can't read it with get_backlog, and Delegate shows you each item before it sends it">
            Restricted Mode
          </span>
        )}
        <span className="muted truncate" title={backlog.path}>{backlog.path}</span>
        <span className="spacer" />
        <button className="icon" title="New Item…" onClick={() => openNewItem(folder, 'board')}>＋ New Item…</button>
        <button className="icon" title="Refresh" onClick={() => void loadBacklog(folder)}>↻</button>
      </div>
      {loaded?.error && <div className="backlog-board-error error">{loaded.error}</div>}
      <Columns backlog={backlog} />
      {backlog.problems.length > 0 && (
        <div className="backlog-board-problems muted" title={backlog.problems.map((p) => `${p.file}: ${p.reason}`).join('\n')}>
          {backlog.problems.length} {backlog.problems.length === 1 ? 'file has' : 'files have'} problems: see the Backlog panel.
        </div>
      )}
      {active && <BacklogDialogs host="board" />}
    </div>
  )
}

function Columns({ backlog }: { backlog: BacklogData }) {
  const groups = groupByStatus(backlog.items)
  const [over, setOver] = useState<Status | null>(null)
  const drop = (e: DragEvent, to: Status) => {
    e.preventDefault()
    setOver(null)
    const d = parseDrop(e.dataTransfer.getData(DRAG_TYPE), backlog.path)
    const status = d && dropStatus(d.status, to)
    if (d && status) void setStatus(backlog.path, d.id, status)
  }
  return (
    <div className="backlog-columns">
      {BOARD_STATUSES.map((status) => (
        <section
          key={status}
          className={`backlog-column${over === status ? ' over' : ''}`}
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
          <header className="backlog-column-head">
            <span>{statusLabel(status)}</span>
            <span className="badge">{groups[status].length}</span>
          </header>
          <div className="backlog-column-body scroll">
            {groups[status].map((item) => (
              <Card key={item.id} backlog={backlog} item={item} />
            ))}
            {groups[status].length === 0 && <div className="backlog-column-empty muted">Drop an item here</div>}
          </div>
        </section>
      ))}
    </div>
  )
}

function Card({ backlog, item }: { backlog: BacklogData; item: ItemInfo }) {
  const [dragging, setDragging] = useState(false)
  return (
    <div
      className={`backlog-card${dragging ? ' dragging' : ''}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, dragPayload(backlog.path, item))
        e.dataTransfer.effectAllowed = 'move'
        setDragging(true)
      }}
      onDragEnd={() => setDragging(false)}
    >
      <div className="backlog-card-title">
        <ItemTitle item={item} />
      </div>
      <ItemMeta item={item} backlog={backlog} />
      <ItemTasks tasks={item.tasks} />
      <ItemActions backlog={backlog} item={item} />
    </div>
  )
}
