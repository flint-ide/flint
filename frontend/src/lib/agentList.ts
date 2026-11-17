// The Agents panel: every chat agent and worktree-task agent of the window in one
// list, the ones that need the user first. Pure, so the ordering and the "seen"
// rule are testable.
//
// State comes from one vocabulary, Go's agenttask states (idle, working, waiting,
// finished, failed, queued). A chat in a task's worktree takes the state Go derived
// for the task. Other chats have no Go tracker, so chatState maps the transcript the
// reducer built from the same session events onto the same rules (agenttask.Next).
import type { ChatState } from './agentReducer'
import type { TaskView } from './agentTasks'

export type AgentRunState = 'waiting' | 'failed' | 'finished-unseen' | 'working' | 'queued' | 'idle'

/** Most urgent first; the order of the list. */
const RANK: Record<AgentRunState, number> = { waiting: 0, failed: 1, 'finished-unseen': 2, working: 3, queued: 4, idle: 5 }

const LABELS: Record<AgentRunState, string> = {
  waiting: 'waiting for you',
  failed: 'failed',
  'finished-unseen': 'finished',
  working: 'working',
  queued: 'queued',
  idle: 'idle',
}

const HINTS: Record<AgentRunState, string> = {
  waiting: 'The agent asks for your approval in its chat',
  failed: 'The last turn ended with an error',
  'finished-unseen': 'The turn ended while you were in another tab',
  working: 'The agent is working',
  queued: 'Waits for a working task to settle: its chat has not started',
  idle: 'No turn is running',
}

export const stateLabel = (s: AgentRunState): string => LABELS[s]
export const stateHint = (s: AgentRunState): string => HINTS[s]

/** The states that ask something of the user. */
const NEEDS_USER: readonly AgentRunState[] = ['waiting', 'failed', 'finished-unseen']

export type SettledChatState = 'idle' | 'working' | 'waiting' | 'finished' | 'failed'

/** A chat's state from its transcript, by the rules of agenttask.Next. A restored (dormant) chat runs nothing. */
export function chatState(chat: ChatState, dormant: boolean): SettledChatState {
  if (dormant) return 'idle'
  if (chat.busy) return chat.items.some((it) => it.type === 'permission' && !it.settled) ? 'waiting' : 'working'
  const last = chat.items.findLast((it) => it.type === 'end')
  if (last?.type !== 'end') return 'idle'
  if (last.stopReason === 'end_turn') return 'finished'
  return last.stopReason === 'cancelled' ? 'idle' : 'failed'
}

const MAX_TITLE = 60

/** What a chat is about: its first message on one line. */
export function chatTitle(chat: ChatState): string {
  const first = chat.items.find((it) => it.type === 'user')
  const text = first?.type === 'user' ? first.text.replace(/\s+/g, ' ').trim() : ''
  if (!text) return 'New chat'
  return text.length > MAX_TITLE ? `${text.slice(0, MAX_TITLE - 1)}…` : text
}

/**
 * Chats whose latest turn ended while their tab was not the open one. A chat
 * stays here until its tab is shown or it closes; ended: chats whose turn just ended.
 */
export function nextUnseen(unseen: readonly string[], ended: readonly string[], active: string | null, live: readonly string[]): string[] {
  const keep = new Set(live)
  return [...new Set([...unseen, ...ended])].filter((id) => id !== active && keep.has(id))
}

export interface AgentChatInput {
  id: string
  agent: string
  cwd: string
  chat: ChatState
  dormant?: boolean
}

export interface AgentRow {
  /** Stable list key. */
  key: string
  /** A chat of a worktree task is a 'task' row (named after the task). */
  kind: 'chat' | 'task'
  sessionId?: string
  taskId?: string
  agent: string
  title: string
  root: string
  /** Last folder name of root. */
  project: string
  state: AgentRunState
}

const trimSlash = (p: string) => p.replace(/\/+$/, '')
const folderName = (p: string) => trimSlash(p).slice(trimSlash(p).lastIndexOf('/') + 1)

const taskStateOf = (task: string, unseen: boolean): AgentRunState => {
  switch (task) {
    case 'waiting':
    case 'working':
    case 'failed':
    case 'queued':
      return task
    case 'finished':
      return unseen ? 'finished-unseen' : 'idle'
    default:
      return 'idle'
  }
}

export interface AgentListInput {
  /** Chats in the order they were opened. */
  sessions: readonly AgentChatInput[]
  tasks: readonly TaskView[]
  /** Ids of chats from nextUnseen. */
  unseen: readonly string[]
}

/** Every live agent of the window, most urgent first (stable within a state). */
export function agentRows({ sessions, tasks, unseen }: AgentListInput): AgentRow[] {
  const unseenIds = new Set(unseen)
  const live = tasks.filter((t) => !t.stale)
  const rows: AgentRow[] = sessions.map((s): AgentRow => {
    const task = live.find((t) => trimSlash(t.worktree) === trimSlash(s.cwd))
    const mine = unseenIds.has(s.id)
    const base = { key: `chat:${s.id}`, sessionId: s.id, agent: s.agent, root: s.cwd, project: folderName(s.cwd) }
    if (task) return { ...base, kind: 'task', taskId: task.id, title: task.title, state: taskStateOf(task.state, mine) }
    const state = chatState(s.chat, !!s.dormant)
    return { ...base, kind: 'chat', title: chatTitle(s.chat), state: state === 'finished' ? (mine ? 'finished-unseen' : 'idle') : state }
  })
  // A queued task has no chat yet but is an agent that will start; settled tasks without a chat are the Tasks panel's.
  for (const t of live) {
    if (t.state !== 'queued' || sessions.some((s) => trimSlash(s.cwd) === trimSlash(t.worktree))) continue
    rows.push({ key: `task:${t.id}`, kind: 'task', taskId: t.id, agent: t.agent, title: t.title, root: t.worktree, project: folderName(t.worktree), state: 'queued' })
  }
  return rows.map((row, i) => ({ row, i })).sort((a, b) => RANK[a.row.state] - RANK[b.row.state] || a.i - b.i).map((x) => x.row)
}

/** How many agents wait for the user: waiting, failed or finished unseen. */
export const attentionCount = (rows: readonly AgentRow[]): number => rows.filter((r) => NEEDS_USER.includes(r.state)).length
