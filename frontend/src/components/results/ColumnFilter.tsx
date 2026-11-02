import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { distinctValues, isKeyShown, isNullKey, setFilter, type GridView } from '../../lib/gridView'

const WIDTH = 280
const MAX_LIST = 1000

interface Props {
  anchor: HTMLElement
  rows: readonly (readonly unknown[])[]
  col: number
  columnName: string
  view: GridView
  onChange: (view: GridView) => void
  onClose: () => void
}

/**
 * A column's local filter: its distinct values (among rows the other filters keep) with
 * counts and a checkbox each. Applies as you tick; nothing is sent to the database.
 */
export function ColumnFilter({ anchor, rows, col, columnName, view, onChange, onClose }: Props) {
  const box = useRef<HTMLDivElement>(null)
  const [query, setQuery] = useState('')
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  // The list is computed once per opening, so values don't vanish as you untick them.
  const values = useMemo(() => distinctValues(rows, col, view, MAX_LIST), [rows, col]) // eslint-disable-line react-hooks/exhaustive-deps
  const filter = view.filters[col]
  const needle = query.trim().toLowerCase()
  const visible = needle ? values.filter((v) => v.label.toLowerCase().includes(needle)) : values

  useLayoutEffect(() => {
    const r = anchor.getBoundingClientRect()
    setPos({ top: r.bottom + 4, left: Math.max(8, Math.min(r.left, window.innerWidth - WIDTH - 8)) })
  }, [anchor])

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node) && !anchor.contains(e.target as Node)) onClose()
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('mousedown', onDown, true)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown, true)
      window.removeEventListener('keydown', onKey)
    }
  }, [anchor, onClose])

  const shownKeys = () => values.filter((v) => isKeyShown(filter, v.key)).map((v) => v.key)

  const toggle = (key: string) => {
    const shown = new Set(shownKeys())
    if (shown.has(key)) shown.delete(key)
    else shown.add(key)
    const hidden = values.filter((v) => !shown.has(v.key)).map((v) => v.key)
    // Exclusions keep working for values beyond the list (and new rows of a re-run).
    onChange(setFilter(view, col, hidden.length ? { mode: 'notIn', keys: hidden } : null))
  }

  /** Shows exactly the values matching the search box (or all of them). */
  const selectVisible = () => {
    if (!needle) onChange(setFilter(view, col, null))
    else onChange(setFilter(view, col, { mode: 'in', keys: visible.map((v) => v.key) }))
  }
  const selectNone = () => onChange(setFilter(view, col, { mode: 'in', keys: [] }))

  if (!pos) return null
  return createPortal(
    <div className="column-filter" ref={box} style={{ top: pos.top, left: pos.left, width: WIDTH }}>
      <div className="column-filter-head">
        <span className="column-filter-title">Filter {columnName}</span>
        <span className="muted">local</span>
      </div>
      <input
        className="column-filter-search"
        autoFocus
        placeholder="Search values…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && selectVisible()}
      />
      <div className="column-filter-actions">
        <button className="link" onClick={selectVisible} title={needle ? 'Show only the values matching the search (↵)' : 'Show every value'}>
          {needle ? 'Only matching' : 'Select all'}
        </button>
        <button className="link" onClick={selectNone}>Select none</button>
        <span className="spacer" />
        {filter && <button className="link" onClick={() => onChange(setFilter(view, col, null))}>Clear</button>}
      </div>
      <div className="column-filter-list">
        {visible.length === 0 && <div className="muted column-filter-empty">No values</div>}
        {visible.map((v) => (
          <label key={v.key} className={`column-filter-item${isNullKey(v.key) || v.key === '' ? ' special' : ''}`} title={v.label}>
            <input type="checkbox" checked={isKeyShown(filter, v.key)} onChange={() => toggle(v.key)} />
            <span className="column-filter-label">{v.label}</span>
            <span className="column-filter-count">{v.count.toLocaleString()}</span>
          </label>
        ))}
        {values.length >= MAX_LIST && <div className="muted column-filter-empty">Showing the {MAX_LIST} most frequent values</div>}
      </div>
    </div>,
    document.body,
  )
}
