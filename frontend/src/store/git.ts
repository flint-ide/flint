import { useEffect, useState } from 'react'
import { createStore } from '../lib/store'
import * as Git from '../../bindings/flint/gitapi'
import { EventsOn } from '../wails/runtime'
import type { git } from '../wails/models'
import { errorMessage } from '../api/backend'
import { confirmAction, showToast } from './dialog'
import { useWorkspace } from './workspace'
import { isWithin } from '../lib/fileTree'
import { followedRoot } from './activeRoot'
import { useEditor } from './editor'

export type FileChange = git.FileChange
export type GitStatus = git.Status
export type Branch = git.Branch
export type Worktree = git.Worktree

export { Git }

type RemoteOp = 'fetch' | 'pull' | 'push' | 'commit' | null

interface GitState {
  /** Workspace folder whose repository is shown (follows the active tab's folder). */
  folder: string | null
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
  /**
   * Bumps when HEAD of the opened work tree moves (commit, checkout, reset), so editors
   * refetch their gutter baseline, blame and history. Index writes leave it alone.
   */
  headVersion: number
  /** Bumps on every git:changed (index, refs, stashes, other worktrees' HEAD). */
  refsVersion: number
  /** Changed-file count per workspace folder (multi-root switcher); absent = not a repo. */
  counts: Record<string, number>

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
  folder: null,
  counts: {},
  root: null,
  isRepo: false,
  status: null,
  excluded: {},
  includedUntracked: {},
  message: '',
  amend: false,
  busy: null,
  headVersion: 0,
  refsVersion: 0,

  load: async (folder) => {
    set({ folder })
    if (!folder) {
      set({ root: null, isRepo: false, status: null })
      return
    }
    try {
      const info = await Git.Info(folder)
      if (get().folder !== folder) return // switched again meanwhile
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

/** Asks before discarding changes (new files are deleted), then rolls back. */
export async function confirmRollback(files: FileChange[]): Promise<void> {
  if (files.length === 0) return
  const deletes = files.filter((f) => f.kind === 'untracked' || f.kind === 'added').length
  const what = files.length === 1 ? files[0].path : `${files.length} files`
  const extra = deletes ? ` ${deletes} new file(s) will be deleted.` : ''
  if (await confirmAction(`Rollback changes in ${what}?${extra}`, 'Rollback')) await useGit.getState().rollback(files)
}

/** Changed files at or under any of the absolute paths (file tree ▸ Rollback). */
export function changesUnder(paths: string[]): FileChange[] {
  const { root, status } = useGit.getState()
  if (!root || !status) return []
  return status.files.filter((f) => paths.some((p) => isWithin(p, `${root}/${f.path}`)))
}

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
        const prev = useGit.getState().status
        // Most refreshes (an agent's `git status`, a save) change nothing: keep the
        // old object so the Commit panel and status bar don't render.
        if (sameStatus(prev, status)) continue
        useGit.setState((s) => ({
          status,
          headVersion: prev?.head !== status.head ? s.headVersion + 1 : s.headVersion,
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

function sameStatus(a: GitStatus | null, b: GitStatus): boolean {
  if (!a) return false
  if (a.head !== b.head || a.branch !== b.branch || a.upstream !== b.upstream) return false
  if (a.ahead !== b.ahead || a.behind !== b.behind || a.operation !== b.operation) return false
  const fa = a.files ?? []
  const fb = b.files ?? []
  return (
    fa.length === fb.length &&
    fa.every((f, i) => {
      const g = fb[i]
      return f.path === g.path && f.origPath === g.origPath && f.kind === g.kind && f.index === g.index && f.worktree === g.worktree
    })
  )
}

/** headVersion for the opened work tree; refsVersion for another one (see useHeadVersion). */
export function headVersionOf(s: Pick<GitState, 'root' | 'headVersion' | 'refsVersion'>, workTree: string): number {
  return workTree === s.root ? s.headVersion : s.refsVersion
}

const dirname = (p: string) => p.slice(0, Math.max(p.lastIndexOf('/'), 1))

/**
 * Changes when HEAD of the work tree containing path may have moved. Only the opened
 * work tree's HEAD is tracked (headVersion); a file in another one (an agent's nested
 * worktree) follows every git:changed instead.
 */
export function useHeadVersion(path: string): number {
  const root = useGit((s) => s.root)
  const [workTree, setWorkTree] = useState<string | null>(null)
  useEffect(() => {
    let cancelled = false
    Git.Info(dirname(path)).then(
      (info) => !cancelled && setWorkTree(info.isRepo ? info.root : null),
      () => undefined, // keep the guess below
    )
    return () => {
      cancelled = true
    }
  }, [path, root])
  // Until Info answers, a file under the opened folder is assumed to be in its work tree.
  const tree = workTree ?? (root && path.startsWith(`${root}/`) ? root : '')
  return useGit((s) => headVersionOf(s, tree))
}

/** Counts changes of every workspace folder for the Commit panel's switcher (several folders only). */
async function refreshCounts(): Promise<void> {
  const roots = useWorkspace.getState().roots
  if (roots.length < 2) {
    if (Object.keys(useGit.getState().counts).length) useGit.setState({ counts: {} })
    return
  }
  const results = await Promise.all(roots.map((r) => Git.Status(r).then((st) => (st.files ?? []).length, () => -1)))
  const counts: Record<string, number> = {}
  roots.forEach((r, i) => {
    if (results[i] >= 0) counts[r] = results[i]
  })
  // Most refreshes change no count: keep the old object so the switcher doesn't render.
  const prev = useGit.getState().counts
  const keys = Object.keys(counts)
  if (keys.length === Object.keys(prev).length && keys.every((k) => prev[k] === counts[k])) return
  useGit.setState({ counts })
}

let debounce: ReturnType<typeof setTimeout> | undefined
function scheduleRefresh(delay = 250): void {
  clearTimeout(debounce)
  debounce = setTimeout(() => {
    void refreshStatus()
    void refreshCounts()
  }, delay)
}

/** Shows the repository of the active tab's workspace folder (like IntelliJ's branch widget). */
function followActiveFolder(): void {
  const target = followedRoot(useGit.getState().folder)
  if (target !== useGit.getState().folder) void useGit.getState().load(target)
}

let subscribed = false
export function subscribeGitEvents(): void {
  if (subscribed) return
  subscribed = true
  // HEAD / index / refs changed (commit, checkout, rebase — ours or an agent's).
  // HEAD itself is compared by the status refresh: index-only writes (an agent's
  // `git add`) must not make every editor re-baseline, re-blame and reload history.
  EventsOn('git:changed', () => {
    useGit.setState((s) => ({ refsVersion: s.refsVersion + 1 }))
    scheduleRefresh(100)
  })
  // Working-tree edits change the status too.
  EventsOn('fs:changed', () => scheduleRefresh())
  window.addEventListener('focus', () => scheduleRefresh(0))
  // Follow the workspace's folders and the active tab's folder among them.
  useWorkspace.subscribe((s, prev) => {
    if (s.roots !== prev.roots || s.root !== prev.root) {
      followActiveFolder()
      void refreshCounts()
    }
  })
  useEditor.subscribe((s, prev) => {
    if (s.activeId !== prev.activeId) followActiveFolder()
  })
  followActiveFolder()
}
