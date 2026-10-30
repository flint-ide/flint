import { useEditor, viewFor } from '../store/editor'
import { useWorkspace } from '../store/workspace'
import { chatTabId, useAgentChat } from '../store/agentChat'
import { useAgent } from '../store/agent'
import { showToast } from '../store/dialog'

/**
 * ⌘L: attaches the current editor selection (or the whole file when nothing is
 * selected) to the active agent chat, starting one with the preferred agent if needed.
 */
export async function attachSelectionToAgent(): Promise<void> {
  const { tabs, activeId } = useEditor.getState()
  const tab = tabs.find((t) => t.id === activeId)
  if (!tab || tab.kind !== 'file') {
    showToast('Open a file to attach it to the agent')
    return
  }
  const sel = currentSelection(tab.id)

  const chat = useAgentChat.getState()
  // The chat used most recently (⌘L from a file returns you to it).
  const editor = useEditor.getState()
  const recentChat = [...editor.backStack].reverse().find((t) => t.startsWith('chat:'))
  let id = recentChat?.slice(5) ?? chat.order[chat.order.length - 1]
  if (!id || !chat.sessions[id]) {
    const agent = useAgent.getState().preferred || chat.agents[0]
    if (!agent) {
      showToast('No agent installed (claude or codex)', 'error')
      return
    }
    id = (await chat.start(agent, useWorkspace.getState().root ?? '')) ?? ''
    if (!id) return
  }
  useAgentChat.getState().attach(id, sel ? { path: tab.path, ...sel } : { path: tab.path })
  useEditor.getState().setActive(chatTabId(id))
}

/** The main selection of the active editor tab, with 1-based line numbers. */
function currentSelection(tabId: string): { text: string; startLine: number; endLine: number } | null {
  const view = viewFor(tabId)
  if (!view) return null
  const { from, to } = view.state.selection.main
  if (from === to) return null
  const doc = view.state.doc
  return { text: doc.sliceString(from, to), startLine: doc.lineAt(from).number, endLine: doc.lineAt(to).number }
}
