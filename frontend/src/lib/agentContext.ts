import { useEditor, viewFor } from '../store/editor'
import { terminalSelection, useTerminal } from '../store/terminal'
import { showToast } from '../store/dialog'
import { askAgent } from './askAgent'
import { terminalOutputPrompt } from './agentPrompt'

/**
 * ⌘L: attaches the current editor selection (or the whole file when nothing is
 * selected) to the active agent chat, starting one with the preferred agent if needed.
 * In a terminal it sends the selected output instead.
 */
export async function attachSelectionToAgent(): Promise<void> {
  const { tabs, activeId } = useEditor.getState()
  const tab = tabs.find((t) => t.id === activeId)
  if (tab?.kind === 'terminal') {
    await sendTerminalSelectionToAgent(tab.termId)
    return
  }
  if (!tab || tab.kind !== 'file') {
    showToast('Open a file to attach it to the agent')
    return
  }
  const sel = currentSelection(tab.id)
  await askAgent({ prompt: '', attachments: [sel ? { path: tab.path, ...sel } : { path: tab.path }] })
}

/** Attaches files or folders (file tree ▸ Add to Agent Chat); the agent reads them itself. */
export async function attachPathsToAgent(paths: string[]): Promise<void> {
  await askAgent({ prompt: '', attachments: paths.map((path) => ({ path })) })
}

/** Sends the text selected in a terminal to the agent chat as a fenced block. */
export async function sendTerminalSelectionToAgent(termId: string, send = false): Promise<void> {
  const text = terminalSelection(termId)
  if (!text.trim()) {
    showToast('Select terminal output to send it to the agent')
    return
  }
  const title = useTerminal.getState().tabs.find((t) => t.id === termId)?.title
  await askAgent({ prompt: terminalOutputPrompt(text, title), send })
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
