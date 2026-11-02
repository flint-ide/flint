import type { DropdownItem } from '../ui/Dropdown'
import type { Column, Connection, DatabaseInfo, Table } from '../../api/backend'
import { connectionUrl, countQuery, previewQuery, qualifiedTable, quoteIdent } from '../../lib/databases'

// Right-click menus of the Databases panel. Pure: the panel passes in what each item does,
// so the menus are testable without the stores behind them.

/** Puts text on the clipboard (and says so). */
type Copy = { copy: (text: string) => void }

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
    { type: 'separator' },
    { label: 'Delete Connection…', danger: true, onSelect: () => a.remove() },
  ]
}

export interface DatabaseMenuActions extends Copy {
  newConsole: () => void
  refresh: () => void
}

export function databaseMenu(info: DatabaseInfo, a: DatabaseMenuActions): DropdownItem[] {
  const copyName: DropdownItem = { label: 'Copy Name', onSelect: () => a.copy(info.name) }
  // Without CONNECT there's nothing to open or load.
  if (!info.canConnect) return [copyName]
  return [
    { label: 'New SQL Console', onSelect: () => a.newConsole() },
    { label: 'Refresh Schema', onSelect: () => a.refresh() },
    { type: 'separator' },
    copyName,
  ]
}

export function schemaMenu(name: string, a: Copy): DropdownItem[] {
  return [{ label: 'Copy Name', onSelect: () => a.copy(name) }]
}

export interface TableMenuActions extends Copy {
  /** Opens a SQL console with the statement. */
  query: (sql: string) => void
}

export function tableMenu(driver: string, t: Table, a: TableMenuActions): DropdownItem[] {
  const columns = t.columns.map((c) => quoteIdent(driver, c.name)).join(', ')
  return [
    { label: 'Preview Data', hint: 'double-click', onSelect: () => a.query(previewQuery(driver, t)) },
    { label: 'Count Rows', onSelect: () => a.query(countQuery(driver, t)) },
    { type: 'separator' },
    { label: 'Copy Name', onSelect: () => a.copy(t.name) },
    { label: 'Copy Qualified Name', onSelect: () => a.copy(qualifiedTable(driver, t)) },
    ...(columns ? [{ label: 'Copy Column Names', onSelect: () => a.copy(columns) }] : []),
  ]
}

export function columnMenu(c: Column, a: Copy): DropdownItem[] {
  return [
    { label: 'Copy Name', onSelect: () => a.copy(c.name) },
    { label: 'Copy Name and Type', onSelect: () => a.copy(`${c.name} ${c.type}`) },
  ]
}
