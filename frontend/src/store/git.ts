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

/** One repository of the workspace as the Commit panel lists it (several folders only). */
export interface RepoView {
  /** Workspace folder it was found from. */
  folder: string
  /** Work-tree root. */
  root: string
  status: GitStatus
}

/** Changed files of one repository (rollback, file tree). */
export interface RepoChanges {
  root: string
  files: FileChange[]
}

/** Key of a file in the commit selection: absolute, so several repositories never clash. */
export const selectionKey = (root: string, f: Pick<FileChange, 'path'>) => `${root}/${f.path}`

type RemoteOp = 'fetch' | 'pull' | 'push' | 'commit' | null

interface GitState {
  /** Workspace folder whose repository is shown (follows the active tab's folder). */
  folder: string | null
  root: string | null // work-tree root (may differ from the opened folder)
  isRepo: boolean
  status: GitStatus | null
  /** Files (selectionKey) excluded from the next commit (IntelliJ includes all changes by default). */
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
  /** Every repository of the workspace, in folder order, one per work tree (several folders only). */
  repos: RepoView[]

  load: (folder: string | null) => Promise<void>
  refresh: () => Promise<void>
  /** refresh plus every other repository of the workspace (the Commit panel's ↻). */
  refreshAll: () => Promise<void>
  setMessage: (m: string) => void
  setAmend: (amend: boolean) => Promise<void>
  /** root: the file's repository (default: the shown one). */
  toggleFile: (f: FileChange, root?: string) => void
  setGroup: (files: FileChange[], include: boolean, root?: string) => void
  isIncluded: (f: FileChange, root?: string) => boolean
  /** Commits the ticked files of every repository (one commit each, same message). */
  commit: (push: boolean) => Promise<void>
  rollback: (files: FileChange[], root?: string) => Promise<void>
  remote: (op: 'fetch' | 'pull' | 'push') => Promise<void>
}

export const useGit = createStore<GitState>((set, get) => ({
  folder: null,
  counts: {},
  repos: [],
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
      // Ticks are keyed by absolute path, so switching repositories keeps each one's.
      set({ root: info.isRepo ? info.root : null, isRepo: info.isRepo, status: null })
      if (info.isRepo) await get().refresh()
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  refresh: () => refreshStatus(),
  refreshAll: async () => {
    await Promise.all([refreshStatus(), refreshRepos()])
  },

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

  isIncluded: (f, root = get().root ?? '') => {
    const key = selectionKey(root, f)
    return f.kind === 'untracked' ? !!get().includedUntracked[key] : !get().excluded[key]
  },

  toggleFile: (f, root = get().root ?? '') => {
    const key = selectionKey(root, f)
    set((s) =>
      f.kind === 'untracked'
        ? { includedUntracked: { ...s.includedUntracked, [key]: !s.includedUntracked[key] } }
        : { excluded: { ...s.excluded, [key]: !s.excluded[key] } },
    )
  },

  setGroup: (files, include, root = get().root ?? '') =>
    set((s) => {
      const excluded = { ...s.excluded }
      const includedUntracked = { ...s.includedUntracked }
      for (const f of files) {
        const key = selectionKey(root, f)
        if (f.kind === 'untracked') includedUntracked[key] = include
        else excluded[key] = !include
      }
      return { excluded, includedUntracked }
    }),

  commit: async (push) => {
    const { message, amend } = get()
    const targets = commitTargets(get())
    if (targets.length === 0) return
    const many = targets.length > 1
    const done: string[] = []
    set({ busy: 'commit' })
    try {
      for (const t of targets) {
        // Renames need both sides so the deletion of the old path is committed too.
        const paths = [...new Set(t.files.flatMap((f) => (f.origPath ? [f.path, f.origPath] : [f.path])))]
        const hash = await Git.Commit(t.root, message, paths, amend)
        done.push(`${hash}${many ? ` in ${baseName(t.root)}` : ''}${paths.length ? ` (${t.files.length} files)` : ''}`)
      }
      // Forget the ticks of the committed repositories only: others keep what was unticked.
      const committed = (key: string) => targets.some((t) => key.startsWith(`${t.root}/`))
      const keep = (m: Record<string, boolean>) => Object.fromEntries(Object.entries(m).filter(([k]) => !committed(k)))
      set((s) => ({ message: '', amend: false, excluded: keep(s.excluded), includedUntracked: keep(s.includedUntracked) }))
      showToast(`Committed ${done.join(', ')}`)
      if (push) {
        set({ busy: 'push' })
        const failed: string[] = []
        for (const t of targets) {
          try {
            await Git.Push(t.root)
          } catch (err) {
            failed.push(`${many ? `${baseName(t.root)}: ` : ''}${errorMessage(err)}`)
          }
        }
        if (failed.length) showToast(`Push failed — ${failed.join('; ')}`, 'error')
        else showToast(many ? `Pushed ${targets.length} repositories` : 'Pushed')
      }
    } catch (err) {
      // Repositories committed before the failure keep their commits: say which.
      showToast(`${done.length ? `Committed ${done.join(', ')}; then: ` : ''}${errorMessage(err)}`, 'error')
    } finally {
      set({ busy: null })
      void get().refresh()
      void refreshRepos()
    }
  },

  rollback: async (files, root = get().root ?? '') => {
    if (!root || files.length === 0) return
    const paths = [...new Set(files.flatMap((f) => (f.origPath ? [f.path, f.origPath] : [f.path])))]
    try {
      await Git.Rollback(root, paths)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      void get().refresh()
      void refreshRepos()
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

const baseName = (p: string) => p.slice(p.lastIndexOf('/') + 1)

/** Repositories with their freshest status: the shown one's comes from its own refresh. */
function repoStatuses(s: Pick<GitState, 'root' | 'status' | 'repos'>): { root: string; status: GitStatus }[] {
  if (s.repos.length > 1) return s.repos.map((r) => ({ root: r.root, status: r.root === s.root ? (s.status ?? r.status) : r.status }))
  return s.root && s.status ? [{ root: s.root, status: s.status }] : []
}

/** What Commit commits: the ticked files of each repository (one commit per repository). */
export function commitTargets(s: Pick<GitState, 'root' | 'status' | 'repos' | 'amend' | 'isIncluded'>): RepoChanges[] {
  const ticked = repoStatuses(s).map((v) => ({
    root: v.root,
    files: (v.status.files ?? []).filter((f) => f.kind !== 'conflicted' && s.isIncluded(f, v.root)),
  }))
  // Amend rewrites the shown repository's last commit only (its message is the one loaded):
  // never another repository's, which may already be pushed.
  if (s.amend) return s.root ? [{ root: s.root, files: ticked.find((t) => t.root === s.root)?.files ?? [] }] : []
  return ticked.filter((t) => t.files.length > 0)
}

/** Asks before discarding changes (new files are deleted), then rolls back; root: their repository. */
export async function confirmRollback(files: FileChange[], root?: string): Promise<void> {
  return confirmRollbackIn([{ root: root ?? useGit.getState().root ?? '', files }])
}

/** confirmRollback over several repositories at once (file tree selection across folders). */
export async function confirmRollbackIn(groups: RepoChanges[]): Promise<void> {
  const files = groups.flatMap((g) => g.files)
  if (files.length === 0) return
  const deletes = files.filter((f) => f.kind === 'untracked' || f.kind === 'added').length
  const what = files.length === 1 ? files[0].path : `${files.length} files`
  const extra = deletes ? ` ${deletes} new file(s) will be deleted.` : ''
  if (!(await confirmAction(`Rollback changes in ${what}?${extra}`, 'Rollback'))) return
  for (const g of groups) if (g.files.length) await useGit.getState().rollback(g.files, g.root)
}

/** Changed files at or under any of the absolute paths, per repository (file tree ▸ Rollback). */
export function changesUnder(paths: string[]): RepoChanges[] {
  return repoStatuses(useGit.getState())
    .map((v) => ({ root: v.root, files: (v.status.files ?? []).filter((f) => paths.some((p) => isWithin(p, `${v.root}/${f.path}`))) }))
    .filter((g) => g.files.length > 0)
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

/**
 * Work-tree root per workspace folder ('' = not a repository), until the folders change.
 * Only repositories are cached, so a `git init` (or a failed lookup) is noticed next time.
 */
let infoCache = new Map<string, Promise<string>>()

function workTreeOf(folder: string): Promise<string> {
  let p = infoCache.get(folder)
  if (!p) {
    const cache = infoCache
    p = Git.Info(folder).then(
      (i) => {
        if (!i.isRepo) cache.delete(folder)
        return i.isRepo ? i.root : ''
      },
      () => {
        cache.delete(folder)
        return ''
      },
    )
    infoCache.set(folder, p)
  }
  return p
}

/** Bumps per refreshRepos call: only the latest one applies (calls overlap on slow repositories). */
let reposGeneration = 0

/** Status of every repository of the workspace for the Commit panel (several folders only). */
async function refreshRepos(): Promise<void> {
  const generation = ++reposGeneration
  const roots = useWorkspace.getState().roots
  if (roots.length < 2) {
    const s = useGit.getState()
    if (Object.keys(s.counts).length || s.repos.length) useGit.setState({ counts: {}, repos: [] })
    return
  }
  const trees = await Promise.all(roots.map(workTreeOf))
  const unique = [...new Set(trees.filter(Boolean))]
  const statuses = new Map(await Promise.all(unique.map(async (t) => [t, await Git.Status(t).catch(() => null)] as const)))
  // A newer refresh started meanwhile (its statuses are fresher), or the folders changed.
  if (generation !== reposGeneration || useWorkspace.getState().roots !== roots) return
  const counts: Record<string, number> = {}
  const repos: RepoView[] = []
  const seen = new Set<string>()
  roots.forEach((folder, i) => {
    const status = trees[i] ? statuses.get(trees[i]) : null
    if (!status) return
    counts[folder] = (status.files ?? []).length
    if (seen.has(trees[i])) return // two folders of one repository: list it once
    seen.add(trees[i])
    repos.push({ folder, root: trees[i], status })
  })
  // Most refreshes change nothing: keep the old objects so the panel doesn't render.
  const prev = useGit.getState()
  const keys = Object.keys(counts)
  const sameCounts = keys.length === Object.keys(prev.counts).length && keys.every((k) => prev.counts[k] === counts[k])
  const kept = repos.map((r, i) => {
    const old = prev.repos[i]
    return old && old.folder === r.folder && old.root === r.root && sameStatus(old.status, r.status) ? old : r
  })
  const sameRepos = kept.length === prev.repos.length && kept.every((r, i) => r === prev.repos[i])
  if (sameCounts && sameRepos) return
  useGit.setState({ ...(sameCounts ? {} : { counts }), ...(sameRepos ? {} : { repos: kept }) })
}

let debounce: ReturnType<typeof setTimeout> | undefined
function scheduleRefresh(delay = 250): void {
  clearTimeout(debounce)
  debounce = setTimeout(() => {
    void refreshStatus()
    void refreshRepos()
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
      if (s.roots !== prev.roots) infoCache = new Map()
      followActiveFolder()
      void refreshRepos()
    }
  })
  useEditor.subscribe((s, prev) => {
    if (s.activeId !== prev.activeId) followActiveFolder()
  })
  followActiveFolder()
  void refreshRepos()
}
