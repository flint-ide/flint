import { createStore } from '../lib/store'
import { Workspace, errorMessage, type Entry } from '../api/backend'
import { restoreSession, saveSession } from './session'

export type SidebarView = 'files' | 'search' | 'git' | 'database' | 'scratch'
/** What the branches popup opens on (title bar widget or the palette). */
export type BranchesMode = 'list' | 'new-branch' | 'new-worktree'

interface WorkspaceState {
  root: string | null
  /** True once the initial folder (CLI arg / last used) has been resolved. */
  ready: boolean
  /** Files indexed for quick-open; the list itself stays in Go. */
  fileCount: number
  filesLoadedAt: number
  children: Record<string, Entry[]>
  expanded: Record<string, boolean>
  sidebar: SidebarView
  sidebarVisible: boolean
  quickOpen: boolean
  recentOpen: boolean
  paletteOpen: boolean
  branchesPopup: BranchesMode | null
  error: string | null

  init: () => Promise<void>
  openFolder: (path: string) => Promise<void>
  refreshFiles: () => Promise<void>
  toggleDir: (path: string) => Promise<void>
  reloadTree: () => Promise<void>
  setSidebar: (view: SidebarView) => void
  toggleSidebar: () => void
  setQuickOpen: (open: boolean) => void
  setRecentOpen: (open: boolean) => void
  setPaletteOpen: (open: boolean) => void
  setBranchesPopup: (mode: BranchesMode | null) => void
  clearError: () => void
}

let listing: Promise<void> | null = null
let relistRequested = false

export const useWorkspace = createStore<WorkspaceState>((set, get) => ({
  root: null,
  ready: false,
  fileCount: 0,
  filesLoadedAt: 0,
  children: {},
  expanded: {},
  sidebar: 'files',
  sidebarVisible: true,
  quickOpen: false,
  recentOpen: false,
  paletteOpen: false,
  branchesPopup: null,
  error: null,

  init: async () => {
    try {
      const initial = await Workspace.InitialFolder()
      if (initial) await get().openFolder(initial)
    } finally {
      set({ ready: true })
    }
  },

  openFolder: async (path) => {
    try {
      const previous = get().root
      const root = await Workspace.OpenFolder(path)
      if (previous && previous !== root) await saveSession(previous)
      const entries = await Workspace.ListDir(root)
      // Restore before publishing root so the "first terminal" effect sees restored tabs.
      if (previous !== root) await restoreSession(root)
      set({ root, children: { [root]: entries }, expanded: { [root]: true }, fileCount: 0, filesLoadedAt: 0 })
      void get().refreshFiles()
    } catch (err) {
      set({ error: errorMessage(err) })
    }
  },

  // Single-flight: while a listing runs, further requests collapse into one re-run.
  refreshFiles: async () => {
    if (listing) {
      relistRequested = true
      return listing
    }
    listing = (async () => {
      do {
        relistRequested = false
        const root = get().root
        if (!root) return
        try {
          const fileCount = await Workspace.IndexFiles(root)
          if (get().root === root) set({ fileCount, filesLoadedAt: Date.now() })
        } catch (err) {
          set({ error: errorMessage(err) })
          return
        }
      } while (relistRequested)
    })().finally(() => {
      listing = null
    })
    return listing
  },

  toggleDir: async (path) => {
    const { expanded, children } = get()
    if (expanded[path]) {
      set({ expanded: { ...expanded, [path]: false } })
      return
    }
    try {
      const entries = children[path] ?? (await Workspace.ListDir(path))
      set((s) => ({
        children: { ...s.children, [path]: entries },
        expanded: { ...s.expanded, [path]: true },
      }))
    } catch (err) {
      set({ error: errorMessage(err) })
    }
  },

  reloadTree: async () => {
    const { expanded } = get()
    const open = Object.keys(expanded).filter((p) => expanded[p])
    const results = await Promise.allSettled(open.map((p) => Workspace.ListDir(p)))
    const children: Record<string, Entry[]> = {}
    results.forEach((r, i) => {
      if (r.status === 'fulfilled') children[open[i]] = r.value
    })
    set({ children })
    void get().refreshFiles()
  },

  setSidebar: (view) => set({ sidebar: view, sidebarVisible: true }),
  toggleSidebar: () => set((s) => ({ sidebarVisible: !s.sidebarVisible })),
  setQuickOpen: (open) => set({ quickOpen: open }),
  setRecentOpen: (open) => set({ recentOpen: open }),
  setPaletteOpen: (open) => set({ paletteOpen: open }),
  setBranchesPopup: (mode) => set({ branchesPopup: mode }),
  clearError: () => set({ error: null }),
}))
