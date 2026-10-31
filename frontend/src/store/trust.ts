import { createStore } from '../lib/store'
import * as Workspace from '../../bindings/flint/workspaceapi'
import { errorMessage } from '../api/backend'
import { showToast } from './dialog'

interface TrustState {
  /** Workspace folder → trusted; missing while unknown. */
  trusted: Readonly<Record<string, boolean>>
  /** Banners closed for this run (the folder stays restricted). */
  dismissed: ReadonlySet<string>
}

export const useTrust = createStore<TrustState>(() => ({ trusted: {}, dismissed: new Set() }))

/** Asks Go which of the open folders are trusted (Restricted Mode is enforced there). */
export async function refreshTrust(roots: readonly string[]): Promise<void> {
  const entries = await Promise.all(roots.map(async (r) => [r, await Workspace.IsTrusted(r).catch(() => false)] as const))
  useTrust.setState({ trusted: Object.fromEntries(entries) })
}

export async function trustFolder(root: string): Promise<void> {
  try {
    await Workspace.TrustFolder(root)
    useTrust.setState((s) => ({ trusted: { ...s.trusted, [root]: true } }))
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export async function untrustFolder(root: string): Promise<void> {
  try {
    await Workspace.UntrustFolder(root)
    useTrust.setState((s) => ({ trusted: { ...s.trusted, [root]: false } }))
    showToast('Restricted Mode is back on for this folder (running language servers stop on restart)')
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export function dismissTrustBanner(root: string): void {
  useTrust.setState((s) => ({ dismissed: new Set(s.dismissed).add(root) }))
}
