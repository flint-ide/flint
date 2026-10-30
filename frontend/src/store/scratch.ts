import { createStore } from '../lib/store'
import { Scratch, errorMessage, type ScratchKind, type ScratchPad } from '../api/backend'
import { showToast } from './dialog'
import { useEditor } from './editor'

interface ScratchState {
  items: ScratchPad[]
  load: () => Promise<void>
  /** database: one of the connection's databases ("" = the profile's default). */
  create: (kind: ScratchKind, connId?: string, initial?: string, database?: string) => Promise<ScratchPad | null>
  rename: (id: string, name: string) => Promise<void>
  setConnection: (id: string, connId: string, database?: string) => Promise<void>
  remove: (id: string) => Promise<void>
}

/** Initial content for scratchpads created with a prefilled query (e.g. from the schema tree). */
export const pendingContent = new Map<string, string>()

export const useScratch = createStore<ScratchState>((set, get) => ({
  items: [],

  load: async () => {
    try {
      set({ items: (await Scratch.List()) ?? [] })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  create: async (kind, connId = '', initial, database = '') => {
    try {
      const sc = await Scratch.Create('', kind, connId, database)
      if (initial) pendingContent.set(sc.id, initial)
      set((s) => ({ items: [sc, ...s.items] }))
      useEditor.getState().openScratch(sc)
      return sc
    } catch (err) {
      showToast(errorMessage(err), 'error')
      return null
    }
  },

  rename: async (id, name) => {
    try {
      await Scratch.Rename(id, name)
      set((s) => ({ items: s.items.map((i) => (i.id === id ? { ...i, name } : i)) }))
      useEditor.getState().renameScratchTab(id, name)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  // Optimistic: a query run right after switching must already target the new database.
  setConnection: async (id, connId, database = '') => {
    const previous = get().items.find((i) => i.id === id)
    set((s) => ({ items: s.items.map((i) => (i.id === id ? { ...i, connId, database } : i)) }))
    try {
      await Scratch.SetConnection(id, connId, database)
    } catch (err) {
      if (previous) set((s) => ({ items: s.items.map((i) => (i.id === id ? previous : i)) }))
      showToast(errorMessage(err), 'error')
    }
  },

  remove: async (id) => {
    try {
      await Scratch.Delete(id)
      useEditor.getState().closeScratch(id)
      set({ items: get().items.filter((i) => i.id !== id) })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },
}))
