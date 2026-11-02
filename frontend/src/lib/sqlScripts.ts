// Statements generated from schema metadata for the Databases panel's menus and the
// result grid's "Copy as …" items. Pure strings; nothing here runs SQL.
import type { Table } from '../api/backend'
import { qualifiedTable, quoteIdent } from './databases'

type TableRef = Pick<Table, 'schema' | 'name'>
type TableWithColumns = TableRef & Pick<Table, 'columns'>

/**
 * A value as a SQL literal: NULL, bare numbers and booleans, everything else a quoted
 * string (JSON documents serialized). ClickHouse also treats backslash as an escape.
 */
export function sqlLiteral(driver: string, v: unknown): string {
  if (v === null || v === undefined) return 'NULL'
  if (typeof v === 'number' && Number.isFinite(v)) return String(v)
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  const text = typeof v === 'object' ? JSON.stringify(v) : String(v)
  const escaped = driver === 'clickhouse' ? text.replaceAll('\\', '\\\\') : text
  return `'${escaped.replaceAll("'", "''")}'`
}

const columnList = (driver: string, t: TableWithColumns, sep = ', ') => t.columns.map((c) => quoteIdent(driver, c.name)).join(sep)

/** SELECT listing every column instead of *. */
export function selectStatement(driver: string, t: TableWithColumns): string {
  const cols = t.columns.length ? columnList(driver, t, ',\n       ') : '*'
  return `SELECT ${cols}\nFROM ${qualifiedTable(driver, t)}\nLIMIT 100;\n`
}

/** INSERT template: one NULL placeholder per column, its name and type in a comment. */
export function insertStatement(driver: string, t: TableWithColumns): string {
  const values = t.columns.map((c, i) => `    NULL${i < t.columns.length - 1 ? ',' : ''} -- ${c.name} ${c.type}`).join('\n')
  return `INSERT INTO ${qualifiedTable(driver, t)} (${columnList(driver, t)})\nVALUES (\n${values}\n);\n`
}

/** UPDATE template (ClickHouse: ALTER TABLE … UPDATE, a mutation); WHERE false until edited. */
export function updateStatement(driver: string, t: TableWithColumns): string {
  const sets = t.columns.map((c) => `    ${quoteIdent(driver, c.name)} = NULL`).join(',\n')
  if (driver === 'clickhouse') {
    return `-- Remove the key columns (ORDER BY, PARTITION BY): ClickHouse can't update them.\nALTER TABLE ${qualifiedTable(driver, t)}\nUPDATE\n${sets}\nWHERE false;\n`
  }
  return `UPDATE ${qualifiedTable(driver, t)}\nSET\n${sets}\nWHERE false;\n`
}

/** DELETE template; WHERE false until edited. */
export function deleteStatement(driver: string, t: TableRef): string {
  return `DELETE FROM ${qualifiedTable(driver, t)}\nWHERE false;\n`
}

/**
 * The table's DDL. ClickHouse prints the real one; Postgres has no built-in, so this is a
 * CREATE TABLE rebuilt from the columns (without keys, defaults or indexes, as it says).
 */
export function ddlStatement(driver: string, t: TableWithColumns): string {
  if (driver === 'clickhouse') return `SHOW CREATE TABLE ${qualifiedTable(driver, t)};\n`
  const cols = t.columns.map((c) => `    ${quoteIdent(driver, c.name)} ${c.type}`).join(',\n')
  return `-- Rebuilt from the column list: constraints, defaults and indexes aren't included.\nCREATE TABLE ${qualifiedTable(driver, t)} (\n${cols}\n);\n`
}

const tableFilter = (driver: string, schemaCol: string, tableCol: string, t: TableRef) =>
  `${schemaCol} = ${sqlLiteral(driver, t.schema)} AND ${tableCol} = ${sqlLiteral(driver, t.name)}`

/** The table's indexes (ClickHouse: its data skipping indexes). */
export function indexesQuery(driver: string, t: TableRef): string {
  if (driver === 'clickhouse') {
    return `SELECT name, type_full, expr, granularity\nFROM system.data_skipping_indices\nWHERE ${tableFilter(driver, 'database', 'table', t)};\n`
  }
  return `SELECT indexname, indexdef\nFROM pg_indexes\nWHERE ${tableFilter(driver, 'schemaname', 'tablename', t)};\n`
}

/** The table's size on disk. */
export function tableSizeQuery(driver: string, t: TableRef): string {
  if (driver === 'clickhouse') {
    return `SELECT formatReadableSize(sum(bytes_on_disk)) AS size, sum(rows) AS rows, count() AS parts\nFROM system.parts\nWHERE active AND ${tableFilter(driver, 'database', 'table', t)};\n`
  }
  const rel = sqlLiteral(driver, qualifiedTable(driver, t))
  return `SELECT pg_size_pretty(pg_total_relation_size(${rel})) AS total,\n       pg_size_pretty(pg_relation_size(${rel})) AS data,\n       pg_size_pretty(pg_indexes_size(${rel})) AS indexes;\n`
}

/** Tables of a ClickHouse database or a Postgres schema, biggest first. */
export function tableSizesQuery(driver: string, name: string): string {
  if (driver === 'clickhouse') {
    return `SELECT table, formatReadableSize(sum(bytes_on_disk)) AS size, sum(rows) AS rows\nFROM system.parts\nWHERE active AND database = ${sqlLiteral(driver, name)}\nGROUP BY table\nORDER BY sum(bytes_on_disk) DESC;\n`
  }
  return `SELECT c.relname AS table, pg_size_pretty(pg_total_relation_size(c.oid)) AS size, c.reltuples::bigint AS approx_rows\nFROM pg_class c\nJOIN pg_namespace n ON n.oid = c.relnamespace\nWHERE n.nspname = ${sqlLiteral(driver, name)} AND c.relkind IN ('r', 'p', 'm')\nORDER BY pg_total_relation_size(c.oid) DESC;\n`
}

/** Size of the database the console is connected to. */
export function databaseSizeQuery(driver: string, name: string): string {
  if (driver === 'clickhouse') {
    return `SELECT formatReadableSize(sum(bytes_on_disk)) AS size, sum(rows) AS rows\nFROM system.parts\nWHERE active AND database = ${sqlLiteral(driver, name)};\n`
  }
  return `SELECT pg_size_pretty(pg_database_size(current_database())) AS size;\n`
}

/** Most frequent values of a column. */
export function distinctValuesQuery(driver: string, t: TableRef, column: string): string {
  const c = quoteIdent(driver, column)
  return `SELECT ${c}, count(*) AS count\nFROM ${qualifiedTable(driver, t)}\nGROUP BY ${c}\nORDER BY count DESC\nLIMIT 100;\n`
}

/** Rows where a column is NULL. */
export function nullRowsQuery(driver: string, t: TableRef, column: string): string {
  return `SELECT *\nFROM ${qualifiedTable(driver, t)}\nWHERE ${quoteIdent(driver, column)} IS NULL\nLIMIT 100;\n`
}

/** Removes every row; opened in a console for review, never run for you. */
export function truncateStatement(driver: string, t: TableRef): string {
  return `TRUNCATE TABLE ${qualifiedTable(driver, t)};\n`
}

/** Drops the table or view; opened in a console for review, never run for you. */
export function dropStatement(driver: string, t: TableRef & Pick<Table, 'kind'>): string {
  return `DROP ${dropKind(driver, t.kind)} ${qualifiedTable(driver, t)};\n`
}

function dropKind(driver: string, kind: string): string {
  if (kind === 'materialized view') return driver === 'clickhouse' ? 'VIEW' : 'MATERIALIZED VIEW'
  if (kind.includes('view')) return 'VIEW'
  if (kind === 'dictionary') return 'DICTIONARY'
  return 'TABLE'
}

/** INSERT of one result row with its values; table is already qualified/quoted. */
export function insertRowStatement(driver: string, table: string, columns: readonly string[], row: readonly unknown[]): string {
  const cols = columns.map((c) => quoteIdent(driver, c)).join(', ')
  return `INSERT INTO ${table} (${cols}) VALUES (${row.map((v) => sqlLiteral(driver, v)).join(', ')});`
}

/** A column's distinct values as an IN list, e.g. for a follow-up WHERE; NULL never matches IN, so it's left out. */
export function inList(driver: string, values: readonly unknown[]): string {
  const unique = [...new Set(values.filter((v) => v !== null && v !== undefined).map((v) => sqlLiteral(driver, v)))]
  return `(${unique.join(', ')})`
}
