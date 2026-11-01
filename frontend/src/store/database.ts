import { createStore } from '../lib/store'
import { Database, errorMessage, type Connection, type DatabaseInfo, type Table } from '../api/backend'
import { isSchemaKeyOf, schemaKey } from '../lib/databases'
import { confirmAction, showToast } from './dialog'

interface SchemaEntry {
  tables: Table[]
  loading: boolean
  error: string | null
}

interface DatabasesEntry {
  list: DatabaseInfo[]
  loading: boolean
  error: string | null
}

interface DatabaseState {
  connections: Connection[]
  /** Schemas per (connection, database), keyed by schemaKey; database is always resolved. */
  schemas: Record<string, SchemaEntry>
  /** Connection being edited in the form ('new' = adding one); null when closed. */
  editing: Connection | 'new' | null
  setEditing: (editing: Connection | 'new' | null) => void
  /** Databases on each connection's server, keyed by connection id. */
  databases: Record<string, DatabasesEntry>
  loadConnections: () => Promise<void>
  loadDatabases: (connId: string, refresh?: boolean) => Promise<void>
  loadSchema: (connId: string, database: string, refresh?: boolean) => Promise<void>
  removeConnection: (connId: string) => Promise<void>
  upsertConnection: (conn: Connection) => void
}

/** Drops every cached schema and database list of a connection (profile changed or deleted). */
function withoutConnection(s: DatabaseState, connId: string) {
  const schemas = Object.fromEntries(Object.entries(s.schemas).filter(([key]) => !isSchemaKeyOf(key, connId)))
  const { [connId]: _stale, ...databases } = s.databases
  return { schemas, databases }
}

export const useDatabase = createStore<DatabaseState>((set, get) => ({
  connections: [],
  schemas: {},
  editing: null,

  setEditing: (editing) => set({ editing }),

  databases: {},

  loadConnections: async () => {
    try {
      set({ connections: (await Database.ListConnections()) ?? [] })
      // Connections moved from ClickHouse's native protocol to HTTP: tell the user once.
      const notice = await Database.MigrationNotice()
      if (notice) void confirmAction(notice, 'OK')
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  loadDatabases: async (connId, refresh = false) => {
    const current = get().databases[connId]
    if (current?.loading || (current && !current.error && !refresh)) return
    set((s) => ({
      databases: { ...s.databases, [connId]: { list: current?.list ?? [], loading: true, error: null } },
    }))
    // The profile may change mid-flight (upsertConnection clears the entry): drop stale answers.
    const stale = () => !get().databases[connId]
    try {
      const list = (await Database.Databases(connId)) ?? []
      if (stale()) return
      set((s) => ({ databases: { ...s.databases, [connId]: { list, loading: false, error: null } } }))
    } catch (err) {
      if (stale()) return
      set((s) => ({
        databases: { ...s.databases, [connId]: { list: [], loading: false, error: errorMessage(err) } },
      }))
    }
  },

  loadSchema: async (connId, database, refresh = false) => {
    const key = schemaKey(connId, database)
    const current = get().schemas[key]
    if (current?.loading || (current && !current.error && !refresh)) return
    set((s) => ({
      schemas: { ...s.schemas, [key]: { tables: current?.tables ?? [], loading: true, error: null } },
    }))
    const stale = () => !get().schemas[key]
    try {
      const tables = (await Database.Schema(connId, database, refresh)) ?? []
      if (stale()) return
      set((s) => ({ schemas: { ...s.schemas, [key]: { tables, loading: false, error: null } } }))
    } catch (err) {
      if (stale()) return
      set((s) => ({
        schemas: { ...s.schemas, [key]: { tables: [], loading: false, error: errorMessage(err) } },
      }))
    }
  },

  removeConnection: async (connId) => {
    try {
      await Database.DeleteConnection(connId)
      set((s) => ({ connections: s.connections.filter((c) => c.id !== connId), ...withoutConnection(s, connId) }))
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  upsertConnection: (conn) =>
    set((s) => {
      const exists = s.connections.some((c) => c.id === conn.id)
      return {
        connections: exists ? s.connections.map((c) => (c.id === conn.id ? conn : c)) : [...s.connections, conn],
        ...withoutConnection(s, conn.id), // profile changed: refetch lazily
      }
    }),
}))
