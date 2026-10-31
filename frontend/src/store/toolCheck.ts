import * as Diagnostics from '../../bindings/flint/diagnosticsapi'
import { createStore } from '../lib/store'
import type { ToolStatus } from '../lib/toolCheck'
import { errorMessage } from '../api/backend'

// The tool check: which external tools Flint drives are installed. Opens by
// itself on the first launch (the flag lives in Go's state.json), and later
// from the command palette or the Welcome screen.

interface ToolCheckState {
  open: boolean
  /** null until the first check finished. */
  tools: ToolStatus[] | null
  checking: boolean
  error: string
}

export const useToolCheck = createStore<ToolCheckState>(() => ({ open: false, tools: null, checking: false, error: '' }))

/** Runs the check again (Go runs only version commands, in parallel, with timeouts). */
export async function recheckTools(): Promise<void> {
  if (useToolCheck.getState().checking) return
  useToolCheck.setState({ checking: true, error: '' })
  try {
    useToolCheck.setState({ tools: (await Diagnostics.Tools()) ?? [] })
  } catch (err) {
    useToolCheck.setState({ error: errorMessage(err) })
  } finally {
    useToolCheck.setState({ checking: false })
  }
}

export function openToolCheck(): void {
  useToolCheck.setState({ open: true })
  void recheckTools()
}

export function closeToolCheck(): void {
  useToolCheck.setState({ open: false })
}

/** On the first launch only, so new users see what is missing before panels come up empty. */
export async function openToolCheckOnFirstRun(): Promise<void> {
  try {
    if (await Diagnostics.ToolCheckOnFirstRun()) openToolCheck()
  } catch {
    // not worth an error toast on startup: the check is in the command palette
  }
}
