// Pure helpers for session snapshots (see store/session.ts).
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
  | { kind: 'diff'; path: string; basePath: string; repoRoot: string; title: string }
  | { kind: 'terminal'; title: string; cwd: string; output: string }
  | { kind: 'chat'; title: string; chat: SavedChat }

export interface Snapshot {
  version: number
  /** Index of the active tab in tabs (-1: none). */
  active: number
  tabs: SavedTab[]
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
    return { version: snap.version, active: Number.isInteger(snap.active) ? snap.active : -1, tabs: snap.tabs.filter(isTab) }
  } catch {
    return null
  }
}

const KINDS = new Set(['file', 'scratch', 'diff', 'terminal', 'chat'])
const isTab = (t: unknown): t is SavedTab => typeof t === 'object' && t !== null && KINDS.has((t as SavedTab).kind)

/** Keeps a transcript small: recent items only, long outputs cut, big diffs dropped. */
export function trimChat(chat: ChatState): ChatState {
  return { ...chat, busy: false, items: chat.items.slice(-CHAT_ITEMS).map(trimItem) }
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
