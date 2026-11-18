// Agent definitions (Go: AgentDefsAPI): Markdown files that define an agent. Go has no
// change event, so the list is read again whenever the UI refreshes its agent list
// (store/agentChat.ts load): when Settings ▸ Agents opens and after New Agent… creates one.
import { createStore } from '../lib/store'
import * as AgentDefs from '../../bindings/flint/agentdefsapi'
import type { AgentDefinitions, DefinedAgent } from '../../bindings/flint/models'
import { errorMessage } from '../api/backend'
import { showToast } from './dialog'
import { useEditor } from './editor'

export type { AgentDefinitions, DefinedAgent }

interface AgentDefsState {
  /** The last read, for Settings ▸ Agents (null before the first). */
  defs: AgentDefinitions | null
  /** The defined agents of the last read, for the pickers' labels. */
  defined: DefinedAgent[]
  /** The New Agent… dialog is open. */
  dialog: boolean
}

export const useAgentDefs = createStore<AgentDefsState>(() => ({ defs: null, defined: [], dialog: false }))

/** Reads the definitions from disk (Go keeps them for new chats); null after an error, which a picker can live without. */
export async function loadAgentDefs(): Promise<AgentDefinitions | null> {
  try {
    const defs = await AgentDefs.List()
    useAgentDefs.setState({ defs, defined: defs.agents ?? [] })
    return defs
  } catch (err) {
    showToast(`Couldn't read the agent definitions: ${errorMessage(err)}`, 'error')
    return null
  }
}

export const openAgentDefDialog = (): void => useAgentDefs.setState({ dialog: true })
export const closeAgentDefDialog = (): void => useAgentDefs.setState({ dialog: false })

/** New Agent…: writes a definition from a template and opens it. Returns the error to show, '' on success; the caller reloads the agent lists (useAgentChat.load). */
export async function createAgentDef(root: string, personal: boolean, name: string): Promise<string> {
  try {
    const path = await AgentDefs.Create(root, personal, name)
    useEditor.getState().openFile(path)
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/** Opens a definition's file in the editor. */
export const openAgentDefFile = (path: string): void => useEditor.getState().openFile(path)
