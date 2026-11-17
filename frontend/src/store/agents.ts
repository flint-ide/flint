// The Agents panel's inputs that nothing else keeps: which chats finished while
// their tab was not the open one, which chat is peeked at, and what a click on a
// row does. Chat and task state stay where they are (store/agentChat.ts,
// store/agentTasks.ts); a settled tab is the editor's hidden tab (store/editor.ts).
import { createStore } from '../lib/store'
import { agentRows, firstNeedingAttention, needsUser, nextUnseen, terminalAgentKey, type AgentRow, type TerminalAgentInput } from '../lib/agentList'
import { settledToReveal } from '../lib/settledTabs'
import { chatTabId, useAgentChat } from './agentChat'
import { useTasks } from './agentTasks'
import { terminalTabId, useTerminal, type TerminalTab } from './terminal'
import { showToast } from './dialog'
import { useEditor } from './editor'
import { useWorkspace } from './workspace'

interface AgentsState {
  /** Chats whose last turn ended while their tab was not shown. */
  unseen: string[]
  /** The chat shown in the peek overlay (components/agents/AgentPeek.tsx), if any. */
  peek: string | null
}

export const useAgents = createStore<AgentsState>(() => ({ unseen: [], peek: null }))

/** The chat of a dock tab id ("chat:<id>"), or null for any other tab. */
const chatOfTab = (tab: string | null): string | null => (tab?.startsWith('chat:') ? tab.slice('chat:'.length) : null)

/** What the panel needs of a terminal tab. */
export const terminalInput = (t: TerminalTab): TerminalAgentInput => ({
  id: t.id, title: t.title, cwd: t.cwd, command: t.command, state: t.agentState, exited: t.exitCode !== null,
})

/** Chats whose tab is settled. */
export const settledChats = (hidden: readonly string[] = useEditor.getState().hidden): string[] =>
  hidden.flatMap((t) => chatOfTab(t) ?? [])

/** Every agent of the window in the panel's order. */
export function currentRows(): AgentRow[] {
  const { sessions, order } = useAgentChat.getState()
  return agentRows({
    sessions: order.flatMap((id) => (sessions[id] ? [sessions[id]] : [])),
    tasks: useTasks.getState().tasks,
    unseen: useAgents.getState().unseen,
    settled: settledChats(),
    terminals: useTerminal.getState().tabs.map(terminalInput),
  })
}

function refresh(ended: string[]): void {
  const { sessions } = useAgentChat.getState()
  const { unseen, peek } = useAgents.getState()
  const activeId = useEditor.getState().activeId
  // A terminal agent is kept under its tab id ("term:<id>"), a chat under its session id.
  const active = chatOfTab(activeId) ?? (activeId?.startsWith('term:') ? activeId : null)
  const live = [...Object.keys(sessions), ...useTerminal.getState().tabs.map((t) => terminalAgentKey(t.id))]
  // What is read in the peek is seen, as in its own tab.
  const next = nextUnseen(unseen, ended, active, live).filter((id) => id !== peek)
  useAgents.setState({ unseen: next, ...(peek && !sessions[peek] ? { peek: null } : {}) })
}

/** Draws settled tabs whose agent needs the user again; they do not take focus. */
function revealSettled(): void {
  const settled = settledChats()
  if (settled.length === 0) return
  for (const id of settledToReveal(settled, currentRows())) useEditor.getState().showTab(chatTabId(id))
}

let subscribed = false
/** Follows turn ends, task states and tab switches for the whole session, so the panel is right when it opens. */
export function subscribeAgents(): void {
  if (subscribed) return
  subscribed = true
  useAgentChat.subscribe((s, prev) => {
    if (s.sessions === prev.sessions) return
    const ended = Object.keys(s.sessions).filter((id) => prev.sessions[id]?.chat.busy && !s.sessions[id].chat.busy)
    refresh(ended)
    revealSettled()
  })
  useTerminal.subscribe((s, prev) => {
    if (s.tabs === prev.tabs) return
    // An agent CLI in a terminal finished its turn (the Stop hook) while its tab was not the open one.
    const ended = s.tabs.filter((t) => t.agentState === 'done' && prev.tabs.find((p) => p.id === t.id)?.agentState !== 'done').map((t) => terminalAgentKey(t.id))
    refresh(ended)
  })
  useEditor.subscribe((s, prev) => {
    if (s.activeId !== prev.activeId) refresh([])
  })
  useTasks.subscribe((s, prev) => {
    if (s.tasks !== prev.tasks) revealSettled()
  })
  useAgents.subscribe((s, prev) => {
    if (s.unseen !== prev.unseen) revealSettled()
  })
}

/** Shows an agent: its chat or terminal tab (a chat's is drawn again if it was settled), or the Tasks panel for a queued task that has no chat yet. */
export function focusAgent(row: AgentRow): void {
  if (row.sessionId) useEditor.getState().setActive(chatTabId(row.sessionId))
  else if (row.terminalId) useEditor.getState().setActive(terminalTabId(row.terminalId))
  else useWorkspace.getState().setSidebar('tasks')
}

/**
 * Hides a chat's tab until its agent needs the user. Refused (with a toast) when it
 * already does, since it would come straight back, and when it is the only tab drawn.
 */
export function settleChat(sessionId: string): void {
  const row = currentRows().find((r) => r.sessionId === sessionId)
  if (row && needsUser(row.state)) {
    showToast('This agent already needs you', 'info')
    return
  }
  if (!useEditor.getState().hideTab(chatTabId(sessionId))) showToast('There is no other tab to show instead', 'info')
}

/** Draws a settled chat's tab again without switching to it. */
export const unsettleChat = (sessionId: string): void => useEditor.getState().showTab(chatTabId(sessionId))

/** Shows a chat in the overlay over the current tab, which stays the active one. */
export function peekAgent(sessionId: string): void {
  if (!useAgentChat.getState().sessions[sessionId]) return
  // Its own tab is already in front: nothing to peek at.
  if (chatOfTab(useEditor.getState().activeId) === sessionId) return
  useAgents.setState({ peek: sessionId })
  refresh([])
}

export const closePeek = (): void => useAgents.setState({ peek: null })

/** Closes the peek and makes the chat's tab the active one. */
export function keepPeekAsTab(): void {
  const id = useAgents.getState().peek
  closePeek()
  if (id) useEditor.getState().setActive(chatTabId(id))
}

/** Peeks at the first agent in the panel's order that needs the user; false when none does. */
export function peekAttention(): boolean {
  // The chat in front is not "elsewhere": look past it.
  const here = chatOfTab(useEditor.getState().activeId)
  // Only a chat can be peeked at: a terminal agent has no chat to show over the tab.
  const row = firstNeedingAttention(currentRows().filter((r) => r.sessionId && r.sessionId !== here))
  if (!row?.sessionId) return false
  peekAgent(row.sessionId)
  return true
}
