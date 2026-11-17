// Claude Code running in a terminal tab, reporting its state to Flint (Go:
// api_terminal_hooks.go, internal/agenthooks). Flint never edits the user's own
// Claude settings: "New Claude Terminal" starts `claude` with a settings file of
// Flint's own, and the settings page offers the same hooks to copy.
import { TerminalBackend, useTerminal } from './terminal'
import { activeRoot } from './activeRoot'
import { errorMessage } from '../api/backend'
import { showToast } from './dialog'

/**
 * Opens a terminal in the active folder and types `claude --settings '<hooks file>'`.
 * Go writes the file only for a trusted folder; otherwise it says why and no
 * terminal is opened.
 */
export async function newClaudeTerminal(): Promise<void> {
  const root = activeRoot()
  try {
    const line = await TerminalBackend.ClaudeCommand(root)
    useTerminal.getState().create(root, { input: line })
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Puts the hooks for the user's own Claude Code settings on the clipboard. */
export async function copyHookSnippet(): Promise<void> {
  try {
    await navigator.clipboard.writeText(await TerminalBackend.HookSnippet())
    showToast('Copied the hooks: merge them into the "hooks" of your Claude Code settings')
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}
