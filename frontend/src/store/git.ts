import { createStore } from '../lib/store'
import * as Git from '../../wailsjs/go/main/GitAPI'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { git } from '../../wailsjs/go/models'
import { errorMessage } from '../api/backend'
import { showToast } from './dialog'
import { useWorkspace } from './workspace'

export type FileChange = git.FileChange
export type GitStatus = git.Status
export type Branch = git.Branch
export type Worktree = git.Worktree

export { Git }

type RemoteOp = 'fetch' | 'pull' | 'push' | 'commit' | null

interface GitState {
  root: string | null // work-tree root (may differ from the opened folder)
  isRepo: boolean
  status: GitStatus | null
  /** Paths excluded from the next commit (IntelliJ includes all changes by default). */
  excluded: Record<string, boolean>
  /** Untracked files the user explicitly ticked (excluded by default, like IntelliJ). */
  includedUntracked: Record<string, boolean>
  message: string
  amend: boolean
  busy: RemoteOp
  /** Bumps whenever HEAD/index change so editors refetch their gutter baseline. */
  headVersion: number

  load: (folder: string | null) => Promise<void>
  refresh: () => Promise<void>
  setMessage: (m: string) => void
  setAmend: (amend: boolean) => Promise<void>
  toggleFile: (f: FileChange) => void
  setGroup: (files: FileChange[], include: boolean) => void
  isIncluded: (f: FileChange) => boolean
  commit: (push: boolean) => Promise<void>
  rollback: (files: FileChange[]) => Promise<void>
  remote: (op: 'fetch' | 'pull' | 'push') => Promise<void>
}

export const useGit = createStore<GitState>((set, get) => ({
  root: null,
  isRepo: false,
  status: null,
  excluded: {},
  includedUntracked: {},
  message: '',
  amend: false,
  busy: null,
  headVersion: 0,

  load: async (folder) => {
    if (!folder) {
      set({ root: null, isRepo: false, status: null })
      return
    }
    try {
      const info = await Git.Info(folder)
      set({ root: info.isRepo ? info.root : null, isRepo: info.isRepo, status: null, excluded: {}, includedUntracked: {} })
      if (info.isRepo) await get().refresh()
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  refresh: () => refreshStatus(),

  setMessage: (message) => set({ message }),

  setAmend: async (amend) => {
    set({ amend })
    const { root, message } = get()
    // Like IntelliJ: switching to amend pulls in the previous message when empty.
    if (amend && root && !message.trim()) {
      try {
        set({ message: await Git.LastCommitMessage(root) })
      } catch {
        // No commits yet: nothing to amend into, keep the empty message.
      }
    }
  },

  isIncluded: (f) => (f.kind === 'untracked' ? !!get().includedUntracked[f.path] : !get().excluded[f.path]),

  toggleFile: (f) =>
    set((s) =>
      f.kind === 'untracked'
        ? { includedUntracked: { ...s.includedUntracked, [f.path]: !s.includedUntracked[f.path] } }
        : { excluded: { ...s.excluded, [f.path]: !s.excluded[f.path] } },
    ),

  setGroup: (files, include) =>
    set((s) => {
      const excluded = { ...s.excluded }
      const includedUntracked = { ...s.includedUntracked }
      for (const f of files) {
        if (f.kind === 'untracked') includedUntracked[f.path] = include
        else excluded[f.path] = !include
      }
      return { excluded, includedUntracked }
    }),

  commit: async (push) => {
    const { root, status, message, amend, isIncluded } = get()
    if (!root || !status) return
    const selected = status.files.filter((f) => f.kind !== 'conflicted' && isIncluded(f))
    // Renames need both sides so the deletion of the old path is committed too.
    const paths = [...new Set(selected.flatMap((f) => (f.origPath ? [f.path, f.origPath] : [f.path])))]
    set({ busy: 'commit' })
    try {
      const hash = await Git.Commit(root, message, paths, amend)
      set({ message: '', amend: false, excluded: {}, includedUntracked: {} })
      showToast(`Committed ${hash}${paths.length ? ` (${selected.length} files)` : ''}`)
      if (push) {
        set({ busy: 'push' })
        await Git.Push(root)
        showToast('Pushed')
      }
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      set({ busy: null })
      void get().refresh()
    }
  },

  rollback: async (files) => {
    const root = get().root
    if (!root || files.length === 0) return
    const paths = [...new Set(files.flatMap((f) => (f.origPath ? [f.path, f.origPath] : [f.path])))]
    try {
      await Git.Rollback(root, paths)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      void get().refresh()
    }
  },

  remote: async (op) => {
    const root = get().root
    if (!root || get().busy) return
    set({ busy: op })
    try {
      if (op === 'fetch') await Git.Fetch(root)
      if (op === 'pull') await Git.Pull(root)
      if (op === 'push') await Git.Push(root)
      showToast(op === 'fetch' ? 'Fetched' : op === 'pull' ? 'Updated' : 'Pushed')
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      set({ busy: null })
      void get().refresh()
    }
  },
}))

// Single-flight status refresh with one trailing re-run, so bursts of agent edits cost
// at most two `git status` calls.
let inFlight: Promise<void> | null = null
let again = false

function refreshStatus(): Promise<void> {
  if (inFlight) {
    again = true
    return inFlight
  }
  inFlight = (async () => {
    do {
      again = false
      const root = useGit.getState().root
      if (!root) return
      try {
        const status = await Git.Status(root)
        if (useGit.getState().root !== root) return
        const prevHead = useGit.getState().status?.head
        useGit.setState((s) => ({
          status,
          headVersion: prevHead !== status.head ? s.headVersion + 1 : s.headVersion,
        }))
      } catch (err) {
        showToast(errorMessage(err), 'error')
        return
      }
    } while (again)
  })().finally(() => {
    inFlight = null
  })
  return inFlight
}

let debounce: ReturnType<typeof setTimeout> | undefined
function scheduleRefresh(delay = 250): void {
  clearTimeout(debounce)
  debounce = setTimeout(() => void refreshStatus(), delay)
}

let subscribed = false
export function subscribeGitEvents(): void {
  if (subscribed) return
  subscribed = true
  // HEAD / index / refs changed (commit, checkout, rebase — ours or an agent's).
  EventsOn('git:changed', () => {
    useGit.setState((s) => ({ headVersion: s.headVersion + 1 }))
    scheduleRefresh(100)
  })
  // Working-tree edits change the status too.
  EventsOn('fs:changed', () => scheduleRefresh())
  window.addEventListener('focus', () => scheduleRefresh(0))
  // Follow the opened folder.
  let lastRoot: string | null = null
  useWorkspace.subscribe((s) => {
    if (s.root !== lastRoot) {
      lastRoot = s.root
      void useGit.getState().load(s.root)
    }
  })
  void useGit.getState().load(useWorkspace.getState().root)
}
