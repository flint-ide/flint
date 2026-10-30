import { createStore } from '../lib/store'
import * as Agent from '../../wailsjs/go/main/AgentAPI'
import { errorMessage } from '../api/backend'
import { showToast } from './dialog'
import { useTerminal } from './terminal'

interface AgentState {
  available: string[]
  preferred: string
  loaded: boolean
  load: () => Promise<void>
  setPreferred: (kind: string) => Promise<void>
}

export const useAgent = createStore<AgentState>((set) => ({
  available: [],
  preferred: '',
  loaded: false,

  load: async () => {
    try {
      const info = await Agent.Info()
      set({ available: info.available ?? [], preferred: info.preferred, loaded: true })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  setPreferred: async (kind) => {
    try {
      await Agent.SetPreferred(kind)
      set({ preferred: kind })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },
}))

/** Asks the agent for a commit message for the given repo-relative paths. */
export function suggestCommitMessage(root: string, tracked: string[], untracked: string[]): Promise<string> {
  return Agent.SuggestCommitMessage(root, tracked, untracked)
}

/**
 * Starts an interactive agent session in a new terminal tab to resolve conflicts.
 * The user watches it work and answers its questions right there.
 */
export async function resolveConflictsWithAgent(root: string, operation: string, files: string[]): Promise<void> {
  try {
    const command = await Agent.ResolveConflictsCommand(operation, files)
    const agent = useAgent.getState().preferred || 'agent'
    useTerminal.getState().create(root, { title: `${agent}: conflicts`, input: command })
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}
