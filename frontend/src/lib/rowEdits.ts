// Staged row edits of a result grid: cell changes, deleted rows and new rows are kept
// here (immutably) until Submit sends them to Go as one batch. Values are the text a
// user would type in a SQL literal; null is SQL NULL. Framework-free for node:test.
import { cellText } from './format'

/** A staged cell value: literal text, or null for SQL NULL. */
export type CellValue = string | null

export interface StagedEdits {
  /** Changed cells of existing rows: row index → column index → value. */
  updates: Readonly<Record<number, Readonly<Record<number, CellValue>>>>
  /** Existing rows marked for deletion. */
  deleted: Readonly<Record<number, true>>
  /** New rows, shown after the result's rows; unset columns get their default. */
  inserted: readonly Readonly<Record<number, CellValue>>[]
}

export const noEdits: StagedEdits = { updates: {}, deleted: {}, inserted: [] }

/** The parts of a result the edit model needs. */
export interface EditableResult {
  columns: { name: string }[]
  rows: unknown[][]
  attnums?: number[]
}

/** The parts of db.EditColumn / db.EditTarget the model needs. */
export interface TargetColumn {
  attnum: number
  name: string
  kind: string
  notNull: boolean
  hasDefault: boolean
  primaryKey: boolean
  editable: boolean
  readOnlyReason?: string
}

export interface Target {
  tableOid: number
  table: string
  key: string[]
  columns: TargetColumn[]
}

/** One change in the shape Go's db.RowEdit expects. */
export interface RowEditPayload {
  op: 'update' | 'insert' | 'delete'
  key: Record<string, CellValue>
  values: Record<string, CellValue>
}

/** How a result maps onto its table: per result column, the table column (or null). */
export interface Editability {
  editable: boolean
  reason: string
  columns: (TargetColumn | null)[]
}

/**
 * Decides whether a result can be edited: it must come from one table with a primary
 * key whose columns are all in the result, on a connection that isn't read-only.
 */
export function editability(result: EditableResult, target: Target | null, readOnly: boolean): Editability {
  const none = (reason: string): Editability => ({ editable: false, reason, columns: result.columns.map(() => null) })
  if (readOnly) return none('the connection is read-only')
  if (!target) return none('the result doesn’t come from a single table')
  if (target.key.length === 0) return none(`${target.table} has no primary key`)
  const byAttnum = new Map(target.columns.map((c) => [c.attnum, c]))
  const columns = result.columns.map((_, i) => byAttnum.get(result.attnums?.[i] ?? 0) ?? null)
  const present = new Set(columns.filter((c) => c !== null).map((c) => c.name))
  const missing = target.key.filter((k) => !present.has(k))
  if (missing.length) return { editable: false, reason: `primary key column ${missing.join(', ')} isn’t in the result`, columns }
  return { editable: true, reason: '', columns }
}

/** The text form of a result value, as staged and sent to Go. */
export function toCellValue(v: unknown): CellValue {
  if (v === null || v === undefined) return null
  return cellText(v)
}

/** The value a grid cell shows: staged if changed, else the original. */
export function displayValue(result: EditableResult, edits: StagedEdits, row: number, col: number): unknown {
  const base = result.rows.length
  if (row >= base) {
    const r = edits.inserted[row - base]
    return r && col in r ? r[col] : undefined // undefined = DEFAULT
  }
  const u = edits.updates[row]
  return u && col in u ? u[col] : result.rows[row][col]
}

export function isCellChanged(result: EditableResult, edits: StagedEdits, row: number, col: number): boolean {
  if (row >= result.rows.length) return col in (edits.inserted[row - result.rows.length] ?? {})
  return col in (edits.updates[row] ?? {})
}

/** Stages a cell value; setting an existing row's cell back to its original drops the change. */
export function setCell(result: EditableResult, edits: StagedEdits, row: number, col: number, value: CellValue): StagedEdits {
  const base = result.rows.length
  if (row >= base) {
    const i = row - base
    if (i >= edits.inserted.length) return edits
    const inserted = edits.inserted.map((r, j) => (j === i ? { ...r, [col]: value } : r))
    return { ...edits, inserted }
  }
  const current = { ...(edits.updates[row] ?? {}) }
  if (value === toCellValue(result.rows[row][col])) delete current[col]
  else current[col] = value
  const updates = { ...edits.updates }
  if (Object.keys(current).length) updates[row] = current
  else delete updates[row]
  return { ...edits, updates }
}

export function addRow(edits: StagedEdits): StagedEdits {
  return { ...edits, inserted: [...edits.inserted, {}] }
}

/** Marks an existing row deleted (again: restores it); a new row is simply dropped. */
export function toggleDelete(result: EditableResult, edits: StagedEdits, row: number): StagedEdits {
  const base = result.rows.length
  if (row >= base) return { ...edits, inserted: edits.inserted.filter((_, j) => j !== row - base) }
  const deleted = { ...edits.deleted }
  if (deleted[row]) delete deleted[row]
  else deleted[row] = true
  return { ...edits, deleted }
}

export function changeCount(edits: StagedEdits): number {
  const deleted = new Set(Object.keys(edits.deleted))
  const updated = Object.keys(edits.updates).filter((r) => !deleted.has(r)).length
  return updated + deleted.size + edits.inserted.length
}

/**
 * The batch for Go, in a stable order: updates and deletes by row, then inserts. Rows are
 * identified by their original primary key; an updated row that is also deleted is
 * only deleted.
 */
export function buildBatch(result: EditableResult, target: Target, edits: StagedEdits): { tableOid: number; edits: RowEditPayload[] } {
  const { editable, reason, columns } = editability(result, target, false)
  if (!editable) throw new Error(`Rows can’t be edited: ${reason}`)
  const keyOf = (row: number): Record<string, CellValue> => {
    const key: Record<string, CellValue> = {}
    columns.forEach((c, i) => {
      if (c?.primaryKey) key[c.name] = toCellValue(result.rows[row][i])
    })
    return key
  }
  const valuesOf = (cells: Readonly<Record<number, CellValue>>): Record<string, CellValue> => {
    const values: Record<string, CellValue> = {}
    for (const [col, v] of Object.entries(cells)) {
      const c = columns[Number(col)]
      if (!c) throw new Error(`Column ${result.columns[Number(col)]?.name ?? col} can’t be edited`)
      values[c.name] = v
    }
    return values
  }
  const rows = [...new Set([...Object.keys(edits.updates), ...Object.keys(edits.deleted)].map(Number))].sort((a, b) => a - b)
  const out: RowEditPayload[] = []
  for (const row of rows) {
    if (edits.deleted[row]) out.push({ op: 'delete', key: keyOf(row), values: {} })
    else out.push({ op: 'update', key: keyOf(row), values: valuesOf(edits.updates[row]) })
  }
  for (const cells of edits.inserted) out.push({ op: 'insert', key: {}, values: valuesOf(cells) })
  return { tableOid: target.tableOid, edits: out }
}

/** Validates typed text for a column kind; returns an error message or ''. */
export function validateCell(kind: string, value: CellValue, column?: Pick<TargetColumn, 'notNull' | 'name'>): string {
  if (value === null) return column?.notNull ? `${column.name} can’t be NULL` : ''
  switch (kind) {
    case 'number':
      return /^\s*[-+]?(\d+(\.\d*)?|\.\d+)([eE][-+]?\d+)?\s*$/.test(value) || /^\s*[-+]?(Infinity|NaN)\s*$/i.test(value)
        ? ''
        : 'Not a number'
    case 'bool':
      return /^(true|false|t|f|yes|no|on|off|1|0)$/i.test(value.trim()) ? '' : 'Expected true or false'
    case 'json':
      try {
        JSON.parse(value)
        return ''
      } catch (err) {
        return `Invalid JSON: ${err instanceof Error ? err.message : String(err)}`
      }
  }
  return ''
}
