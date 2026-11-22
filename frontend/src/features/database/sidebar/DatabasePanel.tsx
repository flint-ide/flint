import { Badge, Button, EmptyState, IconButton, PanelHeader, TreeRow } from '@flint-ide/ui'
import { Pencil, Plus, RefreshCw, SquareTerminal, X } from '@flint-ide/ui/icons'
import { useState } from 'react'
import type { Connection } from '../../../api/backend'
import { defaultDatabase } from '../lib/databases'
import { useDatabase } from '../store/database'
import { useScratch } from '../../../store/scratch'
import { confirmAction } from '../../../store/dialog'
import { ConnectionForm } from './ConnectionForm'
import { copyText, DatabasesNode, onMenu } from './DatabaseTree'
import { connectionMenu } from './databaseMenu'
import { keyringName } from '../../../lib/platform'

/** Connections list; each expands into its server's databases, each with a lazily loaded schema tree. */
export function DatabasePanel() {
  const connections = useDatabase((s) => s.connections)
  // In the store so the palette's "New Database Connection…" can open the form.
  const editing = useDatabase((s) => s.editing)
  const setEditing = useDatabase((s) => s.setEditing)

  return (
    <div className="panel">
      <PanelHeader
        title="Databases"
        actions={<IconButton variant="ghost" size="sm" aria-label="New connection" title="New connection" onClick={() => setEditing('new')}><Plus /></IconButton>}
      />
      <div className="tree scroll">
        {connections.length === 0 && (
          <EmptyState
            variant="default"
            title="No connections yet"
            action={<Button variant="ghost" size="sm" onClick={() => setEditing('new')}>Add PostgreSQL or ClickHouse</Button>}
          />
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
    <div>
      <TreeRow
        twisty={open ? 'open' : 'closed'}
        onClick={toggle}
        onContextMenu={menu}
        icon={<span className={`driver-badge ${conn.driver}`}>{conn.driver === 'clickhouse' ? 'CH' : 'PG'}</span>}
        label={<span title={`${conn.user}@${conn.host}:${conn.port}/${defaultDatabase(conn)}`}>{conn.name}</span>}
        actions={
          <>
            <IconButton variant="ghost" size="sm" aria-label="New SQL console" title="New SQL console" onClick={newConsole}><SquareTerminal /></IconButton>
            <IconButton variant="ghost" size="sm" aria-label="Refresh databases" title="Refresh databases" onClick={refresh}><RefreshCw /></IconButton>
            <IconButton variant="ghost" size="sm" aria-label="Edit" title="Edit" onClick={onEdit}><Pencil /></IconButton>
            <IconButton variant="ghost" size="sm" aria-label="Delete" title="Delete" onClick={() => void remove()}><X /></IconButton>
          </>
        }
      >
        {conn.readOnly && <Badge size="sm" title="Read-only">RO</Badge>}
      </TreeRow>
      {open && <DatabasesNode conn={conn} />}
    </div>
  )
}
