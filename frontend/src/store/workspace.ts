import { createStore } from '../lib/store'
import { Workspace, errorMessage, type Entry } from '../api/backend'
import { restoreSession, saveSession, setSessionRoots } from './session'
import { addRootError, withoutRoot } from '../lib/workspaceRoots'
import { useEditor } from './editor'

export type SidebarView = 'files' | 'search' | 'git' | 'database' | 'scratch'
/** What the branches popup opens on (title bar widget or the palette). */
export type BranchesMode = 'list' | 'new-branch' | 'new-worktree'

interface WorkspaceState {
  /** The primary project folder (its session holds the workspace). */
  root: string | null
  /** Every folder of the workspace, the primary one first; [] when nothing is open. */
  roots: string[]
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
  /** Attaches another project folder (multi-root workspace). */
  addRoot: (path: string) => Promise<string | null>
  /** Shows the folder picker, then attaches the chosen folder. */
  pickRoot: () => Promise<void>
  /** Detaches an attached folder; files stay on disk. */
  removeRoot: (path: string) => Promise<void>
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

/** Attaches the folders a saved session had, skipping ones that are gone; returns those attached. */
function attachSaved(add: (path: string) => Promise<string | null>) {
  return async (roots: string[]): Promise<string[]> => {
    const attached: string[] = []
    for (const r of roots) {
      const ok = await Workspace.ListDir(r).then(
        () => true,
        () => false,
      )
      const root = ok ? await add(r) : null
      if (root) attached.push(root)
    }
    return attached
  }
}

export const useWorkspace = createStore<WorkspaceState>((set, get) => ({
  root: null,
  roots: [],
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
      // Re-opening the open project keeps its attached folders (the backend reset them).
      const keep = previous === root ? get().roots.slice(1) : []
      set({ roots: [root], children: { [root]: entries }, expanded: { [root]: true } })
      setSessionRoots([root])
      for (const r of keep) await get().addRoot(r)
      // Restore before publishing root so the "first terminal" effect sees restored tabs.
      if (previous !== root) await restoreSession(root, attachSaved(get().addRoot))
      set({ root, fileCount: 0, filesLoadedAt: 0 })
      void get().refreshFiles()
    } catch (err) {
      set({ error: errorMessage(err) })
    }
  },

  addRoot: async (path) => {
    const message = addRootError(get().roots, path)
    if (message) {
      set({ error: message })
      return null
    }
    try {
      const root = await Workspace.AddRoot(path)
      const entries = await Workspace.ListDir(root)
      set((s) => ({
        roots: [...s.roots, root],
        children: { ...s.children, [root]: entries },
        expanded: { ...s.expanded, [root]: true },
      }))
      setSessionRoots(get().roots)
      void get().refreshFiles()
      return root
    } catch (err) {
      set({ error: errorMessage(err) })
      return null
    }
  },

  pickRoot: async () => {
    try {
      const path = await Workspace.PickFolder()
      if (path) await get().addRoot(path)
    } catch (err) {
      set({ error: errorMessage(err) })
    }
  },

  removeRoot: async (root) => {
    try {
      await Workspace.RemoveRoot(root)
    } catch (err) {
      set({ error: errorMessage(err) })
      return
    }
    useEditor.getState().closePaths([root])
    set((s) => ({
      roots: s.roots.filter((r) => r !== root),
      children: withoutRoot(s.children, root),
      expanded: withoutRoot(s.expanded, root),
    }))
    setSessionRoots(get().roots)
    void get().refreshFiles()
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
        const roots = get().roots
        if (roots.length === 0) return
        try {
          const counts = await Promise.all(roots.map((r) => Workspace.IndexFiles(r)))
          const fileCount = counts.reduce((a, b) => a + b, 0)
          if (get().roots === roots) set({ fileCount, filesLoadedAt: Date.now() })
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
