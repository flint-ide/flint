import { useState } from 'react'
import type { Connection } from '../../api/backend'
import { defaultDatabase } from '../../lib/databases'
import { useDatabase } from '../../store/database'
import { useScratch } from '../../store/scratch'
import { confirmAction } from '../../store/dialog'
import { ConnectionForm } from './ConnectionForm'
import { copyText, DatabasesNode, onMenu } from './DatabaseTree'
import { connectionMenu } from './databaseMenu'
import { keyringName } from '../../lib/platform'

/** Connections list; each expands into its server's databases, each with a lazily loaded schema tree. */
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
  const loadDatabases = useDatabase((s) => s.loadDatabases)
  const removeConnection = useDatabase((s) => s.removeConnection)
  const createScratch = useScratch((s) => s.create)

  const toggle = () => {
    if (!open) void loadDatabases(conn.id)
    setOpen(!open)
  }

  const remove = async () => {
    if (await confirmAction(`Delete connection "${conn.name}"? Its password is removed from the ${keyringName()}.`, 'Delete')) {
      await removeConnection(conn.id)
    }
  }

  const newConsole = () => void createScratch('sql', conn.id)
  const refresh = () => void loadDatabases(conn.id, true)
  const menu = onMenu(() => connectionMenu(conn, { newConsole, refresh, edit: onEdit, remove: () => void remove(), copy: copyText }))

  return (
    <div className="conn-node">
      <div className="tree-row static conn-row" onClick={toggle} onContextMenu={menu}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span className={`driver-badge ${conn.driver}`}>{conn.driver === 'clickhouse' ? 'CH' : 'PG'}</span>
        <span className="conn-name" title={`${conn.user}@${conn.host}:${conn.port}/${defaultDatabase(conn)}`}>{conn.name}</span>
        {conn.readOnly && <span className="badge" title="Read-only">RO</span>}
        <span className="row-actions" onClick={(e) => e.stopPropagation()}>
          <button className="icon" title="New SQL console" onClick={newConsole}>⌨</button>
          <button className="icon" title="Refresh databases" onClick={refresh}>↻</button>
          <button className="icon" title="Edit" onClick={onEdit}>✎</button>
          <button className="icon" title="Delete" onClick={() => void remove()}>✕</button>
        </span>
      </div>
      {open && (
        <div className="conn-children">
          <DatabasesNode conn={conn} />
        </div>
      )}
    </div>
  )
}
