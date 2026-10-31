import { createStore } from '../lib/store'
import * as Agent from '../../bindings/flint/agentapi'
import { errorMessage } from '../api/backend'
import { showToast } from './dialog'
import { useTerminal } from './terminal'
import { flushSettings, useSettings } from './settings'

interface AgentState {
  available: string[]
  /** The agent in use: the default from settings, else the first installed one. */
  preferred: string
  /**
   * The agent that resolves conflicts in a terminal: the preferred one when it is
   * Claude Code or Codex, else the first of those installed ('' = none). Custom ACP
   * agents have no interactive terminal mode.
   */
  conflictAgent: string
  loaded: boolean
  load: () => Promise<void>
  /** '' = the first installed agent. */
  setPreferred: (kind: string) => void
}

export const useAgent = createStore<AgentState>((set) => ({
  available: [],
  preferred: '',
  conflictAgent: '',
  loaded: false,

  load: async () => {
    try {
      const info = await Agent.Info()
      set({ available: info.available ?? [], preferred: info.preferred, conflictAgent: info.conflictAgent, loaded: true })
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  },

  // The default agent is a setting (settings.json); the commit panel's picker and
  // Settings ▸ Agents both change it here.
  setPreferred: (kind) => {
    useSettings.getState().update({ defaultAgent: kind })
    set((s) => ({ preferred: kind || s.available[0] || '' }))
    // Which agent resolves conflicts follows the default; Go decides.
    void flushSettings().then(() => useAgent.getState().load())
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
    const command = await Agent.ResolveConflictsCommand(root, operation, files)
    const agent = useAgent.getState().conflictAgent || 'agent'
    useTerminal.getState().create(root, { title: `${agent}: conflicts`, input: command })
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}
