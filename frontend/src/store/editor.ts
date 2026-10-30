import { create } from 'zustand'
import type { EditorView } from '@codemirror/view'
import type { ScratchKind, ScratchPad } from '../api/backend'

export interface Reveal {
  line: number // 1-based
  from: number // UTF-16 column offsets within the line
  to: number
  nonce: number // changes on every request so the same location can be revealed twice
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

export type Tab = FileTab | ScratchTab | DiffTab | TerminalTab | ChatTab

const HISTORY_LIMIT = 50

interface EditorState {
  tabs: Tab[]
  activeId: string | null
  /** Previously active tabs (most recent last), for ⌘[ like IntelliJ's Back. */
  backStack: string[]
  forwardStack: string[]
  openTerminal: (termId: string, title: string) => void
  openChat: (sessionId: string, title: string) => void
  back: () => void
  forward: () => void
  openFile: (path: string, reveal?: Omit<Reveal, 'nonce'>) => void
  openScratch: (sc: ScratchPad) => void
  openDiff: (repoRoot: string, path: string, basePath?: string) => void
  renameScratchTab: (scratchId: string, title: string) => void
  closeTab: (id: string) => void
  closeScratch: (scratchId: string) => void
  setActive: (id: string) => void
  setDirty: (id: string, dirty: boolean) => void
}

const fileTabId = (path: string) => `file:${path}`
const scratchTabId = (id: string) => `scratch:${id}`
const baseName = (path: string) => path.split('/').pop() ?? path

let revealSeq = 0

/** Switches to id, remembering the previous tab for Back. */
function activate(s: EditorState, id: string): Partial<EditorState> {
  if (s.activeId === id) return { activeId: id }
  const backStack = s.activeId ? [...s.backStack, s.activeId].slice(-HISTORY_LIMIT) : s.backStack
  return { activeId: id, backStack, forwardStack: [] }
}

export const useEditor = create<EditorState>((set) => ({
  tabs: [],
  activeId: null,
  backStack: [],
  forwardStack: [],

  openTerminal: (termId, title) =>
    set((s) => {
      const id = `term:${termId}`
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: TerminalTab = { id, kind: 'terminal', termId, title, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  openChat: (sessionId, title) =>
    set((s) => {
      const id = `chat:${sessionId}`
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: ChatTab = { id, kind: 'chat', sessionId, title, dirty: false }
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
      const id = `diff:${path}`
      if (s.tabs.some((t) => t.id === id)) return activate(s, id)
      const tab: DiffTab = { id, kind: 'diff', path, basePath, repoRoot, title: `Δ ${baseName(path)}`, dirty: false }
      return { tabs: insertAfterActive(s.tabs, s.activeId, tab), ...activate(s, id) }
    }),

  renameScratchTab: (scratchId, title) =>
    set((s) => ({
      tabs: s.tabs.map((t) => (t.kind === 'scratch' && t.scratchId === scratchId ? { ...t, title } : t)),
    })),

  closeTab: (id) => set((s) => removeTab(s, id)),

  closeScratch: (scratchId) => set((s) => removeTab(s, scratchTabId(scratchId))),

  setActive: (id) => set((s) => activate(s, id)),

  setDirty: (id, dirty) =>
    set((s) => {
      const tab = s.tabs.find((t) => t.id === id)
      if (!tab || tab.dirty === dirty) return {}
      return { tabs: s.tabs.map((t) => (t.id === id ? { ...t, dirty } : t)) }
    }),
}))

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
