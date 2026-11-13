// A table view of an extension (Go: internal/extensions/host/richviews.go) as
// the table component uses it: the cells' text, the order of the rows for a
// sort, and column widths. Go already typed every cell by its column (a
// string, a number, or null); nothing here trusts more than that.
// Framework-free.

export type ColumnType = 'text' | 'number' | 'date' | 'badge'

export interface TableColumn {
  id: string
  title: string
  type: string
  align: string
  badges?: Readonly<Record<string, string | undefined>> | null
}

export interface TableRow {
  id: string
  cells: readonly unknown[]
}

export interface TableSort {
  column: string
  descending?: boolean
}

const SEVERITIES = ['info', 'ok', 'warning', 'error']

/** The time of an ISO date or date-time in ms; NaN when it isn't one. A date-time without a zone is UTC, as in Go. */
export function dateValue(v: unknown): number {
  if (typeof v !== 'string') return NaN
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) return Date.parse(`${v}T00:00:00Z`)
  if (/^\d{4}-\d{2}-\d{2}T[\d:.]+$/.test(v)) return Date.parse(`${v}Z`)
  return Date.parse(v)
}

const pad = (n: number) => String(n).padStart(2, '0')

/** An ISO date as written, a date-time as "2026-11-01 09:30" in local time (UTC with utc). */
export function formatDate(v: string, utc = false): string {
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) return v
  const t = dateValue(v)
  if (Number.isNaN(t)) return ''
  const d = new Date(t)
  return utc
    ? `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
    : `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** A number with thousands separators and at most four decimals: 1,234,567.25. */
export function formatNumber(n: number): string {
  if (!Number.isFinite(n)) return ''
  const fixed = Math.abs(n) >= 1e15 ? n.toExponential(3) : String(Math.round(n * 1e4) / 1e4)
  if (fixed.includes('e')) return fixed
  const [whole, frac] = fixed.split('.')
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  return frac ? `${grouped}.${frac}` : grouped
}

/** The text a cell shows ('' for a missing value). */
export function cellText(col: TableColumn, v: unknown, utc = false): string {
  if (v === null || v === undefined) return ''
  if (col.type === 'number') return typeof v === 'number' ? formatNumber(v) : ''
  if (col.type === 'date') return typeof v === 'string' ? formatDate(v, utc) : ''
  return typeof v === 'string' ? v : typeof v === 'number' ? String(v) : ''
}

/** A badge cell's severity (info, ok, warning, error), '' for a value the column doesn't map. */
export function badgeSeverity(col: TableColumn, v: unknown): string {
  if (col.type !== 'badge' || typeof v !== 'string') return ''
  const s = col.badges && Object.prototype.hasOwnProperty.call(col.badges, v) ? col.badges[v] : undefined
  return s !== undefined && SEVERITIES.includes(s) ? s : ''
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

/** The value a cell sorts by: a number, text, or null for a missing or mistyped one. */
function sortKey(type: string, v: unknown): number | string | null {
  if (type === 'number') return typeof v === 'number' && Number.isFinite(v) ? v : null
  if (type === 'date') {
    const t = dateValue(v)
    return Number.isNaN(t) ? null : t
  }
  return typeof v === 'string' && v !== '' ? v : null
}

/**
 * The rows' indices in the order to show them: by the sort's column (numbers
 * and dates by value, text without case and with numbers in it by value),
 * missing values last either way, ties in the extension's order. Without a
 * sort, or for a column that isn't there, the extension's order.
 */
export function sortRows(columns: readonly TableColumn[], rows: readonly TableRow[], sort: TableSort | null | undefined): number[] {
  const order = rows.map((_, i) => i)
  const ci = sort ? columns.findIndex((c) => c.id === sort.column) : -1
  if (!sort || ci < 0) return order
  const type = columns[ci].type
  const keys = rows.map((r) => sortKey(type, r.cells[ci]))
  const dir = sort.descending ? -1 : 1
  return order.sort((a, b) => {
    const x = keys[a]
    const y = keys[b]
    if (x === null || y === null) return x === y ? a - b : x === null ? 1 : -1
    const c = typeof x === 'number' && typeof y === 'number' ? x - y : collator.compare(String(x), String(y))
    return c !== 0 ? c * dir : a - b
  })
}

/** A click on a column's header: ascending, then descending, then the extension's order again. */
export function toggleSort(sort: TableSort | null, column: string): TableSort | null {
  if (!sort || sort.column !== column) return { column, descending: false }
  return sort.descending ? null : { column, descending: true }
}

/** What a screen reader hears for a column's header (aria-sort). */
export function ariaSort(sort: TableSort | null, column: string): 'ascending' | 'descending' | 'none' {
  return sort?.column === column ? (sort.descending ? 'descending' : 'ascending') : 'none'
}

const CHAR = 7.3
const MIN_WIDTH = 60
const MAX_WIDTH = 360

/** Column widths in px, from the titles and the first rows' text. */
export function columnWidths(columns: readonly TableColumn[], rows: readonly TableRow[], sample = 200): number[] {
  return columns.map((col, ci) => {
    let chars = col.title.length + 3 // the sort mark
    for (let i = 0; i < rows.length && i < sample; i++) {
      chars = Math.max(chars, cellText(col, rows[i].cells[ci], true).length + (col.type === 'badge' ? 2 : 0))
    }
    return Math.round(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, chars * CHAR + 18)))
  })
}

/** The shown rows as tab-separated text with a header line, for the clipboard. */
export function tableToTSV(columns: readonly TableColumn[], rows: readonly TableRow[], order: readonly number[]): string {
  const clean = (s: string) => s.replace(/[\t\n\r]+/g, ' ')
  const lines = [columns.map((c) => clean(c.title)).join('\t')]
  for (const i of order) lines.push(columns.map((c, ci) => clean(cellText(c, rows[i].cells[ci]))).join('\t'))
  return lines.join('\n')
}
