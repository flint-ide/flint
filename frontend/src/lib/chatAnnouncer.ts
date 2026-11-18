// Which chats are in a turn, and what each event of a chat means for the
// user (lib/attention.ts chatSignals). store/agentChat.ts feeds it every
// event as it arrives, before the transcript is updated once per frame, so a
// request that comes in the same batch as its turn_start is already in a
// turn. Pure apart from its set of chats; testable without the stores.
import type { AgentEvent } from './agentReducer'
import { chatSignals, turnAfter, type ChatInfo, type ChatSignal } from './attention'

/** The event kinds the attention model reads (turnAfter, chatSignals): the others (streamed tokens, tool updates) cost nothing here. */
export const ANNOUNCED_KINDS: ReadonlySet<string> = new Set(['turn_start', 'turn_end', 'permission_request', 'permission_settled'])

export interface ChatAnnouncer {
  /** An event of chat id. chat is what is known of it, null when the chat is not (or no longer) here. */
  event: (id: string, e: AgentEvent, chat: () => ChatInfo | null) => ChatSignal[]
  /** The chat closed. */
  forget: (id: string) => void
  /** The chats in a turn now (tests). */
  inTurn: () => string[]
}

export function createChatAnnouncer(): ChatAnnouncer {
  const turns = new Set<string>()
  return {
    event(id, e, chat) {
      if (!ANNOUNCED_KINDS.has(e.kind)) return []
      const info = chat()
      if (!info) {
        // Never adopted, or closed: nothing to tell, nothing to keep.
        turns.delete(id)
        return []
      }
      const inTurn = turns.has(id)
      if (turnAfter(inTurn, e)) turns.add(id)
      else turns.delete(id)
      return chatSignals(id, e, inTurn, info)
    },
    forget: (id) => void turns.delete(id),
    inTurn: () => [...turns],
  }
}
