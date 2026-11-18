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

/** What the "waiting" state means for an agent CLI in a terminal tab: it signalled that it needs you. */
const TERMINAL_WAITING_HINT = 'The agent in this terminal asked for your attention (an approval or a question)'

export const stateLabel = (s: AgentRunState): string => LABELS[s]
export const stateHint = (s: AgentRunState, kind?: AgentRow['kind'], blocked?: string): string =>
  kind === 'terminal' && s === 'waiting' ? TERMINAL_WAITING_HINT : s === 'queued' && blocked ? blocked : HINTS[s]

/** The states that ask something of the user. */
const NEEDS_USER: readonly AgentRunState[] = ['waiting', 'failed', 'finished-unseen']
export const needsUser = (s: AgentRunState): boolean => NEEDS_USER.includes(s)

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
  /** A chat of a worktree task is a 'task' row (named after the task); an agent CLI in a terminal tab is a 'terminal' row. */
  kind: 'chat' | 'task' | 'terminal'
  sessionId?: string
  taskId?: string
  /** The terminal of a 'terminal' row. */
  terminalId?: string
  agent: string
  title: string
  root: string
  /** Last folder name of root. */
  project: string
  state: AgentRunState
  /** The chat's tab is settled: not drawn in the tab strip until the agent needs the user. */
  hidden: boolean
  /** Why a queued task does not start by itself, when it is more than a free slot. */
  blocked?: string
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

/**
 * A task chat's state: Go's task state, unless the chat's own transcript is
 * newer. The task list is re-read on Go's events, but a chat's events arrive
 * first (and the list is not followed at all while its panels are closed): a
 * live chat that waits or works says so at once, and one that ended is not
 * shown working from a stale list.
 */
export function taskChatState(task: string, chat: SettledChatState | null, unseen: boolean): AgentRunState {
  if (chat === 'waiting') return 'waiting'
  if (chat === 'working') return task === 'waiting' ? 'waiting' : 'working' // Go may know of a request the chat shows next
  if ((chat === 'finished' || chat === 'failed') && (task === 'working' || task === 'waiting')) {
    return chat === 'failed' ? 'failed' : unseen ? 'finished-unseen' : 'idle'
  }
  return taskStateOf(task, unseen)
}

/** The state an agent CLI in a terminal reports (Go: terminal.Agent* constants, event "term:agent"). */
export type TerminalAgentState = 'working' | 'waiting' | 'done' | 'idle'

export interface TerminalAgentInput {
  id: string
  title: string
  cwd: string
  /** The running command ("claude --resume"), '' at the prompt. */
  command: string
  /** Absent until the program reports one. */
  state?: TerminalAgentState
  exited?: boolean
}

export interface TerminalBadge {
  label: string
  /** The `task-state` look it borrows from the Agents panel. */
  look: 'working' | 'waiting' | 'finished'
  hint: string
}

/** The badge on a terminal's tab for what its agent reported; none for no agent or one that left (idle). */
export function terminalBadge(state: TerminalAgentState | undefined): TerminalBadge | null {
  switch (state) {
    case 'working':
      return { label: 'working', look: 'working', hint: 'The agent in this terminal is working' }
    case 'waiting':
      return { label: 'waiting', look: 'waiting', hint: TERMINAL_WAITING_HINT }
    case 'done':
      return { label: 'done', look: 'finished', hint: 'The agent in this terminal finished its turn' }
    default:
      return null
  }
}

/** The key under which a terminal agent is kept in the "unseen" list (a chat's is its session id). */
export const terminalAgentKey = (id: string): string => `term:${id}`

/** A terminal that runs an agent: it reported working, waiting or done, and its shell still lives. `idle` means the agent left. */
const isTerminalAgent = (t: TerminalAgentInput): boolean => !t.exited && (t.state === 'working' || t.state === 'waiting' || t.state === 'done')

function terminalRow(t: TerminalAgentInput, unseen: boolean): AgentRow {
  const state: AgentRunState = t.state === 'done' ? (unseen ? 'finished-unseen' : 'idle') : t.state === 'waiting' ? 'waiting' : 'working'
  return {
    key: terminalAgentKey(t.id), kind: 'terminal', terminalId: t.id, agent: t.command.split(' ')[0] || 'terminal', title: t.title,
    root: t.cwd, project: folderName(t.cwd), state, hidden: false,
  }
}

export interface AgentListInput {
  /** Chats in the order they were opened. */
  sessions: readonly AgentChatInput[]
  tasks: readonly TaskView[]
  /** Ids of chats from nextUnseen. */
  unseen: readonly string[]
  /** Ids of chats whose tab is settled (store/agents.ts). */
  settled?: readonly string[]
  /** Terminal tabs (store/terminal.ts); the ones running an agent get a row. */
  terminals?: readonly TerminalAgentInput[]
}

/** Every live agent of the window, most urgent first (stable within a state). */
export function agentRows({ sessions, tasks, unseen, settled = [], terminals = [] }: AgentListInput): AgentRow[] {
  const unseenIds = new Set(unseen)
  const settledIds = new Set(settled)
  const live = tasks.filter((t) => !t.stale)
  const rows: AgentRow[] = sessions.map((s): AgentRow => {
    const task = live.find((t) => trimSlash(t.worktree) === trimSlash(s.cwd))
    const mine = unseenIds.has(s.id)
    const base = { key: `chat:${s.id}`, sessionId: s.id, agent: s.agent, root: s.cwd, project: folderName(s.cwd), hidden: settledIds.has(s.id) }
    if (task) return { ...base, kind: 'task', taskId: task.id, title: task.title, state: taskChatState(task.state, s.dormant ? null : chatState(s.chat, false), mine) }
    const state = chatState(s.chat, !!s.dormant)
    return { ...base, kind: 'chat', title: chatTitle(s.chat), state: state === 'finished' ? (mine ? 'finished-unseen' : 'idle') : state }
  })
  // A queued task has no chat yet but is an agent that will start; settled tasks without a chat are the Tasks panel's.
  for (const t of live) {
    if (t.state !== 'queued' || sessions.some((s) => trimSlash(s.cwd) === trimSlash(t.worktree))) continue
    rows.push({
      key: `task:${t.id}`, kind: 'task', taskId: t.id, agent: t.agent, title: t.title, root: t.worktree, project: folderName(t.worktree), state: 'queued',
      hidden: false, ...(t.blocked ? { blocked: t.blocked } : {}),
    })
  }
  for (const t of terminals) if (isTerminalAgent(t)) rows.push(terminalRow(t, unseenIds.has(terminalAgentKey(t.id))))
  return rows.map((row, i) => ({ row, i })).sort((a, b) => RANK[a.row.state] - RANK[b.row.state] || a.i - b.i).map((x) => x.row)
}

/** How many agents wait for the user: waiting, failed or finished unseen. */
export const attentionCount = (rows: readonly AgentRow[]): number => rows.filter((r) => needsUser(r.state)).length

/** The first agent in the panel's order that needs the user and has a tab to show (a queued task has none). */
export const firstNeedingAttention = (rows: readonly AgentRow[]): AgentRow | undefined =>
  rows.find((r) => (r.sessionId || r.terminalId) && needsUser(r.state))
