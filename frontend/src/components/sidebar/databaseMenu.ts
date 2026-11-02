import type { DropdownItem } from '../ui/Dropdown'
import type { Column, Connection, DatabaseInfo, Table } from '../../api/backend'
import { connectionUrl, countQuery, previewQuery, qualifiedTable, quoteIdent } from '../../lib/databases'
import {
  databaseSizeQuery,
  ddlStatement,
  deleteStatement,
  distinctValuesQuery,
  dropStatement,
  indexesQuery,
  insertStatement,
  nullRowsQuery,
  selectStatement,
  tableSizeQuery,
  tableSizesQuery,
  truncateStatement,
  updateStatement,
} from '../../lib/sqlScripts'

// Right-click menus of the Databases panel. Pure: the panel passes in what each item does,
// so the menus are testable without the stores behind them. Statements that change data
// (TRUNCATE, DROP, templates) only open in a console: nothing here runs them.

/** Puts text on the clipboard (and says so). */
type Copy = { copy: (text: string) => void }

/** Opens a SQL console with the statement. */
type Query = { query: (sql: string) => void }

export interface ConnectionMenuActions extends Copy {
  newConsole: () => void
  refresh: () => void
  edit: () => void
  remove: () => void
}

type ConnectionFields = Pick<Connection, 'host' | 'port' | 'user' | 'database' | 'name'> & { driver: string }

export function connectionMenu(conn: ConnectionFields, a: ConnectionMenuActions): DropdownItem[] {
  return [
    { label: 'New SQL Console', onSelect: () => a.newConsole() },
    { label: 'Refresh Databases', onSelect: () => a.refresh() },
    { label: 'Edit Connection…', onSelect: () => a.edit() },
    { type: 'separator' },
    { label: 'Copy Name', onSelect: () => a.copy(conn.name) },
    { label: 'Copy Connection URL', hint: 'no password', onSelect: () => a.copy(connectionUrl(conn)) },
    { label: 'Copy Host and Port', onSelect: () => a.copy(`${conn.host}:${conn.port}`) },
    { type: 'separator' },
    { label: 'Delete Connection…', danger: true, onSelect: () => a.remove() },
  ]
}

export interface DatabaseMenuActions extends Copy, Query {
  newConsole: () => void
  refresh: () => void
}

export function databaseMenu(driver: string, info: DatabaseInfo, a: DatabaseMenuActions): DropdownItem[] {
  const copyName: DropdownItem = { label: 'Copy Name', onSelect: () => a.copy(info.name) }
  // Without CONNECT there's nothing to open or load.
  if (!info.canConnect) return [copyName]
  return [
    { label: 'New SQL Console', onSelect: () => a.newConsole() },
    { label: 'Refresh Schema', onSelect: () => a.refresh() },
    { type: 'section', label: 'Information' },
    { label: 'Database Size', onSelect: () => a.query(databaseSizeQuery(driver, info.name)) },
    ...(driver === 'clickhouse' ? [{ label: 'Table Sizes', onSelect: () => a.query(tableSizesQuery(driver, info.name)) }] : []),
    { type: 'separator' },
    copyName,
  ]
}

export function schemaMenu(driver: string, name: string, a: Copy & Query): DropdownItem[] {
  return [
    { label: 'Table Sizes', onSelect: () => a.query(tableSizesQuery(driver, name)) },
    { type: 'separator' },
    { label: 'Copy Name', onSelect: () => a.copy(name) },
  ]
}

export interface TableMenuActions extends Copy, Query {
  /** Re-reads the database's schema (new columns, dropped tables). */
  refresh?: () => void
}

export function tableMenu(driver: string, t: Table, a: TableMenuActions): DropdownItem[] {
  const columns = t.columns.map((c) => quoteIdent(driver, c.name)).join(', ')
  const isView = t.kind.includes('view') || t.kind === 'dictionary'
  return [
    { label: 'Preview Data', hint: 'double-click', onSelect: () => a.query(previewQuery(driver, t)) },
    { label: 'Count Rows', onSelect: () => a.query(countQuery(driver, t)) },
    ...(a.refresh ? [{ label: 'Refresh Schema', onSelect: () => a.refresh?.() }] : []),
    { type: 'section', label: 'SQL Scripts' },
    { label: 'SELECT Statement', onSelect: () => a.query(selectStatement(driver, t)) },
    ...(isView
      ? []
      : [
          { label: 'INSERT Statement', onSelect: () => a.query(insertStatement(driver, t)) },
          { label: 'UPDATE Statement', onSelect: () => a.query(updateStatement(driver, t)) },
          { label: 'DELETE Statement', onSelect: () => a.query(deleteStatement(driver, t)) },
        ]),
    { label: driver === 'clickhouse' ? 'Show DDL' : 'Generate DDL', hint: driver === 'clickhouse' ? undefined : 'columns only', onSelect: () => a.query(ddlStatement(driver, t)) },
    { type: 'section', label: 'Information' },
    { label: 'Indexes', onSelect: () => a.query(indexesQuery(driver, t)) },
    ...(isView ? [] : [{ label: 'Table Size', onSelect: () => a.query(tableSizeQuery(driver, t)) }]),
    { type: 'section', label: 'Copy' },
    { label: 'Copy Name', onSelect: () => a.copy(t.name) },
    { label: 'Copy Qualified Name', onSelect: () => a.copy(qualifiedTable(driver, t)) },
    ...(columns ? [{ label: 'Copy Column Names', onSelect: () => a.copy(columns) }] : []),
    { label: 'Copy SELECT Statement', onSelect: () => a.copy(selectStatement(driver, t)) },
    { type: 'separator' },
    ...(isView ? [] : [{ label: 'Truncate…', hint: 'opens in console', danger: true, onSelect: () => a.query(truncateStatement(driver, t)) }]),
    { label: 'Drop…', hint: 'opens in console', danger: true, onSelect: () => a.query(dropStatement(driver, t)) },
  ]
}

export function columnMenu(driver: string, t: Pick<Table, 'schema' | 'name'>, c: Column, a: Copy & Query): DropdownItem[] {
  return [
    { label: 'Most Frequent Values', onSelect: () => a.query(distinctValuesQuery(driver, t, c.name)) },
    { label: 'Rows Where NULL', onSelect: () => a.query(nullRowsQuery(driver, t, c.name)) },
    { type: 'separator' },
    { label: 'Copy Name', onSelect: () => a.copy(c.name) },
    { label: 'Copy Qualified Name', onSelect: () => a.copy(`${qualifiedTable(driver, t)}.${quoteIdent(driver, c.name)}`) },
    { label: 'Copy Name and Type', onSelect: () => a.copy(`${c.name} ${c.type}`) },
  ]
}
