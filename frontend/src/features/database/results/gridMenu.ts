import type { DropdownItem } from '../../../components/ui/Dropdown'
import { cellText } from '../../../lib/format'
import { excludeValue, isFiltered, onlyValue, setFilter, setSort, type GridView } from '../lib/gridView'
import { formatKeys } from '../../../lib/commands'

// Right-click menus of the result grid. Pure: the grid passes the clicked cell and what
// each item does, so the menus are testable without React.

export interface GridMenuActions {
  setView: (view: GridView) => void
  /** Opens the column's value filter list. */
  openFilter: (col: number) => void
  copy: (text: string, what: string) => void
}

export interface CellCopies {
  row: () => string
  rowJSON: () => string
  rowInsert: () => string
  column: () => string
  columnIn: () => string
}

/** Present when the cell can be edited. */
export interface CellEditActions {
  /** False for read-only columns: only the row can be deleted. */
  canEdit: boolean
  edit: () => void
  setNull: () => void
  toggleDelete: () => void
  deleted: boolean
}

export interface CellMenuInput {
  view: GridView
  col: number
  columnName: string
  /** The fetched value, which filters match. */
  value: unknown
  /** The value on screen (with a staged edit), which Copy Value copies; defaults to value. */
  shown?: unknown
  copies: CellCopies
  edit?: CellEditActions
}

const MAX_LABEL = 32

/** A value short enough for a menu label. */
export function shortValue(v: unknown): string {
  if (v === null || v === undefined) return 'NULL'
  const text = cellText(v).replace(/\s+/g, ' ')
  if (text === '') return "''"
  return text.length > MAX_LABEL ? `${text.slice(0, MAX_LABEL - 1)}…` : text
}

function filterItems(view: GridView, col: number, a: GridMenuActions): DropdownItem[] {
  return [
    { label: 'Filter Column Values…', onSelect: () => a.openFilter(col) },
    ...(view.filters[col] ? [{ label: 'Clear Column Filter', onSelect: () => a.setView(setFilter(view, col, null)) }] : []),
    ...(isFiltered(view) ? [{ label: 'Clear All Filters', onSelect: () => a.setView({ ...view, filters: {}, search: '' }) }] : []),
  ]
}

function sortItems(view: GridView, col: number, a: GridMenuActions): DropdownItem[] {
  const dir = view.sort.length === 1 && view.sort[0].col === col ? view.sort[0].dir : null
  return [
    { type: 'section', label: 'Sort' },
    { label: 'Sort Ascending', checked: dir === 'asc', onSelect: () => a.setView(setSort(view, col, 'asc')) },
    { label: 'Sort Descending', checked: dir === 'desc', onSelect: () => a.setView(setSort(view, col, 'desc')) },
    ...(view.sort.length ? [{ label: 'Clear Sort', onSelect: () => a.setView({ ...view, sort: [] }) }] : []),
  ]
}

export function cellMenu(input: CellMenuInput, a: GridMenuActions): DropdownItem[] {
  const { view, col, columnName, value, copies, edit } = input
  const label = shortValue(value)
  const onScreen = 'shown' in input ? input.shown : value
  const editItems: DropdownItem[] = edit
    ? [
        { type: 'section', label: 'Edit' },
        ...(edit.deleted || !edit.canEdit ? [] : [{ label: 'Edit Value', kbd: '↵', onSelect: () => edit.edit() }, { label: 'Set NULL', onSelect: () => edit.setNull() }]),
        { label: edit.deleted ? 'Restore Row' : 'Delete Row', kbd: formatKeys('Mod+Backspace'), danger: !edit.deleted, onSelect: () => edit.toggleDelete() },
      ]
    : []
  return [
    { type: 'section', label: `Filter · ${columnName}` },
    { label: `Show Only = ${label}`, onSelect: () => a.setView(onlyValue(view, col, value)) },
    { label: `Exclude ≠ ${label}`, onSelect: () => a.setView(excludeValue(view, col, value)) },
    ...filterItems(view, col, a),
    ...sortItems(view, col, a),
    { type: 'section', label: 'Copy' },
    { label: 'Copy Value', kbd: formatKeys('Mod+C'), onSelect: () => a.copy(cellText(onScreen), 'value') },
    { label: 'Copy Row', hint: 'TSV', onSelect: () => a.copy(copies.row(), 'row') },
    { label: 'Copy Row as JSON', onSelect: () => a.copy(copies.rowJSON(), 'row as JSON') },
    { label: 'Copy Row as INSERT', onSelect: () => a.copy(copies.rowInsert(), 'row as INSERT') },
    { label: 'Copy Column Values', hint: 'shown rows', onSelect: () => a.copy(copies.column(), 'column') },
    { label: 'Copy Column as IN (…)', hint: 'shown rows', onSelect: () => a.copy(copies.columnIn(), 'IN list') },
    ...editItems,
  ]
}

export function headerMenu(view: GridView, col: number, columnName: string, copyColumn: () => string, a: GridMenuActions): DropdownItem[] {
  return [
    ...sortItems(view, col, a).slice(1),
    { type: 'separator' },
    ...filterItems(view, col, a),
    { type: 'separator' },
    { label: 'Copy Column Name', onSelect: () => a.copy(columnName, 'column name') },
    { label: 'Copy Column Values', hint: 'shown rows', onSelect: () => a.copy(copyColumn(), 'column') },
  ]
}
