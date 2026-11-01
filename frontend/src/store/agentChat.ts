import { createStore } from '../lib/store'
import * as Chat from '../../bindings/flint/agentchatapi'
import { EventsOn } from '../wails/runtime'
import { agentsession } from '../wails/models'
import { errorMessage } from '../api/backend'
import {
  addUserMessage,
  answerPermission,
  applyEvent,
  applyEvents,
  emptyChat,
  settleItems,
  type AgentEvent,
  type Attachment,
  type ChatState,
} from '../lib/agentReducer'
import { showToast } from './dialog'
import { createReorderer, type Reorderer } from '../lib/reorder'
import type { SavedChat } from '../lib/sessionSnapshot'
import { useEditor } from './editor'
import { alertDone } from './alerts'

export interface ChatSession {
  id: string
  agent: string
  cwd: string
  model?: string
  chat: ChatState
  draft: string
  attachments: Attachment[]
  /** Restored after a restart: the agent is reconnected on the next message. */
  dormant?: boolean
}

interface AgentChatState {
  agents: string[]
  sessions: Record<string, ChatSession>
  order: string[]
  starting: string | null
  policy: agentsession.Policy | null

  load: () => Promise<void>
  start: (agent: string, cwd: string) => Promise<string | null>
  send: (id: string) => Promise<void>
  cancel: (id: string) => void
  respond: (id: string, requestId: string, optionId: string, optionName: string) => Promise<void>
  stop: (id: string) => Promise<void>
  setDraft: (id: string, draft: string) => void
  attach: (id: string, a: Attachment) => void
  detach: (id: string, index: number) => void
  setPolicy: (p: agentsession.Policy) => Promise<void>
  /** Re-creates a chat from a snapshot without starting its agent. */
  restore: (saved: SavedChat) => void
}

/** Dock tab id of a chat session (terminals and chats share the right dock). */
export const chatTabId = (sessionId: string) => `chat:${sessionId}`

// A resumed conversation keeps its id, but if the agent hands back a new one (or a
// fresh session replaces one that could not be resumed) the chat keeps its local id:
// calls go to remoteId(local), events are routed back through localId(remote).
const remoteIds = new Map<string, string>()
const localIds = new Map<string, string>()
const remoteId = (id: string) => remoteIds.get(id) ?? id
const localId = (id: string) => localIds.get(id) ?? id
/** The agent's session id of a chat, for Go calls about it (store/usage.ts). */
export const chatRemoteId = remoteId
/** The chat a Go event about an agent session belongs to. */
export const chatLocalId = localId

function link(local: string, remote: string): void {
  if (local === remote) return
  remoteIds.set(local, remote)
  localIds.set(remote, local)
}

export const useAgentChat = createStore<AgentChatState>((set, get) => ({
  agents: [],
  sessions: {},
  order: [],
  starting: null,
  policy: null,

  load: async () => {
    try {
      const [agents, policy] = await Promise.all([Chat.Agents(), Chat.Policy()])
      set({ agents: agents ?? [], policy })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  start: async (agent, cwd) => {
    set({ starting: agent })
    try {
      const info = await Chat.Start(agent, cwd)
      const session: ChatSession = {
        id: info.id, agent: info.agent || agent, cwd: info.cwd, model: info.model, chat: emptyChat, draft: '', attachments: [],
      }
      set((s) => ({ sessions: { ...s.sessions, [info.id]: session }, order: [...s.order, info.id] }))
      useEditor.getState().openChat(info.id, session.agent)
      return info.id
    } catch (err) {
      showToast(`Could not start ${agent}: ${errorMessage(err)}`, 'error')
      return null
    } finally {
      set({ starting: null })
    }
  },

  send: async (id) => {
    const s = get().sessions[id]
    const text = s?.draft.trim()
    if (!s || !text || s.chat.busy) return
    const attachments = s.attachments
    update(id, (cs) => ({ ...cs, draft: '', attachments: [], chat: addUserMessage(cs.chat, text, attachments) }))
    try {
      if (s.dormant) await wake(s)
      await Chat.Prompt(remoteId(id), text, attachments.map((a) => agentsession.Attachment.createFrom(a)))
    } catch (err) {
      update(id, (cs) => ({
        ...cs,
        chat: applyEvent(applyEvent(cs.chat, { sessionId: id, kind: 'error', text: errorMessage(err) }), {
          sessionId: id, kind: 'turn_end', stopReason: 'error',
        }),
      }))
    }
  },

  cancel: (id) => void Chat.Cancel(remoteId(id)).catch((err) => showToast(errorMessage(err), 'error')),

  respond: async (id, requestId, optionId, optionName) => {
    update(id, (cs) => ({ ...cs, chat: answerPermission(cs.chat, requestId, optionName) }))
    try {
      await Chat.Respond(remoteId(id), requestId, optionId)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  stop: async (id) => {
    useEditor.getState().closeTab(chatTabId(id))
    if (!get().sessions[id]?.dormant) await Chat.Stop(remoteId(id)).catch(() => undefined)
    deliverAgent?.forget(remoteId(id))
    localIds.delete(remoteId(id))
    remoteIds.delete(id)
    set((s) => {
      const { [id]: _gone, ...sessions } = s.sessions
      return { sessions, order: s.order.filter((x) => x !== id) }
    })
  },

  setDraft: (id, draft) => update(id, (cs) => ({ ...cs, draft })),

  attach: (id, a) =>
    update(id, (cs) =>
      cs.attachments.some((x) => x.path === a.path && x.startLine === a.startLine && x.endLine === a.endLine)
        ? cs
        : { ...cs, attachments: [...cs.attachments, a] },
    ),

  detach: (id, index) => update(id, (cs) => ({ ...cs, attachments: cs.attachments.filter((_, i) => i !== index) })),

  restore: (saved) => {
    if (get().sessions[saved.id]) return
    const session: ChatSession = { ...saved, chat: settle(saved.chat), attachments: [], dormant: true }
    set((s) => ({ sessions: { ...s.sessions, [saved.id]: session }, order: [...s.order, saved.id] }))
    useEditor.getState().openChat(saved.id, saved.agent, true)
  },

  setPolicy: async (p) => {
    set({ policy: p })
    try {
      await Chat.SetPolicy(p)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },
}))

/**
 * Reconnects a restored chat: resumes the agent's conversation, or starts a new
 * one (and says so) when the agent cannot find it any more.
 */
async function wake(s: ChatSession): Promise<void> {
  let info: agentsession.SessionInfo
  try {
    info = await Chat.Resume(s.agent, s.cwd, s.id)
  } catch (err) {
    info = await Chat.Start(s.agent, s.cwd)
    update(s.id, (cs) => ({
      ...cs,
      chat: applyEvent(cs.chat, {
        sessionId: s.id,
        kind: 'error',
        text: `Could not resume the earlier conversation (${errorMessage(err)}); continuing in a new one that does not see the messages above.`,
      }),
    }))
  }
  link(s.id, info.id)
  update(s.id, (cs) => ({ ...cs, dormant: false, model: info.model || cs.model }))
}

/** A restored transcript is idle: nothing runs and unanswered questions expired. */
function settle(chat: ChatState): ChatState {
  return { ...chat, busy: false, items: settleItems(chat.items) }
}

function update(id: string, fn: (s: ChatSession) => ChatSession): void {
  // Local changes (the user's message, an answer, an error) go after the events
  // received before them.
  flushEvents()
  useAgentChat.setState((st) => {
    const cur = st.sessions[id]
    if (!cur) return {}
    const next = fn(cur)
    return next === cur ? {} : { sessions: { ...st.sessions, [id]: next } }
  })
}

// Agents stream a few events per token. Applying each in its own setState re-renders
// the chat and copies its transcript per token, so events wait in a per-session queue
// (in arrival order) and are applied together once per frame. The timer covers a
// hidden window, where animation frames stop.
const FLUSH_FALLBACK_MS = 100
const queued = new Map<string, AgentEvent[]>()
let pendingFlush: { frame: number; timer: ReturnType<typeof setTimeout> } | null = null

function enqueue(id: string, e: AgentEvent): void {
  if (e.kind === 'turn_end' && e.stopReason !== 'cancelled') {
    const tab = `chat:${id}`
    const title = useEditor.getState().tabs.find((t) => t.id === tab)?.title ?? 'Agent'
    alertDone(title, e.stopReason === 'error' ? 'The agent stopped with an error' : 'The agent finished its turn', tab)
  }
  const q = queued.get(id)
  if (q) q.push(e)
  else queued.set(id, [e])
  pendingFlush ??= { frame: requestAnimationFrame(flushEvents), timer: setTimeout(flushEvents, FLUSH_FALLBACK_MS) }
}

/** Applies every queued event in one store update. */
function flushEvents(): void {
  if (pendingFlush) {
    cancelAnimationFrame(pendingFlush.frame)
    clearTimeout(pendingFlush.timer)
    pendingFlush = null
  }
  if (queued.size === 0) return
  const batch = [...queued]
  queued.clear()
  useAgentChat.setState((st) => {
    let sessions = st.sessions
    for (const [id, events] of batch) {
      const cur = sessions[id]
      if (!cur) continue // closed meanwhile
      const chat = applyEvents(cur.chat, events)
      if (chat !== cur.chat) sessions = { ...sessions, [id]: { ...cur, chat } }
    }
    return sessions === st.sessions ? {} : { sessions }
  })
}

/** Set once subscribed; stop() releases a closed chat's ordering state. */
let deliverAgent: Reorderer<AgentEvent> | null = null

let subscribed = false
export function subscribeAgentChatEvents(): void {
  if (subscribed) return
  subscribed = true
  const deliver = createReorderer<AgentEvent>((id, e) => enqueue(localId(id), e))
  deliverAgent = deliver
  EventsOn('agent:event', (e: AgentEvent) => deliver(e.sessionId, e))
  void useAgentChat.getState().load()
}
