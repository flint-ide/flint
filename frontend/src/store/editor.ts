import { createStore } from '../lib/store'
import type { EditorView } from '@codemirror/view'
import type { ScratchKind, ScratchPad } from '../api/backend'
import { isWithin, remapPath } from '../lib/fileTree'

export interface Reveal {
  line: number // 1-based
  from: number // UTF-16 column offsets within the line
  to: number
  nonce: number // changes on every request so the same location can be revealed twice
  /** Move the cursor without focusing the editor (session restore). */
  quiet?: boolean
}

export interface FileTab {
  id: string
  kind: 'file'
  path: string
  title: string
  dirty: boolean
  reveal?: Reveal
}

export interface ScratchTab {
  id: string
  kind: 'scratch'
  scratchId: string
  scratchKind: ScratchKind
  title: string
  dirty: boolean
}

export interface DiffTab {
  id: string
  kind: 'diff'
  /** Absolute path of the working-tree file. */
  path: string
  /** Absolute path the left side is read from (differs for renames). */
  basePath: string
  /** Work-tree root used for git calls. */
  repoRoot: string
  title: string
  dirty: boolean // always false: diffs are read-only
  /** Revision of the left side (default HEAD). */
  baseRev?: string
  /** Revision of the right side; unset means the working tree (live). */
  rev?: string
  /** Header labels for the two sides. */
  baseLabel?: string
  label?: string
}

/** A diff between two revisions of a file (or a revision and the working tree). */
export type RevisionDiff = Required<Pick<DiffTab, 'repoRoot' | 'path' | 'basePath' | 'baseRev'>> &
  Pick<DiffTab, 'rev' | 'baseLabel' | 'label'>

/** The merge editor for a conflicted file (components/editor/MergeView.tsx). */
export interface MergeTab {
  id: string
  kind: 'merge'
  /** Absolute path of the conflicted file. */
  path: string
  /** Work-tree root used for git calls. */
  repoRoot: string
  title: string
  dirty: boolean
}

/** Commits that touched a file (git log --follow). */
export interface HistoryTab {
  id: string
  kind: 'history'
  /** Absolute path of the file. */
  path: string
  title: string
  dirty: boolean // always false
}

/** A shell; the PTY itself is managed by the terminal store. */
export interface TerminalTab {
  id: string
  kind: 'terminal'
  termId: string
  title: string
  dirty: boolean // always false
}

/** A conversation with a coding agent. */
export interface ChatTab {
  id: string
  kind: 'chat'
  sessionId: string
  title: string
  dirty: boolean // always false
}

/** Output (and test results) of a run configuration or test run; see store/run.ts. */
export interface RunTab {
  id: string
  kind: 'run'
  runKey: string
  title: string
  dirty: boolean // always false
}

/** The debug session (store/debug.ts); there is at most one. */
export interface DebugTab {
  id: 'debug'
  kind: 'debug'
  title: string
  dirty: boolean // always false
}

/** A recorded profile shown as a flame graph; its data lives in store/profile.ts. */
export interface ProfileTab {
  id: string
  kind: 'profile'
  profileId: string
  title: string
  dirty: boolean // always false
}

/** Responses to requests of a .http file (store/httpClient.ts), one tab per file. */
export interface HttpTab {
  id: string
  kind: 'http'
  /** Absolute path of the .http file. */
  path: string
  title: string
  dirty: boolean // always false
}

/** Settings (⌘,); there is at most one. */
export interface SettingsTab {
  id: 'settings'
  kind: 'settings'
  title: string
  dirty: boolean // always false: settings save as they change
}

/** A GitHub pull request (store/pullRequests.ts) of the project at root. */
export interface PullRequestTab {
  id: string
  kind: 'pr'
  /** Workspace folder whose GitHub repository the pull request belongs to. */
  root: string
  number: number
  title: string
  dirty: boolean // always false
}

/** One changed file of a pull request: merge base vs head, with review threads inline. */
export interface PullRequestDiffTab {
  id: string
  kind: 'prdiff'
  root: string
  number: number
  /** Repository-relative path (head side). */
  path: string
  title: string
  dirty: boolean // always false
}

/** The failed-steps log of a GitHub Actions run (or one job), read in store/ci.ts. */
export interface CILogTab {
  id: string
  kind: 'cilog'
  /** Workspace folder the run's repository is checked out in; log paths resolve against it. */
  root: string
  runId: number
  /** 0: every failed job of the run. */
  jobId: number
  title: string
  dirty: boolean // always false
}

export type Tab =
  | FileTab
  | ScratchTab
  | DiffTab
  | MergeTab
  | HistoryTab
  | TerminalTab
  | ChatTab
  | RunTab
  | DebugTab
  | ProfileTab
  | HttpTab
  | PullRequestTab
  | PullRequestDiffTab
  | CILogTab
  | SettingsTab

const HISTORY_LIMIT = 50

interface EditorState {
  tabs: Tab[]
  activeId: string | null
  /** Previously active tabs (most recent last), for ⌘[ like IntelliJ's Back. */
  backStack: string[]
  forwardStack: string[]
  /** background: add the tab without switching to it (session restore). */
  openTerminal: (termId: string, title: string, background?: boolean) => void
  openChat: (sessionId: string, title: string, background?: boolean) => void
  /** Opens (or retitles and shows) a run tab; background: only retitle / add. */
  openRun: (runKey: string, title: string, background?: boolean) => void
  openDebug: (title: string) => void
  openProfile: (profileId: string, title: string) => void
  openSettings: () => void
  back: () => void
  forward: () => void
  openFile: (path: string, reveal?: Omit<Reveal, 'nonce'>) => void
  /** Appends a tab without switching to it; no-op when it is already open. */
  addTab: (tab: Tab) => void
  openScratch: (sc: ScratchPad) => void
  openDiff: (repoRoot: string, path: string, basePath?: string) => void
  openRevisionDiff: (d: RevisionDiff) => void
  openMerge: (repoRoot: string, path: string) => void
  openHistory: (path: string) => void
  /** Opens the response tab of a .http file, right after the file's own tab. */
  openHttpResponse: (path: string) => void
  openPullRequest: (root: string, number: number, title: string) => void
  /** Opens a changed file of a pull request, right after the pull request's tab. */
  openPullRequestDiff: (root: string, number: number, path: string) => void
  openCILog: (root: string, runId: number, jobId: number, title: string) => void
  renameScratchTab: (scratchId: string, title: string) => void
  /** Renames a tab in place (terminals follow what they run). */
  retitleTab: (id: string, title: string) => void
  closeTab: (id: string) => void
  closeScratch: (scratchId: string) => void
  /** Follows a rename on disk: file, diff and history tabs at or under `from` move to `to`. */
  renamePath: (from: string, to: string) => void
  /** Closes file, diff, history and response tabs at or under the given (deleted) paths, and pull requests of removed projects. */
  closePaths: (paths: string[]) => void
  setActive: (id: string) => void
  setDirty: (id: string, dirty: boolean) => void
}

export const fileTabId = (path: string) => `file:${path}`
export const scratchTabId = (id: string) => `scratch:${id}`
export const diffTabId = (path: string) => `diff:${path}`
export const revisionDiffTabId = (d: Pick<DiffTab, 'path' | 'baseRev' | 'rev'>) =>
  `diff:${d.baseRev ?? 'HEAD'}..${d.rev ?? ''}:${d.path}`
export const mergeTabId = (path: string) => `merge:${path}`
export const historyTabId = (path: string) => `history:${path}`
export const profileTabId = (profileId: string) => `profile:${profileId}`
export const httpTabId = (path: string) => `http:${path}`
export const pullRequestTabId = (root: string, number: number) => `pr:${root}#${number}`
export const ciLogTabId = (root: string, runId: number, jobId: number) => `cilog:${root}#${runId}/${jobId}`
export const pullRequestDiffTabId = (root: string, number: number, path: string) => `prdiff:${root}#${number}:${path}`
const baseName = (path: string) => path.split('/').pop() ?? path

let revealSeq = 0

/** Switches to id, remembering the previous tab for Back. */
function activate(s: EditorState, id: string): Partial<EditorState> {
  if (s.activeId === id) return {} // no-op: subscribers are not notified
  const backStack = s.activeId ? [...s.backStack, s.activeId].slice(-HISTORY_LIMIT) : s.backStack
  return { activeId: id, backStack, forwardStack: [] }
}

export const useEditor = createStore<EditorState>((set) => ({
  tabs: [],
  activeId: null,
  backStack: [],
  forwardStack: [],

  openTerminal: (termId, title, background) =>
    set((s) => {
      const id = `term:${termId}`
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: TerminalTab = { id, kind: 'terminal', termId, title, dirty: false }
      if (background) return { tabs: [...s.tabs, tab] }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openChat: (sessionId, title, background) =>
    set((s) => {
      const id = `chat:${sessionId}`
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: ChatTab = { id, kind: 'chat', sessionId, title, dirty: false }
      if (background) return { tabs: [...s.tabs, tab] }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openRun: (runKey, title, background) =>
    set((s) => {
      const id = `run:${runKey}`
      if (s.tabs.some((t) => t.id === id)) {
        const tabs = s.tabs.map((t) => (t.id === id && t.title !== title ? { ...t, title } : t))
        return background ? { tabs } : { tabs, ...activate(s, id) }
      }
      const tab: RunTab = { id, kind: 'run', runKey, title, dirty: false }
      if (background) return { tabs: [...s.tabs, tab] }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openDebug: (title) =>
    set((s) => {
      const id = 'debug'
      if (s.tabs.some((t) => t.id === id)) return { tabs: s.tabs.map((t) => (t.id === id ? { ...t, title } : t)), ...activate(s, id) }
      const tab: DebugTab = { id, kind: 'debug', title, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openProfile: (profileId, title) =>
    set((s) => {
      const id = profileTabId(profileId)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: ProfileTab = { id, kind: 'profile', profileId, title, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openSettings: () =>
    set((s) => {
      const id = 'settings'
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: SettingsTab = { id, kind: 'settings', title: 'Settings', dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  back: () =>
    set((s) => {
      const stack = s.backStack.filter((id) => id !== s.activeId && s.tabs.some((t) => t.id === id))
      const target = stack.pop()
      if (!target) return {}
      return { activeId: target, backStack: stack, forwardStack: s.activeId ? [...s.forwardStack, s.activeId] : s.forwardStack }
    }),

  forward: () =>
    set((s) => {
      const stack = s.forwardStack.filter((id) => id !== s.activeId && s.tabs.some((t) => t.id === id))
      const target = stack.pop()
      if (!target) return {}
      return { activeId: target, forwardStack: stack, backStack: s.activeId ? [...s.backStack, s.activeId] : s.backStack }
    }),

  openFile: (path, reveal) =>
    set((s) => {
      const id = fileTabId(path)
      const nextReveal = reveal ? { ...reveal, nonce: ++revealSeq } : undefined
      const existing = s.tabs.find((t) => t.id === id)
      if (existing) {
        const tabs = s.tabs.map((t) => (t.id === id && nextReveal ? { ...t, reveal: nextReveal } : t))
        return { tabs, ...activate(s, id) }
      }
      const tab: FileTab = { id, kind: 'file', path, title: baseName(path), dirty: false, reveal: nextReveal }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  addTab: (tab) =>
    set((s) => {
      if (s.tabs.some((t) => t.id === tab.id)) return {}
      const withNonce = tab.kind === 'file' && tab.reveal ? { ...tab, reveal: { ...tab.reveal, nonce: ++revealSeq } } : tab
      return { tabs: [...s.tabs, withNonce] }
    }),

  openScratch: (sc) =>
    set((s) => {
      const id = scratchTabId(sc.id)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: ScratchTab = {
        id,
        kind: 'scratch',
        scratchId: sc.id,
        scratchKind: sc.kind as ScratchKind,
        title: sc.name,
        dirty: false,
      }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openDiff: (repoRoot, path, basePath = path) =>
    set((s) => {
      const id = diffTabId(path)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: DiffTab = { id, kind: 'diff', path, basePath, repoRoot, title: `Δ ${baseName(path)}`, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openRevisionDiff: (d) =>
    set((s) => {
      const id = revisionDiffTabId(d)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const at = d.rev ? ` @ ${d.label ?? d.rev.slice(0, 7)}` : ` vs ${d.baseLabel ?? d.baseRev.slice(0, 7)}`
      const tab: DiffTab = { id, kind: 'diff', ...d, title: `Δ ${baseName(d.path)}${at}`, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openMerge: (repoRoot, path) =>
    set((s) => {
      const id = mergeTabId(path)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: MergeTab = { id, kind: 'merge', path, repoRoot, title: `Merge ${baseName(path)}`, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openHistory: (path) =>
    set((s) => {
      const id = historyTabId(path)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: HistoryTab = { id, kind: 'history', path, title: `History: ${baseName(path)}`, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openHttpResponse: (path) =>
    set((s) => {
      const id = httpTabId(path)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: HttpTab = { id, kind: 'http', path, title: `Response: ${baseName(path)}`, dirty: false }
      const after = s.tabs.some((t) => t.id === fileTabId(path)) ? fileTabId(path) : s.activeId
      return { tabs: insertAfterActive(s.tabs, after, tab), ...activate(s, id) }
    }),

  openPullRequest: (root, number, title) =>
    set((s) => {
      const id = pullRequestTabId(root, number)
      const label = `#${number} ${title}`
      if (s.tabs.some((t) => t.id === id)) {
        return { tabs: s.tabs.map((t) => (t.id === id && t.title !== label ? { ...t, title: label } : t)), ...activate(s, id) }
      }
      const tab: PullRequestTab = { id, kind: 'pr', root, number, title: label, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openPullRequestDiff: (root, number, path) =>
    set((s) => {
      const id = pullRequestDiffTabId(root, number, path)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: PullRequestDiffTab = { id, kind: 'prdiff', root, number, path, title: `Δ ${baseName(path)} #${number}`, dirty: false }
      // Next to the pull request (after the files already opened from it), else after the active tab.
      const prId = pullRequestTabId(root, number)
      const siblings = s.tabs.filter((t) => t.id === prId || (t.kind === 'prdiff' && t.root === root && t.number === number))
      const after = siblings.length ? siblings[siblings.length - 1].id : s.activeId
      return { tabs: insertAfterActive(s.tabs, after, tab), ...activate(s, id) }
    }),

  openCILog: (root, runId, jobId, title) =>
    set((s) => {
      const id = ciLogTabId(root, runId, jobId)
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: CILogTab = { id, kind: 'cilog', root, runId, jobId, title, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  retitleTab: (id, title) =>
    set((s) => (s.tabs.some((t) => t.id === id && t.title !== title) ? { tabs: s.tabs.map((t) => (t.id === id ? { ...t, title } : t)) } : s)),

  renameScratchTab: (scratchId, title) =>
    set((s) => ({
      tabs: s.tabs.map((t) => (t.kind === 'scratch' && t.scratchId === scratchId ? { ...t, title } : t)),
    })),

  closeTab: (id) => set((s) => removeTab(s, id)),

  closeScratch: (scratchId) => set((s) => removeTab(s, scratchTabId(scratchId))),

  renamePath: (from, to) =>
    set((s) => {
      const ids = new Map<string, string>()
      const tabs = s.tabs.map((t) => {
        const moved = movedTab(t, from, to)
        if (!moved) return t
        ids.set(t.id, moved.id)
        return moved
      })
      if (ids.size === 0) return {}
      const remap = (id: string) => ids.get(id) ?? id
      return {
        tabs,
        activeId: s.activeId && remap(s.activeId),
        backStack: s.backStack.map(remap),
        forwardStack: s.forwardStack.map(remap),
      }
    }),

  closePaths: (paths) =>
    set((s) => {
      const gone = s.tabs.filter(
        (t) =>
          ((t.kind === 'file' || t.kind === 'diff' || t.kind === 'merge' || t.kind === 'history' || t.kind === 'http') && paths.some((p) => isWithin(p, t.path))) ||
          // Pull requests belong to a project: they go when it leaves the workspace.
          ((t.kind === 'pr' || t.kind === 'prdiff' || t.kind === 'cilog') && paths.some((p) => isWithin(p, t.root))),
      )
      return gone.reduce<Partial<EditorState>>((acc, t) => ({ ...acc, ...removeTab({ ...s, ...acc } as EditorState, t.id) }), {})
    }),

  setActive: (id) => set((s) => activate(s, id)),

  setDirty: (id, dirty) =>
    set((s) => {
      const tab = s.tabs.find((t) => t.id === id)
      if (!tab || tab.dirty === dirty) return {}
      return { tabs: s.tabs.map((t) => (t.id === id ? { ...t, dirty } : t)) }
    }),
}))

/** A file, diff or history tab after a rename of from → to on disk; null when unaffected. */
function movedTab(t: Tab, from: string, to: string): Tab | null {
  if ((t.kind !== 'file' && t.kind !== 'diff' && t.kind !== 'history') || !isWithin(from, t.path)) return null
  const path = remapPath(t.path, from, to)
  switch (t.kind) {
    case 'file':
      return { ...t, id: fileTabId(path), path, title: baseName(path) }
    case 'history':
      return { ...t, id: historyTabId(path), path, title: `History: ${baseName(path)}` }
    case 'diff':
      if (!t.baseRev && !t.rev) return { ...t, id: diffTabId(path), path, basePath: remapPath(t.basePath, from, to), title: `Δ ${baseName(path)}` }
      // A committed side stays where it was in that commit; only the working-tree side moves.
      if (t.rev) return null
      return { ...t, id: revisionDiffTabId({ ...t, path }), path, title: `Δ ${baseName(path)} vs ${t.baseLabel ?? t.baseRev?.slice(0, 7)}` }
  }
}

function insertAfterActive(tabs: Tab[], activeId: string | null, tab: Tab): Tab[] {
  const idx = tabs.findIndex((t) => t.id === activeId)
  if (idx < 0) return [...tabs, tab]
  return [...tabs.slice(0, idx + 1), tab, ...tabs.slice(idx + 1)]
}

/** Closes a tab; when it was active, returns to the previously used tab (like IntelliJ). */
function removeTab(s: EditorState, id: string): Partial<EditorState> {
  const idx = s.tabs.findIndex((t) => t.id === id)
  if (idx < 0) return {}
  const tabs = s.tabs.filter((t) => t.id !== id)
  const backStack = s.backStack.filter((x) => x !== id)
  const forwardStack = s.forwardStack.filter((x) => x !== id)
  if (s.activeId !== id) return { tabs, backStack, forwardStack }
  const previous = [...backStack].reverse().find((x) => tabs.some((t) => t.id === x))
  const activeId = previous ?? tabs[Math.min(idx, tabs.length - 1)]?.id ?? null
  return { tabs, activeId, backStack: backStack.filter((x) => x !== activeId), forwardStack }
}

/** Save callbacks registered by mounted editors, keyed by tab id. */
const savers = new Map<string, () => Promise<void>>()

export function registerSaver(tabId: string, save: () => Promise<void>): () => void {
  savers.set(tabId, save)
  return () => {
    if (savers.get(tabId) === save) savers.delete(tabId)
  }
}

export async function saveTab(tabId: string): Promise<void> {
  await savers.get(tabId)?.()
}

/** Live editor views by tab id, for features that need the real selection (⌘L). */
const views = new Map<string, EditorView>()

export function registerView(tabId: string, view: EditorView): () => void {
  views.set(tabId, view)
  return () => {
    if (views.get(tabId) === view) views.delete(tabId)
  }
}

export function viewFor(tabId: string): EditorView | undefined {
  return views.get(tabId)
}
