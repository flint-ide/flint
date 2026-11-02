// Client-side view of a result grid: sorting and per-value filters applied to the rows
// already fetched, without re-running the query (like a "local filter" in a DB IDE).
// Pure and framework-free so node:test can cover it; the grid keeps the state.
import { cellText } from './format'

export type SortDir = 'asc' | 'desc'

export interface SortKey {
  col: number
  dir: SortDir
}

/** Rows whose value key is (in) or isn't (notIn) one of keys. */
export interface ColumnFilter {
  mode: 'in' | 'notIn'
  keys: readonly string[]
}

export interface GridView {
  /** Sort keys by priority (first = primary). */
  sort: readonly SortKey[]
  /** Column index → its value filter. */
  filters: Readonly<Record<number, ColumnFilter>>
  /** Case-insensitive text every shown row must contain in some cell. */
  search: string
}

export const emptyView: GridView = { sort: [], filters: {}, search: '' }

type Rows = readonly (readonly unknown[])[]

const NULL_KEY = '\u0000NULL'

/** The identity of a value for filtering: its text, with NULL kept apart from 'NULL'. */
export function valueKey(v: unknown): string {
  return v === null || v === undefined ? NULL_KEY : cellText(v)
}

export function isNullKey(key: string): boolean {
  return key === NULL_KEY
}

/** True when the view hides or reorders nothing. */
export function isIdentity(view: GridView): boolean {
  return view.sort.length === 0 && Object.keys(view.filters).length === 0 && view.search.trim() === ''
}

/** True when some rows may be hidden. */
export function isFiltered(view: GridView): boolean {
  return Object.keys(view.filters).length > 0 || view.search.trim() !== ''
}

const NUMERIC = /^[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$/

/** Orders two non-null values: numbers (also numeric text, e.g. big ints) by value, else naturally. */
export function compareValues(a: unknown, b: unknown): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b
  if (typeof a === 'boolean' && typeof b === 'boolean') return Number(a) - Number(b)
  const sa = cellText(a)
  const sb = cellText(b)
  if (NUMERIC.test(sa) && NUMERIC.test(sb)) {
    const d = Number(sa) - Number(sb)
    if (d !== 0) return d
  }
  return sa < sb ? -1 : sa > sb ? 1 : 0
}

function compareCells(a: unknown, b: unknown, dir: SortDir): number {
  const an = a === null || a === undefined
  const bn = b === null || b === undefined
  // NULLs last when ascending, first when descending (Postgres' default).
  if (an || bn) return an === bn ? 0 : (an ? 1 : -1) * (dir === 'asc' ? 1 : -1)
  const c = compareValues(a, b)
  return dir === 'asc' ? c : -c
}

function matchesFilter(row: readonly unknown[], col: number, f: ColumnFilter): boolean {
  const hit = f.keys.includes(valueKey(row[col]))
  return f.mode === 'in' ? hit : !hit
}

function matchesSearch(row: readonly unknown[], needle: string): boolean {
  return row.some((v) => (v === null ? 'null' : cellText(v).toLowerCase()).includes(needle))
}

function rowMatches(row: readonly unknown[], view: GridView, skipCol: number): boolean {
  for (const [c, f] of Object.entries(view.filters)) {
    const col = Number(c)
    if (col !== skipCol && !matchesFilter(row, col, f)) return false
  }
  const needle = view.search.trim().toLowerCase()
  return needle === '' || matchesSearch(row, needle)
}

/** Indices of rows shown by the view, in display order (a stable sort). */
export function applyView(rows: Rows, view: GridView): number[] {
  const indices: number[] = []
  for (let i = 0; i < rows.length; i++) if (rowMatches(rows[i], view, -1)) indices.push(i)
  if (view.sort.length === 0) return indices
  return indices.sort((x, y) => {
    for (const k of view.sort) {
      const c = compareCells(rows[x][k.col], rows[y][k.col], k.dir)
      if (c !== 0) return c
    }
    return x - y
  })
}

export interface DistinctValue {
  key: string
  label: string
  count: number
}

/**
 * Distinct values of a column among the rows the other filters (and the search) let
 * through, most frequent first: what the column's filter list offers.
 */
export function distinctValues(rows: Rows, col: number, view: GridView, limit = 1000): DistinctValue[] {
  const counts = new Map<string, number>()
  for (const row of rows) {
    if (!rowMatches(row, view, col)) continue
    const key = valueKey(row[col])
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  return [...counts]
    .map(([key, count]) => ({ key, label: isNullKey(key) ? 'NULL' : key === '' ? '(empty)' : key, count }))
    .sort((a, b) => b.count - a.count || compareValues(a.key, b.key))
    .slice(0, limit)
}

/**
 * The next sort after a header click: cycles the column asc → desc → off. With add
 * (Shift-click) other columns keep sorting; without it the column becomes the only key.
 */
export function toggleSort(view: GridView, col: number, add: boolean): GridView {
  const current = view.sort.find((k) => k.col === col)
  const next: SortKey | null = !current ? { col, dir: 'asc' } : current.dir === 'asc' ? { col, dir: 'desc' } : null
  if (!add) return { ...view, sort: next ? [next] : [] }
  const others = view.sort.filter((k) => k.col !== col)
  if (!next) return { ...view, sort: others }
  return { ...view, sort: current ? view.sort.map((k) => (k.col === col ? next : k)) : [...others, next] }
}

/** Sorts by one column only (or clears the sort when dir is null). */
export function setSort(view: GridView, col: number, dir: SortDir | null): GridView {
  return { ...view, sort: dir ? [{ col, dir }] : [] }
}

/** Replaces a column's filter; null (or an empty "not in") removes it. */
export function setFilter(view: GridView, col: number, filter: ColumnFilter | null): GridView {
  const rest = Object.fromEntries(Object.entries(view.filters).filter(([c]) => Number(c) !== col))
  if (!filter || (filter.mode === 'notIn' && filter.keys.length === 0)) return { ...view, filters: rest }
  return { ...view, filters: { ...rest, [col]: filter } }
}

/** Keeps only rows whose column has this value (narrowing an existing "in" filter's choice to it). */
export function onlyValue(view: GridView, col: number, value: unknown): GridView {
  return setFilter(view, col, { mode: 'in', keys: [valueKey(value)] })
}

/** Hides rows whose column has this value, on top of the values already excluded. */
export function excludeValue(view: GridView, col: number, value: unknown): GridView {
  const key = valueKey(value)
  const f = view.filters[col]
  if (f?.mode === 'in') return setFilter(view, col, { mode: 'in', keys: f.keys.filter((k) => k !== key) })
  const keys = f ? f.keys : []
  return setFilter(view, col, { mode: 'notIn', keys: keys.includes(key) ? keys : [...keys, key] })
}

/** Whether a distinct value is currently shown by the column's filter. */
export function isKeyShown(filter: ColumnFilter | undefined, key: string): boolean {
  if (!filter) return true
  return filter.mode === 'in' ? filter.keys.includes(key) : !filter.keys.includes(key)
}
