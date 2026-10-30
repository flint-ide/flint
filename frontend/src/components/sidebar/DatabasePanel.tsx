import { useState } from 'react'
import type { Connection, Table } from '../../api/backend'
import { useDatabase } from '../../store/database'
import { useScratch } from '../../store/scratch'
import { confirmAction } from '../../store/dialog'
import { ConnectionForm } from './ConnectionForm'

/** Connections list with a lazily loaded schema tree per connection. */
export function DatabasePanel() {
  const connections = useDatabase((s) => s.connections)
  // In the store so the palette's "New Database Connection…" can open the form.
  const editing = useDatabase((s) => s.editing)
  const setEditing = useDatabase((s) => s.setEditing)

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">Databases</span>
        <button className="icon" title="New connection" onClick={() => setEditing('new')}>＋</button>
      </div>
      <div className="tree scroll">
        {connections.length === 0 && (
          <div className="placeholder muted">
            No connections yet.
            <button className="link" onClick={() => setEditing('new')}>Add PostgreSQL or ClickHouse</button>
          </div>
        )}
        {connections.map((c) => (
          <ConnectionNode key={c.id} conn={c} onEdit={() => setEditing(c)} />
        ))}
      </div>
      {editing && <ConnectionForm initial={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />}
    </div>
  )
}

function ConnectionNode({ conn, onEdit }: { conn: Connection; onEdit: () => void }) {
  const [open, setOpen] = useState(false)
  const schema = useDatabase((s) => s.schemas[conn.id])
  const loadSchema = useDatabase((s) => s.loadSchema)
  const removeConnection = useDatabase((s) => s.removeConnection)
  const createScratch = useScratch((s) => s.create)

  const toggle = () => {
    if (!open) void loadSchema(conn.id)
    setOpen(!open)
  }

  const remove = async () => {
    if (await confirmAction(`Delete connection "${conn.name}"? Its password is removed from the Keychain.`, 'Delete')) {
      await removeConnection(conn.id)
    }
  }

  return (
    <div className="conn-node">
      <div className="tree-row static conn-row" onClick={toggle}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span className={`driver-badge ${conn.driver}`}>{conn.driver === 'clickhouse' ? 'CH' : 'PG'}</span>
        <span className="conn-name" title={`${conn.user}@${conn.host}:${conn.port}/${conn.database}`}>{conn.name}</span>
        {conn.readOnly && <span className="badge" title="Read-only">RO</span>}
        <span className="row-actions" onClick={(e) => e.stopPropagation()}>
          <button className="icon" title="New SQL console" onClick={() => void createScratch('sql', conn.id)}>⌨</button>
          <button className="icon" title="Refresh schema" onClick={() => void loadSchema(conn.id, true)}>↻</button>
          <button className="icon" title="Edit" onClick={onEdit}>✎</button>
          <button className="icon" title="Delete" onClick={() => void remove()}>✕</button>
        </span>
      </div>
      {open && (
        <div className="conn-children">
          {schema?.loading && <div className="tree-row static muted indent-1">Loading schema…</div>}
          {schema?.error && <div className="tree-row static error indent-1 selectable" title={schema.error}>{schema.error}</div>}
          {schema && !schema.loading && !schema.error && <SchemaTree conn={conn} tables={schema.tables} />}
        </div>
      )}
    </div>
  )
}

function SchemaTree({ conn, tables }: { conn: Connection; tables: Table[] }) {
  const [openSchemas, setOpenSchemas] = useState<Record<string, boolean>>({})
  const [openTables, setOpenTables] = useState<Record<string, boolean>>({})
  const createScratch = useScratch((s) => s.create)

  const bySchema = new Map<string, Table[]>()
  for (const t of tables) bySchema.set(t.schema, [...(bySchema.get(t.schema) ?? []), t])
  if (bySchema.size === 0) return <div className="tree-row static muted indent-1">No tables.</div>

  const preview = (t: Table) => {
    const q = conn.driver === 'clickhouse' ? '`' : '"'
    const ident = (s: string) => (/^[a-z_][a-z0-9_]*$/.test(s) ? s : `${q}${s.replaceAll(q, q + q)}${q}`)
    void createScratch('sql', conn.id, `SELECT *\nFROM ${ident(t.schema)}.${ident(t.name)}\nLIMIT 100;\n`)
  }

  return (
    <>
      {[...bySchema].map(([schemaName, list]) => {
        const sOpen = openSchemas[schemaName] ?? bySchema.size === 1
        return (
          <div key={schemaName}>
            <div className="tree-row static indent-1" onClick={() => setOpenSchemas({ ...openSchemas, [schemaName]: !sOpen })}>
              <span className="twisty">{sOpen ? '▾' : '▸'}</span>
              <span className="schema-name">{schemaName}</span>
              <span className="badge">{list.length}</span>
            </div>
            {sOpen &&
              list.map((t) => {
                const key = `${t.schema}.${t.name}`
                const tOpen = !!openTables[key]
                return (
                  <div key={key}>
                    <div
                      className="tree-row static indent-2"
                      title={`${t.kind} · double-click to preview`}
                      onClick={() => setOpenTables({ ...openTables, [key]: !tOpen })}
                      onDoubleClick={() => preview(t)}
                    >
                      <span className="twisty">{tOpen ? '▾' : '▸'}</span>
                      <span className={`table-name ${t.kind.includes('view') ? 'view' : ''}`}>{t.name}</span>
                    </div>
                    {tOpen &&
                      t.columns.map((c) => (
                        <div key={c.name} className="tree-row static indent-3 column-row">
                          <span className="col-name">{c.name}</span>
                          <span className="col-type">{c.type}</span>
                        </div>
                      ))}
                  </div>
                )
              })}
          </div>
        )
      })}
    </>
  )
}
