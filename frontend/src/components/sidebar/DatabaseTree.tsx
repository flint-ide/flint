import { useEffect, useState, type MouseEvent } from 'react'
import { errorMessage, type Connection, type DatabaseInfo, type Table } from '../../api/backend'
import { defaultDatabase, previewQuery, resolveDatabase, schemaKey } from '../../lib/databases'
import { useDatabase } from '../../store/database'
import { useScratch } from '../../store/scratch'
import { openContextMenu } from '../../store/contextMenu'
import { showToast } from '../../store/dialog'
import { DatabaseIcon } from '../icons/DatabaseIcon'
import type { DropdownItem } from '../ui/Dropdown'
import { columnMenu, databaseMenu, schemaMenu, tableMenu } from './databaseMenu'

/** Copies text for a "Copy …" item of the panel's menus and confirms it in a toast. */
export function copyText(text: string): void {
  navigator.clipboard.writeText(text).then(
    () => showToast(`Copied ${text.length > 60 ? `${text.slice(0, 57)}…` : text}`),
    (err: unknown) => showToast(`Copy failed: ${errorMessage(err)}`, 'error'),
  )
}

/** A right-click handler opening the menu built at click time. */
export function onMenu(build: () => DropdownItem[]) {
  return (e: MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    openContextMenu(e.clientX, e.clientY, build())
  }
}

/** "Databases (N)" under a connection: every database on the server, each browsable on its own. */
export function DatabasesNode({ conn }: { conn: Connection }) {
  const [open, setOpen] = useState(true)
  const entry = useDatabase((s) => s.databases[conn.id])
  const fallback = defaultDatabase(conn)
  const count = entry && !entry.loading && !entry.error ? ` (${entry.list.length})` : ''

  return (
    <>
      <div className="tree-row static indent-1" onClick={() => setOpen(!open)}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span className="schema-name">Databases{count}</span>
      </div>
      {open && entry?.loading && <div className="tree-row static muted indent-2">Loading databases…</div>}
      {open && entry?.error && (
        <>
          <div className="tree-row static error indent-2 selectable" title={entry.error}>{entry.error}</div>
          {/* The list is a convenience: the default database still works without it. */}
          <DatabaseNode conn={conn} info={{ name: fallback, canConnect: true, default: true }} />
        </>
      )}
      {open && entry && !entry.loading && !entry.error && entry.list.map((d) => <DatabaseNode key={d.name} conn={conn} info={d} />)}
    </>
  )
}

function DatabaseNode({ conn, info }: { conn: Connection; info: DatabaseInfo }) {
  const isDefault = info.default
  const [open, setOpen] = useState(isDefault && info.canConnect)
  // Consoles opened on the default database keep following the profile's default.
  const scratchDatabase = isDefault ? '' : info.name
  const database = resolveDatabase(conn, scratchDatabase)
  const schema = useDatabase((s) => s.schemas[schemaKey(conn.id, database)])
  const loadSchema = useDatabase((s) => s.loadSchema)
  const createScratch = useScratch((s) => s.create)

  useEffect(() => {
    if (open && info.canConnect) void loadSchema(conn.id, database)
  }, [open, info.canConnect, conn.id, database, loadSchema])

  const newConsole = () => void createScratch('sql', conn.id, undefined, scratchDatabase)
  const refresh = () => void loadSchema(conn.id, database, true)
  const menu = onMenu(() => databaseMenu(info, { newConsole, refresh, copy: copyText }))

  if (!info.canConnect) {
    return (
      <div className="tree-row static indent-2 db-row locked" title="No CONNECT privilege on this database" onContextMenu={menu}>
        <span className="twisty" />
        <DatabaseIcon locked />
        <span className="schema-name">{info.name}</span>
      </div>
    )
  }

  return (
    <div>
      <div
        className="tree-row static indent-2 db-row"
        onClick={() => setOpen(!open)}
        onContextMenu={menu}
        title={isDefault ? 'Default database of this connection' : undefined}
      >
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <DatabaseIcon />
        <span className={`schema-name${isDefault ? ' default-db' : ''}`}>{info.name}</span>
        <span className="row-actions" onClick={(e) => e.stopPropagation()}>
          <button className="icon" title="New SQL console in this database" onClick={newConsole}>⌨</button>
          <button className="icon" title="Refresh schema" onClick={refresh}>↻</button>
        </span>
      </div>
      {open && (
        <>
          {schema?.loading && <div className="tree-row static muted indent-3">Loading schema…</div>}
          {schema?.error && <div className="tree-row static error indent-3 selectable" title={schema.error}>{schema.error}</div>}
          {schema && !schema.loading && !schema.error && (
            <SchemaTree conn={conn} database={scratchDatabase} tables={schema.tables} />
          )}
        </>
      )}
    </div>
  )
}

/**
 * Tables of one database. Postgres groups them by schema; a ClickHouse database is its own
 * namespace, so its tables hang directly under the database.
 */
function SchemaTree({ conn, database, tables }: { conn: Connection; database: string; tables: Table[] }) {
  const [openSchemas, setOpenSchemas] = useState<Record<string, boolean>>({})
  const [openTables, setOpenTables] = useState<Record<string, boolean>>({})
  const createScratch = useScratch((s) => s.create)
  const flat = conn.driver === 'clickhouse'

  const bySchema = new Map<string, Table[]>()
  for (const t of tables) bySchema.set(t.schema, [...(bySchema.get(t.schema) ?? []), t])
  if (bySchema.size === 0) return <div className="tree-row static muted indent-3">No tables.</div>

  const query = (sql: string) => void createScratch('sql', conn.id, sql, database)
  const preview = (t: Table) => query(previewQuery(conn.driver, t))

  const tableRows = (list: Table[], depth: number) =>
    list.map((t) => {
      const key = `${t.schema}.${t.name}`
      const tOpen = !!openTables[key]
      return (
        <div key={key}>
          <div
            className={`tree-row static indent-${depth}`}
            title={`${t.kind} · double-click to preview`}
            onClick={() => setOpenTables({ ...openTables, [key]: !tOpen })}
            onDoubleClick={() => preview(t)}
            onContextMenu={onMenu(() => tableMenu(conn.driver, t, { query, copy: copyText }))}
          >
            <span className="twisty">{tOpen ? '▾' : '▸'}</span>
            <span className={`table-name ${t.kind.includes('view') ? 'view' : ''}`}>{t.name}</span>
          </div>
          {tOpen &&
            t.columns.map((c) => (
              <div
                key={c.name}
                className={`tree-row static indent-${depth + 1} column-row`}
                onContextMenu={onMenu(() => columnMenu(c, { copy: copyText }))}
              >
                <span className="col-name">{c.name}</span>
                <span className="col-type">{c.type}</span>
              </div>
            ))}
        </div>
      )
    })

  if (flat) return <>{tableRows(tables, 3)}</>

  return (
    <>
      {[...bySchema].map(([schemaName, list]) => {
        const sOpen = openSchemas[schemaName] ?? bySchema.size === 1
        return (
          <div key={schemaName}>
            <div
              className="tree-row static indent-3"
              onClick={() => setOpenSchemas({ ...openSchemas, [schemaName]: !sOpen })}
              onContextMenu={onMenu(() => schemaMenu(schemaName, { copy: copyText }))}
            >
              <span className="twisty">{sOpen ? '▾' : '▸'}</span>
              <span className="schema-name">{schemaName}</span>
              <span className="badge">{list.length}</span>
            </div>
            {sOpen && tableRows(list, 4)}
          </div>
        )
      })}
    </>
  )
}
