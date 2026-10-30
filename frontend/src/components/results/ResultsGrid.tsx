import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useVirtualList } from '../../lib/virtualList'
import type { QueryResult } from '../../api/backend'
import { cellText, columnWidths, toTSV } from '../../lib/format'
import {
  displayValue,
  isCellChanged,
  setCell,
  toggleDelete,
  toCellValue,
  type CellValue,
  type StagedEdits,
  type TargetColumn,
} from '../../lib/rowEdits'
import { showToast } from '../../store/dialog'
import { CellEditor } from './CellEditor'

const ROW_HEIGHT = 24
const INDEX_WIDTH = 56
const EDITOR_MIN_WIDTH = 180
const JSON_EDITOR_WIDTH = 380

/** Staged editing of the grid's rows (see EditableResults). */
export interface GridEditing {
  /** Per result column, the table column it edits (null = read-only column). */
  columns: (TargetColumn | null)[]
  edits: StagedEdits
  onChange: (edits: StagedEdits) => void
}

interface Props {
  result: QueryResult
  /** Left side of the grid's header bar (e.g. row count and timing). */
  summary: ReactNode
  /** Extra controls in the header bar; gets the selected row. */
  actions?: (selectedRow: number | null) => ReactNode
  editing?: GridEditing
}

interface Cell {
  row: number
  col: number
}

/** Virtualized result table: only visible rows are rendered, so 100k rows stay smooth. */
export function ResultsGrid({ result, summary, actions, editing }: Props) {
  const [selected, setSelected] = useState<Cell | null>(null)
  const [editingCell, setEditingCell] = useState<Cell | null>(null)
  const widths = useMemo(() => columnWidths(result), [result])
  const offsets = useMemo(() => widths.reduce<number[]>((acc, w, i) => [...acc, acc[i] + w], [INDEX_WIDTH]), [widths])
  const totalWidth = offsets[offsets.length - 1]
  const template = `${INDEX_WIDTH}px ${widths.map((w) => `${w}px`).join(' ')}`

  const edits = editing?.edits
  const rowCount = result.rows.length + (edits?.inserted.length ?? 0)
  const { scrollRef, totalSize, items } = useVirtualList({ count: rowCount, itemSize: ROW_HEIGHT, overscan: 20 })
  // The virtual list owns the scroller through a callback ref; keep our own handle for focus.
  const scroller = useRef<HTMLDivElement | null>(null)
  const setScroller = useCallback(
    (node: HTMLDivElement | null) => {
      scroller.current = node
      scrollRef(node)
    },
    [scrollRef],
  )

  // A row added with "+ Row" lands at the end: scroll to it and select its first cell.
  const insertedCount = edits?.inserted.length ?? 0
  const prevInserted = useRef(insertedCount)
  useEffect(() => {
    const el = scroller.current
    if (insertedCount > prevInserted.current && el) {
      el.scrollTop = el.scrollHeight
      setSelected({ row: rowCount - 1, col: 0 })
      el.focus()
    }
    prevInserted.current = insertedCount
  }, [insertedCount, rowCount])

  const valueAt = (row: number, col: number): unknown => (edits ? displayValue(result, edits, row, col) : result.rows[row][col])
  const editableColumn = (col: number): TargetColumn | null => {
    const c = editing?.columns[col]
    return c?.editable ? c : null
  }

  const copy = async (text: string, what: string) => {
    await navigator.clipboard.writeText(text)
    showToast(`Copied ${what}`)
  }

  const startEdit = (cell: Cell): boolean => {
    if (!editableColumn(cell.col) || edits?.deleted[cell.row]) return false
    setSelected(cell)
    setEditingCell(cell)
    return true
  }

  const commitEdit = (cell: Cell, value: CellValue) => {
    if (editing) {
      const original = cell.row < result.rows.length ? result.rows[cell.row][cell.col] : undefined
      editing.onChange(setCell(result, editing.edits, cell.row, cell.col, sameJSON(value, original) ? toCellValue(original) : value))
    }
    setEditingCell(null)
    scroller.current?.focus()
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!selected) return
    const mod = e.metaKey || e.ctrlKey
    if (mod && e.key === 'c') {
      e.preventDefault()
      void copy(cellText(valueAt(selected.row, selected.col)), 'cell')
    } else if (editing && (e.key === 'Enter' || e.key === 'F2') && !mod) {
      if (startEdit(selected)) e.preventDefault()
    } else if (editing && mod && e.key === 'Backspace') {
      e.preventDefault()
      editing.onChange(toggleDelete(result, editing.edits, selected.row))
    }
  }

  const cellClass = (row: number, col: number, v: unknown): string => {
    let cls = 'grid-cell'
    if (v === null) cls += ' null'
    else if (v === undefined) cls += ' default'
    if (edits && isCellChanged(result, edits, row, col)) cls += ' edited'
    if (selected?.row === row && selected.col === col) cls += ' selected'
    return cls
  }

  const editorColumn = editingCell && editableColumn(editingCell.col)

  return (
    <div className="results-grid">
      <div className="result-summary">
        {summary}
        <span className="spacer" />
        {actions?.(selected?.row ?? null)}
        <button className="link" onClick={() => copy(toTSV(result), 'as TSV')}>Copy TSV</button>
        <button className="link" onClick={() => copy(JSON.stringify(toObjects(result), null, 2), 'as JSON')}>
          Copy JSON
        </button>
      </div>
      <div className="grid-scroller" ref={setScroller} tabIndex={0} onKeyDown={onKeyDown}>
        <div style={{ width: totalWidth, height: totalSize + ROW_HEIGHT, position: 'relative' }}>
          <div className="grid-row grid-header" style={{ gridTemplateColumns: template, width: totalWidth }}>
            <div className="grid-cell index">#</div>
            {result.columns.map((c, i) => {
              const target = editing?.columns[i]
              const title = `${c.name} : ${c.type}${editing && !target?.editable ? ` (read-only${target?.readOnlyReason ? `: ${target.readOnlyReason}` : ''})` : target?.primaryKey ? ' (primary key)' : ''}`
              return (
                <div key={i} className={`grid-cell${editing && !target?.editable ? ' readonly' : ''}`} title={title}>
                  <span className="col-name">{target?.primaryKey && <span className="pk-mark">PK</span>}{c.name}</span>
                  <span className="col-type">{c.type}</span>
                </div>
              )
            })}
          </div>
          {items.map((vr) => {
            const row = vr.index
            const inserted = row >= result.rows.length
            const deleted = !!edits?.deleted[row]
            return (
              <div
                key={row}
                className={`grid-row${inserted ? ' inserted' : ''}${deleted ? ' deleted' : ''}`}
                style={{
                  gridTemplateColumns: template,
                  width: totalWidth,
                  transform: `translateY(${vr.start + ROW_HEIGHT}px)`,
                }}
              >
                <div className="grid-cell index">{inserted ? '+' : row + 1}</div>
                {result.columns.map((_, ci) => {
                  const v = valueAt(row, ci)
                  return (
                    <div
                      key={ci}
                      className={cellClass(row, ci, v)}
                      onClick={() => setSelected({ row, col: ci })}
                      onDoubleClick={() => startEdit({ row, col: ci }) || void copy(cellText(v), 'cell')}
                    >
                      {v === null ? 'NULL' : v === undefined ? 'DEFAULT' : cellText(v)}
                    </div>
                  )
                })}
              </div>
            )
          })}
          {editingCell && editorColumn && (
            <CellEditor
              key={`${editingCell.row}:${editingCell.col}`}
              column={editorColumn}
              initial={editorInitial(valueAt(editingCell.row, editingCell.col))}
              style={{
                left: offsets[editingCell.col + 1] - widths[editingCell.col],
                top: (editingCell.row + 1) * ROW_HEIGHT,
                minWidth: editorColumn.kind === 'json' ? JSON_EDITOR_WIDTH : Math.max(widths[editingCell.col], EDITOR_MIN_WIDTH),
              }}
              onCommit={(value) => commitEdit(editingCell, value)}
              onCancel={() => {
                setEditingCell(null)
                scroller.current?.focus()
              }}
            />
          )}
        </div>
      </div>
    </div>
  )
}

/** Whether edited text is the original JSON document, only re-formatted by the editor. */
function sameJSON(value: CellValue, original: unknown): boolean {
  if (value === null || original === null || typeof original !== 'object') return false
  try {
    return JSON.stringify(JSON.parse(value)) === JSON.stringify(original)
  } catch {
    return false
  }
}

/** A cell's current value as editor text; undefined (DEFAULT) stays undefined. */
function editorInitial(v: unknown): CellValue | undefined {
  if (v === undefined) return undefined
  if (v !== null && typeof v === 'object') return JSON.stringify(v, null, 2)
  return toCellValue(v)
}

function toObjects(result: QueryResult): Record<string, unknown>[] {
  return result.rows.map((row) => Object.fromEntries(result.columns.map((c, i) => [c.name, row[i]])))
}
