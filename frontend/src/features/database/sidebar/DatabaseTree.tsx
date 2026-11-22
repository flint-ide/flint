import { IconButton, TreeRow } from '@flint-ide/ui'
import { RefreshCw, SquareTerminal } from '@flint-ide/ui/icons'
import { useEffect, useState, type MouseEvent } from 'react'
import { errorMessage, type Connection, type DatabaseInfo, type Table } from '../../../api/backend'
import { defaultDatabase, previewQuery, resolveDatabase, schemaKey } from '../lib/databases'
import { useDatabase } from '../store/database'
import { useScratch } from '../../../store/scratch'
import { openContextMenu } from '../../../store/contextMenu'
import { showToast } from '../../../store/dialog'
import { DatabaseIcon } from '../../../components/icons/DatabaseIcon'
import type { DropdownItem } from '../../../components/ui/Dropdown'
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
      <TreeRow depth={1} twisty={open ? 'open' : 'closed'} label={`Databases${count}`} onClick={() => setOpen(!open)} />
      {open && entry?.loading && <TreeRow depth={2} className="muted" label="Loading databases…" />}
      {open && entry?.error && (
        <>
          <TreeRow depth={2} className="error selectable" title={entry.error} label={entry.error} />
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
  const query = (sql: string) => void createScratch('sql', conn.id, sql, scratchDatabase)
  const menu = onMenu(() => databaseMenu(conn.driver, info, { newConsole, refresh, query, copy: copyText }))

  if (!info.canConnect) {
    return (
      <TreeRow
        depth={2}
        twisty="none"
        // Not clickable: dimmed with a plain cursor.
        style={{ color: 'var(--fg-faint)', cursor: 'default' }}
        title="No CONNECT privilege on this database"
        onContextMenu={menu}
        icon={<DatabaseIcon locked />}
        label={info.name}
      />
    )
  }

  return (
    <div>
      <TreeRow
        depth={2}
        twisty={open ? 'open' : 'closed'}
        onClick={() => setOpen(!open)}
        onContextMenu={menu}
        title={isDefault ? 'Default database of this connection' : undefined}
        icon={<DatabaseIcon />}
        label={isDefault ? <strong>{info.name}</strong> : info.name}
        actions={
          <>
            <IconButton variant="ghost" size="sm" aria-label="New SQL console in this database" title="New SQL console in this database" onClick={newConsole}><SquareTerminal /></IconButton>
            <IconButton variant="ghost" size="sm" aria-label="Refresh schema" title="Refresh schema" onClick={refresh}><RefreshCw /></IconButton>
          </>
        }
      />
      {open && (
        <>
          {schema?.loading && <TreeRow depth={3} className="muted" label="Loading schema…" />}
          {schema?.error && <TreeRow depth={3} className="error selectable" title={schema.error} label={schema.error} />}
          {schema && !schema.loading && !schema.error && (
            <SchemaTree conn={conn} database={scratchDatabase} tables={schema.tables} onRefresh={refresh} />
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
function SchemaTree({ conn, database, tables, onRefresh }: { conn: Connection; database: string; tables: Table[]; onRefresh: () => void }) {
  const [openSchemas, setOpenSchemas] = useState<Record<string, boolean>>({})
  const [openTables, setOpenTables] = useState<Record<string, boolean>>({})
  const createScratch = useScratch((s) => s.create)
  const flat = conn.driver === 'clickhouse'

  const bySchema = new Map<string, Table[]>()
  for (const t of tables) bySchema.set(t.schema, [...(bySchema.get(t.schema) ?? []), t])
  if (bySchema.size === 0) return <TreeRow depth={3} className="muted" label="No tables." />

  const query = (sql: string) => void createScratch('sql', conn.id, sql, database)
  const preview = (t: Table) => query(previewQuery(conn.driver, t))

  const tableRows = (list: Table[], depth: number) =>
    list.map((t) => {
      const key = `${t.schema}.${t.name}`
      const tOpen = !!openTables[key]
      return (
        <div key={key}>
          <TreeRow
            depth={depth}
            twisty={tOpen ? 'open' : 'closed'}
            title={`${t.kind} · double-click to preview`}
            onClick={() => setOpenTables({ ...openTables, [key]: !tOpen })}
            onDoubleClick={() => preview(t)}
            onContextMenu={onMenu(() => tableMenu(conn.driver, t, { query, copy: copyText, refresh: onRefresh }))}
            label={t.kind.includes('view') ? <em>{t.name}</em> : t.name}
          />
          {tOpen &&
            t.columns.map((c) => (
              <TreeRow
                key={c.name}
                depth={depth + 1}
                className="column-row"
                onContextMenu={onMenu(() => columnMenu(conn.driver, t, c, { query, copy: copyText }))}
                label={<span className="col-name">{c.name}</span>}
              >
                <span className="col-type">{c.type}</span>
              </TreeRow>
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
            <TreeRow
              depth={3}
              twisty={sOpen ? 'open' : 'closed'}
              onClick={() => setOpenSchemas({ ...openSchemas, [schemaName]: !sOpen })}
              onContextMenu={onMenu(() => schemaMenu(conn.driver, schemaName, { query, copy: copyText }))}
              label={schemaName}
              badge={list.length}
            />
            {sOpen && tableRows(list, 4)}
          </div>
        )
      })}
    </>
  )
}
