import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react'
import type { Row, Table } from '../../../bindings/flint/internal/extensions/host/models'
import { useVirtualList } from '../../lib/virtualList'
import { ariaSort, badgeSeverity, cellText, columnWidths, sortRows, tableToTSV, toggleSort, type TableSort } from '../../lib/viewTable'
import { openContextMenu } from '../../store/contextMenu'
import { showToast } from '../../store/dialog'

const ROW_HEIGHT = 24
const PAGE_ROWS = 20

interface Props {
  table: Table
  /** The extension sent more rows than Flint shows. */
  truncated: boolean
  /** Runs a row's command (one of the extension's own; Go checked it). */
  onCommand: (row: Row) => void
  /** Sends the choice of a row's action. */
  onAction: (row: string, action: string) => void
}

/**
 * A table view of an extension: typed columns the user sorts by clicking a
 * header (the rows are sorted here, not by the extension), virtualized rows,
 * a row's command on Enter or a double click and its actions in a context
 * menu. Cells are text; a badge's color is a severity of Flint's theme.
 */
export function ViewTable({ table, truncated, onCommand, onAction }: Props) {
  // The user's sort wins until the next reload of the page; undefined follows the extension's.
  const [userSort, setUserSort] = useState<TableSort | null | undefined>(undefined)
  const sort = userSort === undefined ? (table.sort ?? null) : userSort
  const [selected, setSelected] = useState<string | null>(null)
  const { columns, rows } = table
  const order = useMemo(() => sortRows(columns, rows, sort), [columns, rows, sort])
  const widths = useMemo(() => columnWidths(columns, rows), [columns, rows])
  const totalWidth = widths.reduce((a, b) => a + b, 0)
  const template = widths.map((w) => `${w}px`).join(' ')
  const position = useMemo(() => new Map(order.map((ri, vi) => [rows[ri].id, vi])), [order, rows])

  const { scrollRef, totalSize, items } = useVirtualList({ count: order.length, itemSize: ROW_HEIGHT, overscan: 20 })
  const scroller = useRef<HTMLDivElement | null>(null)
  const setScroller = useCallback(
    (node: HTMLDivElement | null) => {
      scroller.current = node
      scrollRef(node)
    },
    [scrollRef],
  )

  // A row that is gone after a refresh is no longer selected.
  useEffect(() => {
    if (selected !== null && !position.has(selected)) setSelected(null)
  }, [position, selected])

  const actionsOf = (row: Row) => (row.actions?.length ? row.actions : (table.actions ?? []))

  const openMenu = (row: Row, x: number, y: number) => {
    const actions = actionsOf(row)
    if (!actions.length) return
    openContextMenu(
      x,
      y,
      actions.map((a) => ({ label: a.label, onSelect: () => onAction(row.id, a.id) })),
      { onClose: () => scroller.current?.focus() },
    )
  }

  const onRowMenu = (e: MouseEvent, row: Row) => {
    setSelected(row.id)
    if (!actionsOf(row).length) return
    e.preventDefault()
    openMenu(row, e.clientX, e.clientY)
  }

  const reveal = (vi: number) => {
    const el = scroller.current
    if (!el) return
    const top = (vi + 1) * ROW_HEIGHT // below the sticky header
    if (top < el.scrollTop + ROW_HEIGHT) el.scrollTop = top - ROW_HEIGHT
    else if (top + ROW_HEIGHT > el.scrollTop + el.clientHeight) el.scrollTop = top + ROW_HEIGHT - el.clientHeight
  }

  const select = (vi: number) => {
    if (order.length === 0) return
    const at = Math.max(0, Math.min(order.length - 1, vi))
    setSelected(rows[order[at]].id)
    reveal(at)
  }

  const copy = (text: string, what: string) =>
    navigator.clipboard.writeText(text).then(
      () => showToast(`Copied ${what}`),
      (err: unknown) => showToast(`Copy failed: ${err instanceof Error ? err.message : String(err)}`, 'error'),
    )

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.target !== e.currentTarget) return // a header cell handles its own keys
    const at = selected === null ? -1 : (position.get(selected) ?? -1)
    const row = at >= 0 ? rows[order[at]] : null
    const mod = e.metaKey || e.ctrlKey
    const step: Record<string, number> = { ArrowDown: at + 1, ArrowUp: at < 0 ? 0 : at - 1, PageDown: at + PAGE_ROWS, PageUp: at - PAGE_ROWS, Home: 0, End: order.length - 1 }
    if (e.key in step && !mod && !e.altKey) {
      e.preventDefault()
      select(step[e.key])
    } else if (e.key === 'Enter' && row?.command) {
      e.preventDefault()
      onCommand(row)
    } else if ((e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) && row) {
      e.preventDefault()
      const box = e.currentTarget.getBoundingClientRect()
      openMenu(row, box.left + 24, box.top + (at + 2) * ROW_HEIGHT - e.currentTarget.scrollTop)
    } else if (mod && e.key === 'c' && row) {
      e.preventDefault()
      void copy(tableToTSV(columns, rows, [order[at]]).split('\n')[1] ?? '', 'row')
    }
  }

  const activeId = selected !== null && position.has(selected) ? `ext-row-${position.get(selected)}` : undefined

  return (
    <div className="ext-table">
      <div
        className="grid-scroller ext-table-scroller"
        ref={setScroller}
        tabIndex={0}
        role="grid"
        aria-rowcount={order.length + 1}
        aria-colcount={columns.length}
        aria-activedescendant={activeId}
        onKeyDown={onKeyDown}
      >
        <div style={{ width: totalWidth, height: totalSize + ROW_HEIGHT, position: 'relative' }}>
          <div className="grid-row grid-header" role="row" aria-rowindex={1} style={{ gridTemplateColumns: template, width: totalWidth }}>
            {columns.map((c) => {
              const sorted = sort?.column === c.id
              const toggle = () => setUserSort(toggleSort(sort, c.id))
              return (
                <div
                  key={c.id}
                  className={`grid-cell header-cell ext-header-cell${sorted ? ' sorted' : ''}`}
                  role="columnheader"
                  aria-sort={ariaSort(sort, c.id)}
                  tabIndex={0}
                  title={`${c.title}\nClick to sort`}
                  style={{ textAlign: c.align === 'end' ? 'right' : c.align === 'center' ? 'center' : 'left' }}
                  onClick={toggle}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      toggle()
                    }
                  }}
                >
                  <span className="col-name">{c.title}</span>
                  {sorted && <span className="sort-mark">{sort?.descending ? '▼' : '▲'}</span>}
                </div>
              )
            })}
          </div>
          {items.map((vr) => {
            const row = rows[order[vr.index]]
            const isSelected = row.id === selected
            return (
              <div
                key={row.id}
                id={`ext-row-${vr.index}`}
                className={`grid-row ext-table-row${isSelected ? ' selected' : ''}${row.command ? ' runnable' : ''}`}
                role="row"
                aria-rowindex={vr.index + 2}
                aria-selected={isSelected}
                style={{ gridTemplateColumns: template, width: totalWidth, transform: `translateY(${vr.start + ROW_HEIGHT}px)` }}
                onClick={() => setSelected(row.id)}
                onDoubleClick={() => row.command && onCommand(row)}
                onContextMenu={(e) => onRowMenu(e, row)}
              >
                {columns.map((c, ci) => {
                  const text = cellText(c, row.cells[ci])
                  const severity = badgeSeverity(c, row.cells[ci])
                  return (
                    <div
                      key={c.id}
                      className="grid-cell"
                      role="gridcell"
                      title={text}
                      style={{ textAlign: c.align === 'end' ? 'right' : c.align === 'center' ? 'center' : 'left' }}
                    >
                      {c.type === 'badge' && text ? <span className={`ext-badge${severity ? ` ${severity}` : ''}`}>{text}</span> : text}
                    </div>
                  )
                })}
              </div>
            )
          })}
          {order.length === 0 && <div className="grid-empty muted" style={{ top: ROW_HEIGHT * 2 }}>No rows.</div>}
        </div>
      </div>
      <div className="ext-table-status muted">
        <span>
          {order.length.toLocaleString('en-US')} {order.length === 1 ? 'row' : 'rows'}
          {truncated ? ' (only the first 5,000 are shown)' : ''}
        </span>
        {order.length > 0 && (
          <button className="link" onClick={() => void copy(tableToTSV(columns, rows, order), 'the table')}>
            Copy
          </button>
        )}
      </div>
    </div>
  )
}
