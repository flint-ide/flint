// The Agents panel's inputs that nothing else keeps: which chats finished while
// their tab was not the open one, and what a click on a row does. Chat and task
// state stay where they are (store/agentChat.ts, store/agentTasks.ts).
import { createStore } from '../lib/store'
import { nextUnseen, type AgentRow } from '../lib/agentList'
import { chatTabId, useAgentChat } from './agentChat'
import { useEditor } from './editor'
import { useWorkspace } from './workspace'

interface AgentsState {
  /** Chats whose last turn ended while their tab was not shown. */
  unseen: string[]
}

export const useAgents = createStore<AgentsState>(() => ({ unseen: [] }))

/** The chat of a dock tab id ("chat:<id>"), or null for any other tab. */
const chatOfTab = (tab: string | null): string | null => (tab?.startsWith('chat:') ? tab.slice('chat:'.length) : null)

function refresh(ended: string[]): void {
  const { sessions } = useAgentChat.getState()
  const next = nextUnseen(useAgents.getState().unseen, ended, chatOfTab(useEditor.getState().activeId), Object.keys(sessions))
  useAgents.setState({ unseen: next })
}

let subscribed = false
/** Follows turn ends and tab switches for the whole session, so the panel is right when it opens. */
export function subscribeAgents(): void {
  if (subscribed) return
  subscribed = true
  useAgentChat.subscribe((s, prev) => {
    if (s.sessions === prev.sessions) return
    const ended = Object.keys(s.sessions).filter((id) => prev.sessions[id]?.chat.busy && !s.sessions[id].chat.busy)
    refresh(ended)
  })
  useEditor.subscribe((s, prev) => {
    if (s.activeId !== prev.activeId) refresh([])
  })
}

/** Shows an agent: its chat tab, or the Tasks panel for a queued task that has no chat yet. */
export function focusAgent(row: AgentRow): void {
  if (row.sessionId) useEditor.getState().setActive(chatTabId(row.sessionId))
  else useWorkspace.getState().setSidebar('tasks')
}
