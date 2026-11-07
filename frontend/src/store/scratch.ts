import { createStore } from '../lib/store'
import { Scratch, errorMessage, type ScratchKind, type ScratchPad } from '../api/backend'
import type { scratch } from '../wails/models'
import { scratchFileType } from '../lib/scratchTypes'
import { confirmAction, showToast } from './dialog'
import { saveTab, useEditor } from './editor'
import { useWorkspace } from './workspace'

export type ScratchFile = scratch.File

interface ScratchState {
  items: ScratchPad[]
  /** Scratch files of any type (edited in file tabs), newest first. */
  files: ScratchFile[]
  /** Bumps to open the panel's file type picker (openScratchFilePicker). */
  pickerToken: number
  /** Loads scratchpads and scratch files. */
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
  files: [],
  pickerToken: 0,

  load: async () => {
    try {
      const [items, files] = await Promise.all([Scratch.List(), Scratch.Files()])
      set({ items: items ?? [], files: files ?? [] })
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

// ---------- scratch files (any type, edited in file tabs) ----------

async function reloadFiles(): Promise<void> {
  const files = (await Scratch.Files()) ?? []
  useScratch.setState({ files })
}

/** Creates a scratch file of a type (lib/scratchTypes) and opens it in the editor. */
export async function createScratchFile(typeId: string): Promise<void> {
  const type = scratchFileType(typeId)
  if (!type) return
  try {
    const file = await Scratch.CreateFile(type.fileName, type.template ?? '')
    useScratch.setState((s) => ({ files: [file, ...s.files.filter((f) => f.path !== file.path)] }))
    useEditor.getState().openFile(file.path, type.line ? { line: type.line, from: 0, to: 0 } : undefined)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

const SCRATCH_SAVE_DELAY_MS = 800
const saveTimers = new Map<string, ReturnType<typeof setTimeout>>()

/**
 * Scratch files save themselves like scratchpads do: a modified scratch file tab is
 * saved once typing pauses (project files keep the editor's own save rules).
 */
export function subscribeScratchAutosave(): () => void {
  return useEditor.subscribe((s, prev) => {
    if (s.tabs === prev.tabs) return
    const scratchPaths = new Set(useScratch.getState().files.map((f) => f.path))
    for (const tab of s.tabs) {
      if (tab.kind !== 'file' || !tab.dirty || !scratchPaths.has(tab.path)) continue
      clearTimeout(saveTimers.get(tab.id))
      saveTimers.set(
        tab.id,
        setTimeout(() => {
          saveTimers.delete(tab.id)
          void saveTab(tab.id)
        }, SCRATCH_SAVE_DELAY_MS),
      )
    }
  })
}

/** Shows the Scratchpads panel with its file type picker open (the tab bar's ＋ ▸ Scratch File…). */
export function openScratchFilePicker(): void {
  useWorkspace.getState().focusPanel('scratch')
  useScratch.setState((s) => ({ pickerToken: s.pickerToken + 1 }))
}

/** Renames a scratch file; its open tab follows (saved first). */
export async function renameScratchFile(path: string, name: string): Promise<void> {
  try {
    const tab = useEditor.getState().tabs.find((t) => t.kind === 'file' && t.path === path)
    if (tab?.dirty) await saveTab(tab.id)
    const file = await Scratch.RenameFile(path, name)
    if (file.path !== path) useEditor.getState().renamePath(path, file.path)
    await reloadFiles()
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Asks, then deletes a scratch file and closes its tab. */
export async function deleteScratchFile(file: ScratchFile): Promise<void> {
  if (!(await confirmAction(`Delete scratch file "${file.name}"?`, 'Delete'))) return
  try {
    await Scratch.DeleteFile(file.path)
    useEditor.getState().closePaths([file.path])
    await reloadFiles()
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}
