import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import { useVirtualList } from '../../lib/virtualList'
import type { QueryResult } from '../../api/backend'
import { cellText, columnWidths, toTSV } from '../../lib/format'
import { applyView, emptyView, isFiltered, toggleSort, type GridView } from '../../lib/gridView'
import { inList, insertRowStatement } from '../../lib/sqlScripts'
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
import { openContextMenu } from '../../store/contextMenu'
import { showToast } from '../../store/dialog'
import { CellEditor } from './CellEditor'
import { ColumnFilter } from './ColumnFilter'
import { GridHeader } from './GridHeader'
import { cellMenu, headerMenu, type GridMenuActions } from './gridMenu'

const ROW_HEIGHT = 24
const INDEX_WIDTH = 56
const EDITOR_MIN_WIDTH = 180
const JSON_EDITOR_WIDTH = 380
const PAGE_ROWS = 20

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
  /** SQL dialect for "Copy as INSERT" literals and quoting. */
  driver?: string
  /** Qualified table name for "Copy as INSERT" (else a placeholder). */
  tableName?: string
}

/** A cell by its row in result.rows (inserted rows follow them) and column. */
interface Cell {
  row: number
  col: number
}

const clamp = (n: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, n))

/**
 * Virtualized result table: only visible rows are rendered, so 100k rows stay smooth.
 * Sorting and value filters are local: they reorder and hide the fetched rows without
 * re-running the query. Selection and edits keep addressing rows of result.rows.
 */
export function ResultsGrid({ result, summary, actions, editing, driver = 'postgres', tableName }: Props) {
  const [selected, setSelected] = useState<Cell | null>(null)
  const [editingCell, setEditingCell] = useState<Cell | null>(null)
  const [view, setView] = useState<GridView>(emptyView)
  const [filterCol, setFilterCol] = useState<{ col: number; anchor: HTMLElement } | null>(null)
  const search = useRef<HTMLInputElement>(null)
  const widths = useMemo(() => columnWidths(result), [result])
  const offsets = useMemo(() => widths.reduce<number[]>((acc, w, i) => [...acc, acc[i] + w], [INDEX_WIDTH]), [widths])
  const totalWidth = offsets[offsets.length - 1]
  const template = `${INDEX_WIDTH}px ${widths.map((w) => `${w}px`).join(' ')}`

  // Typing in the search box stays responsive on big results: the rows follow a beat later.
  const shownView = useDeferredValue(view)
  const order = useMemo(() => applyView(result.rows, shownView), [result.rows, shownView])
  const visualOf = useMemo(() => new Map(order.map((row, i) => [row, i])), [order])
  const edits = editing?.edits
  const insertedCount = edits?.inserted.length ?? 0
  const rowCount = order.length + insertedCount
  const base = result.rows.length
  /** The row shown at a display position; new rows stay after the (filtered) result. */
  const rowAt = (vi: number) => (vi < order.length ? order[vi] : base + vi - order.length)
  const positionOf = (row: number) => (row >= base ? order.length + row - base : (visualOf.get(row) ?? -1))

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
  const prevInserted = useRef(insertedCount)
  useEffect(() => {
    const el = scroller.current
    if (insertedCount > prevInserted.current && el) {
      el.scrollTop = el.scrollHeight
      setSelected({ row: base + insertedCount - 1, col: 0 })
      el.focus()
    }
    prevInserted.current = insertedCount
  }, [insertedCount, base])

  // A filter that hides the selected (or edited) row drops the selection, so keys
  // and the toolbar's row actions never act on a row you can't see.
  useEffect(() => {
    const hidden = (c: Cell | null) => c !== null && c.row < base && !visualOf.has(c.row)
    setSelected((c) => (hidden(c) ? null : c))
    setEditingCell((c) => (hidden(c) ? null : c))
  }, [visualOf, base])

  const valueAt = (row: number, col: number): unknown => (edits ? displayValue(result, edits, row, col) : result.rows[row][col])
  const editableColumn = (col: number): TargetColumn | null => {
    const c = editing?.columns[col]
    return c?.editable ? c : null
  }
  const rowValues = (row: number) => result.columns.map((_, c) => valueAt(row, c))
  /** The shown rows (sorted, filtered) with staged edits, as copied. */
  const shownResult = () => ({ columns: result.columns, rows: order.map(rowValues) })

  const copy = async (text: string, what: string) => {
    try {
      await navigator.clipboard.writeText(text)
      showToast(`Copied ${what}`)
    } catch (err) {
      showToast(`Copy failed: ${err instanceof Error ? err.message : String(err)}`, 'error')
    }
  }

  const startEdit = (cell: Cell): boolean => {
    if (!editableColumn(cell.col) || edits?.deleted[cell.row]) return false
    setSelected(cell)
    setEditingCell(cell)
    return true
  }

  const commitEdit = (cell: Cell, value: CellValue) => {
    if (editing) {
      const original = cell.row < base ? result.rows[cell.row][cell.col] : undefined
      editing.onChange(setCell(result, editing.edits, cell.row, cell.col, sameJSON(value, original) ? toCellValue(original) : value))
    }
    setEditingCell(null)
    scroller.current?.focus()
  }

  /** Scrolls so the cell at a display position is fully visible (below the sticky header). */
  const reveal = (vi: number, col: number) => {
    const el = scroller.current
    if (!el) return
    const top = (vi + 1) * ROW_HEIGHT
    if (top < el.scrollTop + ROW_HEIGHT) el.scrollTop = top - ROW_HEIGHT
    else if (top + ROW_HEIGHT > el.scrollTop + el.clientHeight) el.scrollTop = top + ROW_HEIGHT - el.clientHeight
    const left = offsets[col]
    const right = offsets[col + 1]
    if (left < el.scrollLeft + INDEX_WIDTH) el.scrollLeft = left - INDEX_WIDTH
    else if (right > el.scrollLeft + el.clientWidth) el.scrollLeft = right - el.clientWidth
  }

  const move = (dRow: number, dCol: number) => {
    if (rowCount === 0) return
    const from = selected ? positionOf(selected.row) : -1
    const vi = clamp(from < 0 ? 0 : from + dRow, 0, rowCount - 1)
    const col = clamp((selected?.col ?? 0) + dCol, 0, result.columns.length - 1)
    setSelected({ row: rowAt(vi), col })
    reveal(vi, col)
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    const mod = e.metaKey || e.ctrlKey
    const step: Record<string, [number, number]> = {
      ArrowUp: [-1, 0],
      ArrowDown: [1, 0],
      ArrowLeft: [0, -1],
      ArrowRight: [0, 1],
      PageUp: [-PAGE_ROWS, 0],
      PageDown: [PAGE_ROWS, 0],
    }
    if (mod && e.key === 'f') {
      e.preventDefault()
      search.current?.focus()
      search.current?.select()
    } else if (step[e.key] && !mod) {
      e.preventDefault()
      move(...step[e.key])
    } else if (!selected) {
      return
    } else if (mod && e.key === 'c') {
      e.preventDefault()
      void copy(cellText(valueAt(selected.row, selected.col)), 'cell')
    } else if (editing && (e.key === 'Enter' || e.key === 'F2') && !mod) {
      if (startEdit(selected)) e.preventDefault()
    } else if (editing && mod && e.key === 'Backspace') {
      e.preventDefault()
      editing.onChange(toggleDelete(result, editing.edits, selected.row))
    }
  }

  const menuActions: GridMenuActions = {
    setView,
    openFilter: (col) => {
      const el = scroller.current?.querySelector<HTMLElement>(`.grid-header .header-cell:nth-child(${col + 2}) .filter-button`)
      if (el) setFilterCol({ col, anchor: el })
    },
    copy: (text, what) => void copy(text, what),
  }
  const columnValues = (col: number) => order.map((i) => valueAt(i, col))

  const onCellMenu = (cell: Cell, e: MouseEvent) => {
    e.preventDefault()
    setSelected(cell)
    const name = result.columns[cell.col].name
    const columns = result.columns.map((c) => c.name)
    const items = cellMenu(
      {
        view,
        col: cell.col,
        columnName: name,
        // Filters match the fetched values, so they're built from the original.
        value: cell.row < base ? result.rows[cell.row][cell.col] : valueAt(cell.row, cell.col),
        shown: valueAt(cell.row, cell.col),
        copies: {
          row: () => rowValues(cell.row).map((v) => (v === null ? 'NULL' : cellText(v))).join('\t'),
          rowJSON: () => JSON.stringify(Object.fromEntries(columns.map((c, i) => [c, rowValues(cell.row)[i]])), null, 2),
          rowInsert: () => {
            // DEFAULT cells of a new row are left out, so the column default applies.
            const values = rowValues(cell.row)
            const set = columns.flatMap((c, i) => (values[i] === undefined ? [] : [[c, values[i]] as const]))
            return insertRowStatement(driver, tableName ?? 'table_name', set.map(([c]) => c), set.map(([, v]) => v))
          },
          column: () => columnValues(cell.col).map((v) => (v === null ? 'NULL' : cellText(v))).join('\n'),
          columnIn: () => inList(driver, columnValues(cell.col)),
        },
        edit: editing && {
          canEdit: !!editableColumn(cell.col),
          deleted: !!edits?.deleted[cell.row],
          edit: () => void startEdit(cell),
          setNull: () => editing.onChange(setCell(result, editing.edits, cell.row, cell.col, null)),
          toggleDelete: () => editing.onChange(toggleDelete(result, editing.edits, cell.row)),
        },
      },
      menuActions,
    )
    openContextMenu(e.clientX, e.clientY, items, { minWidth: 240, onClose: () => scroller.current?.focus() })
  }

  const onHeaderMenu = (col: number, e: MouseEvent) => {
    e.preventDefault()
    const values = () => columnValues(col).map((v) => (v === null ? 'NULL' : cellText(v))).join('\n')
    openContextMenu(e.clientX, e.clientY, headerMenu(view, col, result.columns[col].name, values, menuActions), { minWidth: 220 })
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
  const filtered = isFiltered(view)

  return (
    <div className="results-grid">
      <div className="result-summary">
        {summary}
        {filtered && (
          <span className="filter-status" title="Rows hidden by local filters (the query wasn't re-run)">
            {order.length.toLocaleString()} of {base.toLocaleString()} shown
            <button className="link" onClick={() => setView({ ...view, filters: {}, search: '' })}>Clear filters</button>
          </span>
        )}
        <span className="spacer" />
        <input
          ref={search}
          className="grid-search"
          placeholder="Filter rows…"
          title="Show rows containing this text in any column (local, case-insensitive)"
          value={view.search}
          onChange={(e) => setView({ ...view, search: e.target.value })}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              setView({ ...view, search: '' })
              scroller.current?.focus()
            } else if (e.key === 'Enter' || e.key === 'ArrowDown') {
              e.preventDefault()
              scroller.current?.focus()
              if (!selected && rowCount > 0) setSelected({ row: rowAt(0), col: 0 })
            }
          }}
        />
        {actions?.(selected?.row ?? null)}
        <button className="link" onClick={() => copy(toTSV(shownResult()), 'as TSV')}>Copy TSV</button>
        <button className="link" onClick={() => copy(JSON.stringify(toObjects(shownResult()), null, 2), 'as JSON')}>
          Copy JSON
        </button>
      </div>
      <div className="grid-scroller" ref={setScroller} tabIndex={0} onKeyDown={onKeyDown}>
        <div style={{ width: totalWidth, height: totalSize + ROW_HEIGHT, position: 'relative' }}>
          <GridHeader
            result={result}
            view={view}
            template={template}
            width={totalWidth}
            targets={editing?.columns}
            onSort={(col, add) => setView((v) => toggleSort(v, col, add))}
            onFilter={(col, anchor) => setFilterCol(filterCol?.col === col ? null : { col, anchor })}
            onMenu={onHeaderMenu}
          />
          {items.map((vr) => {
            const row = rowAt(vr.index)
            const inserted = row >= base
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
                      onContextMenu={(e) => onCellMenu({ row, col: ci }, e)}
                    >
                      {v === null ? 'NULL' : v === undefined ? 'DEFAULT' : cellText(v)}
                    </div>
                  )
                })}
              </div>
            )
          })}
          {rowCount === 0 && base > 0 && <div className="grid-empty muted" style={{ top: ROW_HEIGHT * 2 }}>No rows match the filters.</div>}
          {editingCell && editorColumn && positionOf(editingCell.row) >= 0 && (
            <CellEditor
              key={`${editingCell.row}:${editingCell.col}`}
              column={editorColumn}
              initial={editorInitial(valueAt(editingCell.row, editingCell.col))}
              style={{
                left: offsets[editingCell.col],
                top: (positionOf(editingCell.row) + 1) * ROW_HEIGHT,
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
      {filterCol && (
        <ColumnFilter
          anchor={filterCol.anchor}
          rows={result.rows}
          col={filterCol.col}
          columnName={result.columns[filterCol.col].name}
          view={view}
          onChange={setView}
          onClose={() => setFilterCol(null)}
        />
      )}
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

function toObjects(result: Pick<QueryResult, 'columns' | 'rows'>): Record<string, unknown>[] {
  return result.rows.map((row) => Object.fromEntries(result.columns.map((c, i) => [c.name, row[i]])))
}
