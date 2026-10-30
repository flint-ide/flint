// Pure helpers for session snapshots (see store/session.ts).
import { parseBreakpoints, type BreakpointMap } from './breakpoints'
import type { ChatItem, ChatState, ToolCall } from './agentReducer'

export const SNAPSHOT_VERSION = 1
const CHAT_ITEMS = 200
const TEXT_LIMIT = 8_000
const DIFF_LIMIT = 40_000

/** A chat as kept in a snapshot. */
export interface SavedChat {
  id: string
  agent: string
  cwd: string
  model?: string
  chat: ChatState
  draft: string
}

export type SavedTab =
  | { kind: 'file'; path: string; line?: number; col?: number }
  | { kind: 'scratch'; scratchId: string; scratchKind: 'sql' | 'markdown' | 'text'; title: string }
  | {
      kind: 'diff'
      path: string
      basePath: string
      repoRoot: string
      title: string
      baseRev?: string
      rev?: string
      baseLabel?: string
      label?: string
    }
  | { kind: 'history'; path: string }
  | { kind: 'terminal'; title: string; cwd: string; output: string }
  | { kind: 'chat'; title: string; chat: SavedChat }

export interface Snapshot {
  version: number
  /** Index of the active tab in tabs (-1: none). */
  active: number
  tabs: SavedTab[]
  /** Selected run configuration id (running processes are never saved). */
  runConfig?: string
  /** Debugger breakpoints: 1-based lines by absolute file path. */
  breakpoints?: BreakpointMap
}

/** Whether path is root or lies under it. */
export function inside(path: string, root: string): boolean {
  return path === root || path.startsWith(root.endsWith('/') ? root : `${root}/`)
}

/** Parses a saved snapshot; null for anything Flint did not write (or an older format). */
export function parseSnapshot(raw: string): Snapshot | null {
  if (!raw) return null
  try {
    const snap = JSON.parse(raw) as Snapshot
    if (snap?.version !== SNAPSHOT_VERSION || !Array.isArray(snap.tabs)) return null
    return {
      version: snap.version,
      active: Number.isInteger(snap.active) ? snap.active : -1,
      tabs: snap.tabs.filter(isTab),
      ...(typeof snap.runConfig === 'string' && snap.runConfig ? { runConfig: snap.runConfig } : {}),
      ...(snap.breakpoints ? { breakpoints: parseBreakpoints(snap.breakpoints) } : {}),
    }
  } catch {
    return null
  }
}

const KINDS = new Set(['file', 'scratch', 'diff', 'history', 'terminal', 'chat'])
const isTab = (t: unknown): t is SavedTab => typeof t === 'object' && t !== null && KINDS.has((t as SavedTab).kind)

/** The parts of a chat session autosave looks at. */
export interface AutosavedChat {
  chat: ChatState
  draft: string
  dormant?: boolean
}

/**
 * Whether a change to the chat sessions is worth an autosave: a chat opened or closed,
 * a turn started or ended, an item added, the draft, dormancy or usage changed. Text
 * streaming into the last item is not; the turn's end saves it.
 */
export function chatsNeedSave(prev: Record<string, AutosavedChat>, next: Record<string, AutosavedChat>): boolean {
  if (prev === next) return false
  const ids = Object.keys(prev)
  if (ids.length !== Object.keys(next).length) return true
  return ids.some((id) => {
    const a = prev[id]
    const b = next[id]
    if (!b) return true
    if (a === b) return false
    return (
      a.chat.busy !== b.chat.busy ||
      a.chat.items.length !== b.chat.items.length ||
      a.chat.totals !== b.chat.totals ||
      a.draft !== b.draft ||
      a.dormant !== b.dormant
    )
  })
}

// Chat states are immutable, so a trimmed copy stays valid as long as its chat lives.
const trimmed = new WeakMap<ChatState, ChatState>()

/** Keeps a transcript small: recent items only, long outputs cut, big diffs dropped. */
export function trimChat(chat: ChatState): ChatState {
  const cached = trimmed.get(chat)
  if (cached) return cached
  const out = { ...chat, busy: false, items: chat.items.slice(-CHAT_ITEMS).map(trimItem) }
  trimmed.set(chat, out)
  return out
}

function trimItem(it: ChatItem): ChatItem {
  if (it.type === 'tool') return { ...it, call: trimCall(it.call) }
  if (it.type === 'permission') return { ...it, request: { ...it.request, toolCall: trimCall(it.request.toolCall) } }
  return it
}

function trimCall(call: ToolCall): ToolCall {
  const cut = (s?: string) => (s && s.length > TEXT_LIMIT ? `${s.slice(0, TEXT_LIMIT)}\n…` : s)
  return {
    ...call,
    input: cut(call.input),
    output: cut(call.output),
    diffs: call.diffs?.filter((d) => d.oldText.length + d.newText.length <= DIFF_LIMIT),
  }
}
