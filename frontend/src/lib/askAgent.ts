import { useEditor } from '../store/editor'
import { activeRoot, rootFor } from '../store/activeRoot'
import { chatTabId, useAgentChat } from '../store/agentChat'
import { useAgent } from '../store/agent'
import { showToast } from '../store/dialog'
import type { Attachment } from './agentReducer'
import { appendToDraft, pickChat } from './agentPrompt'

export interface AskAgentOptions {
  /** Message text, added after whatever the user already typed in the draft. */
  prompt: string
  /** Files or line ranges sent along (duplicates of ones already attached are skipped). */
  attachments?: Attachment[]
  /**
   * Send right away (⌥-click) instead of leaving it in the draft for the user to
   * add words. A busy agent keeps it in the draft.
   */
  send?: boolean
  /**
   * Workspace folder the request is about; its chats are preferred. Defaults to the
   * folder of the first attachment, else the active tab's folder.
   */
  root?: string
}

/**
 * Hands a problem to the agent chat: the active (or most recently used) chat of the
 * project it is about, or a new one there with the preferred agent. Fills the draft and
 * attachments, then focuses the chat. Returns the chat session id, or null when
 * no chat could be opened.
 */
export async function askAgent({ prompt, attachments = [], send = false, root }: AskAgentOptions): Promise<string | null> {
  const id = await targetChat(root ?? (attachments.length ? rootFor(attachments[0].path) : activeRoot()))
  if (!id) return null
  const store = useAgentChat.getState()
  const session = store.sessions[id]
  if (!session) return null
  store.setDraft(id, appendToDraft(session.draft, prompt))
  attachments.forEach((a) => store.attach(id, a))
  useEditor.getState().setActive(chatTabId(id))
  if (!send) return id
  if (session.chat.busy) showToast(`${session.agent} is still working — your message waits in the draft`)
  else await useAgentChat.getState().send(id)
  return id
}

/** The chat askAgent writes to, started with the preferred agent when none fits. */
async function targetChat(root: string): Promise<string | null> {
  const chat = useAgentChat.getState()
  const { activeId, backStack } = useEditor.getState()
  const cwds = Object.fromEntries(Object.values(chat.sessions).map((s) => [s.id, s.cwd]))
  const found = pickChat({ activeId, backStack, order: chat.order, cwds, root })
  if (found) return found
  const agent = useAgent.getState().preferred || chat.agents[0]
  if (!agent) {
    showToast('No agent installed (claude or codex)', 'error')
    return null
  }
  return chat.start(agent, root)
}
