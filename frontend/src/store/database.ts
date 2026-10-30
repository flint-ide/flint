import { createStore } from '../lib/store'
import { Database, errorMessage, type Connection, type Table } from '../api/backend'
import { showToast } from './dialog'

interface SchemaEntry {
  tables: Table[]
  loading: boolean
  error: string | null
}

interface DatabaseState {
  connections: Connection[]
  schemas: Record<string, SchemaEntry>
  /** Connection being edited in the form ('new' = adding one); null when closed. */
  editing: Connection | 'new' | null
  setEditing: (editing: Connection | 'new' | null) => void
  loadConnections: () => Promise<void>
  loadSchema: (connId: string, refresh?: boolean) => Promise<void>
  removeConnection: (connId: string) => Promise<void>
  upsertConnection: (conn: Connection) => void
}

export const useDatabase = createStore<DatabaseState>((set, get) => ({
  connections: [],
  schemas: {},
  editing: null,

  setEditing: (editing) => set({ editing }),

  loadConnections: async () => {
    try {
      set({ connections: (await Database.ListConnections()) ?? [] })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  loadSchema: async (connId, refresh = false) => {
    const current = get().schemas[connId]
    if (current?.loading || (current && !current.error && !refresh)) return
    set((s) => ({
      schemas: { ...s.schemas, [connId]: { tables: current?.tables ?? [], loading: true, error: null } },
    }))
    try {
      const tables = (await Database.Schema(connId, refresh)) ?? []
      set((s) => ({ schemas: { ...s.schemas, [connId]: { tables, loading: false, error: null } } }))
    } catch (err) {
      set((s) => ({
        schemas: { ...s.schemas, [connId]: { tables: [], loading: false, error: errorMessage(err) } },
      }))
    }
  },

  removeConnection: async (connId) => {
    try {
      await Database.DeleteConnection(connId)
      set((s) => {
        const { [connId]: _removed, ...schemas } = s.schemas
        return { connections: s.connections.filter((c) => c.id !== connId), schemas }
      })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  upsertConnection: (conn) =>
    set((s) => {
      const exists = s.connections.some((c) => c.id === conn.id)
      const { [conn.id]: _stale, ...schemas } = s.schemas // profile changed: refetch schema lazily
      return {
        connections: exists ? s.connections.map((c) => (c.id === conn.id ? conn : c)) : [...s.connections, conn],
        schemas,
      }
    }),
}))
