import { createStore } from '../lib/store'
import { Scratch, errorMessage, type ScratchKind, type ScratchPad } from '../api/backend'
import { showToast } from './dialog'
import { useEditor } from './editor'

interface ScratchState {
  items: ScratchPad[]
  load: () => Promise<void>
  create: (kind: ScratchKind, connId?: string, initial?: string) => Promise<ScratchPad | null>
  rename: (id: string, name: string) => Promise<void>
  setConnection: (id: string, connId: string) => Promise<void>
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

  create: async (kind, connId = '', initial) => {
    try {
      const sc = await Scratch.Create('', kind, connId)
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

  setConnection: async (id, connId) => {
    try {
      await Scratch.SetConnection(id, connId)
      set((s) => ({ items: s.items.map((i) => (i.id === id ? { ...i, connId } : i)) }))
    } catch (err) {
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
