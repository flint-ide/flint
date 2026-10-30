import type { QueryResult } from '../api/backend'

const MIN_COL = 60
const MAX_COL = 420
const CHAR_PX = 7.4
const SAMPLE_ROWS = 200

/** Renders a cell value as single-line text. */
export function cellText(v: unknown): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

/** Estimates column widths from the header and a sample of rows. */
export function columnWidths(result: Pick<QueryResult, 'columns' | 'rows'>): number[] {
  return result.columns.map((col, ci) => {
    let chars = Math.max(col.name.length, col.type.length * 0.8)
    const n = Math.min(result.rows.length, SAMPLE_ROWS)
    for (let r = 0; r < n; r++) {
      chars = Math.max(chars, cellText(result.rows[r][ci]).length)
    }
    return Math.round(Math.min(MAX_COL, Math.max(MIN_COL, chars * CHAR_PX + 20)))
  })
}

/** Tab-separated export with a header row; tabs/newlines inside values are escaped. */
export function toTSV(result: Pick<QueryResult, 'columns' | 'rows'>): string {
  const esc = (s: string) => s.replace(/\\/g, '\\\\').replace(/\t/g, '\\t').replace(/\n/g, '\\n')
  const lines = [result.columns.map((c) => esc(c.name)).join('\t')]
  for (const row of result.rows) {
    lines.push(row.map((v) => (v === null ? 'NULL' : esc(cellText(v)))).join('\t'))
  }
  return lines.join('\n')
}

/** Human-friendly duration for query timings. */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)} s`
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`
}
