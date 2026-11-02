import type { MouseEvent } from 'react'
import type { QueryResult } from '../../../api/backend'
import type { GridView } from '../lib/gridView'
import type { TargetColumn } from '../lib/rowEdits'

interface Props {
  result: QueryResult
  view: GridView
  template: string
  width: number
  /** Per result column, the table column it edits (absent = not an editable grid). */
  targets?: (TargetColumn | null)[]
  onSort: (col: number, add: boolean) => void
  onFilter: (col: number, anchor: HTMLElement) => void
  onMenu: (col: number, e: MouseEvent) => void
}

const FUNNEL = (
  <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
    <path d="M1.5 2h13l-5 6v5l-3 1.5V8z" fill="currentColor" />
  </svg>
)

/**
 * The sticky header row: click sorts (Shift-click adds a secondary key), the funnel opens
 * the column's value filter, right-click offers both and copying.
 */
export function GridHeader({ result, view, template, width, targets, onSort, onFilter, onMenu }: Props) {
  const multi = view.sort.length > 1
  return (
    <div className="grid-row grid-header" style={{ gridTemplateColumns: template, width }}>
      <div className="grid-cell index">#</div>
      {result.columns.map((c, i) => {
        const target = targets?.[i]
        const readOnly = !!targets && !target?.editable
        const note = readOnly ? ` (read-only${target?.readOnlyReason ? `: ${target.readOnlyReason}` : ''})` : target?.primaryKey ? ' (primary key)' : ''
        const sortIndex = view.sort.findIndex((k) => k.col === i)
        const sort = sortIndex >= 0 ? view.sort[sortIndex] : null
        const filtered = !!view.filters[i]
        return (
          <div
            key={i}
            className={`grid-cell header-cell${readOnly ? ' readonly' : ''}${sort ? ' sorted' : ''}`}
            title={`${c.name} : ${c.type}${note}\nClick to sort, Shift-click to add a sort key`}
            onClick={(e) => onSort(i, e.shiftKey)}
            onContextMenu={(e) => onMenu(i, e)}
          >
            <span className="col-name">
              {target?.primaryKey && <span className="pk-mark">PK</span>}
              {c.name}
            </span>
            <span className="col-type">{c.type}</span>
            {sort && (
              <span className="sort-mark">
                {sort.dir === 'asc' ? '▲' : '▼'}
                {multi && <sub>{sortIndex + 1}</sub>}
              </span>
            )}
            <button
              className={`filter-button${filtered ? ' active' : ''}`}
              title={filtered ? 'Column is filtered: change the filter' : 'Filter by value'}
              onClick={(e) => {
                e.stopPropagation()
                onFilter(i, e.currentTarget)
              }}
            >
              {FUNNEL}
            </button>
          </div>
        )
      })}
    </div>
  )
}
