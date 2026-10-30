import { useEffect, useState } from 'react'
import type { Connection, DatabaseInfo, Table } from '../../api/backend'
import { defaultDatabase, resolveDatabase, schemaKey } from '../../lib/databases'
import { useDatabase } from '../../store/database'
import { useScratch } from '../../store/scratch'
import { DatabaseIcon } from '../icons/DatabaseIcon'

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

  if (!info.canConnect) {
    return (
      <div className="tree-row static indent-2 db-row locked" title="No CONNECT privilege on this database">
        <span className="twisty" />
        <DatabaseIcon locked />
        <span className="schema-name">{info.name}</span>
      </div>
    )
  }

  return (
    <div>
      <div className="tree-row static indent-2 db-row" onClick={() => setOpen(!open)} title={isDefault ? 'Default database of this connection' : undefined}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <DatabaseIcon />
        <span className={`schema-name${isDefault ? ' default-db' : ''}`}>{info.name}</span>
        <span className="row-actions" onClick={(e) => e.stopPropagation()}>
          <button className="icon" title="New SQL console in this database" onClick={() => void createScratch('sql', conn.id, undefined, scratchDatabase)}>⌨</button>
          <button className="icon" title="Refresh schema" onClick={() => void loadSchema(conn.id, database, true)}>↻</button>
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

  const preview = (t: Table) => {
    const q = flat ? '`' : '"'
    const ident = (s: string) => (/^[a-z_][a-z0-9_]*$/.test(s) ? s : `${q}${s.replaceAll(q, q + q)}${q}`)
    void createScratch('sql', conn.id, `SELECT *\nFROM ${ident(t.schema)}.${ident(t.name)}\nLIMIT 100;\n`, database)
  }

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
          >
            <span className="twisty">{tOpen ? '▾' : '▸'}</span>
            <span className={`table-name ${t.kind.includes('view') ? 'view' : ''}`}>{t.name}</span>
          </div>
          {tOpen &&
            t.columns.map((c) => (
              <div key={c.name} className={`tree-row static indent-${depth + 1} column-row`}>
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
            <div className="tree-row static indent-3" onClick={() => setOpenSchemas({ ...openSchemas, [schemaName]: !sOpen })}>
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
