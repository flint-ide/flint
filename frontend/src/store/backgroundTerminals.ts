import * as SessionAPI from '../../bindings/flint/sessionapi'
import { createStore } from '../lib/store'
import { unclaimedNotice, unclaimedTerminals, type LiveTerminal } from '../lib/backgroundTerminals'
import { showToast } from './dialog'
import { TerminalBackend, useTerminal } from './terminal'
import { activeRoot } from './activeRoot'
import { errorMessage } from '../api/backend'

// Background terminals (Settings ▸ Terminal): shells the background host keeps
// running while no window is open. Restored tabs attach to theirs (store/session.ts);
// the ones no tab claims are listed by Show Background Terminals, where the user
// attaches or ends them. Nothing here opens a tab or ends a shell on its own.

interface BackgroundTerminalsState {
  /** Terminals run in the background host this run (the setting was on and the host answered). */
  background: boolean
  /** What the host holds, as last asked. */
  live: LiveTerminal[]
  /** The list dialog is open. */
  open: boolean
}

export const useBackgroundTerminals = createStore<BackgroundTerminalsState>(() => ({ background: false, live: [], open: false }))

/** Asks Go what the host holds; shows, once, why background terminals are off although switched on. */
async function fetchLive(): Promise<LiveTerminal[]> {
  try {
    const res = await SessionAPI.Live()
    if (res.notice) showToast(res.notice, 'error')
    const live = (res.terminals ?? []).map((t) => ({ id: t.id, running: t.running }))
    useBackgroundTerminals.setState({ background: res.background, live })
    return live
  } catch {
    return [] // terminals then restore as new shells, as without the host
  }
}

/** Ids the host holds now, for restoring tabs. */
export async function liveTerminalIds(): Promise<Set<string>> {
  return new Set((await fetchLive()).map((t) => t.id))
}

const openIds = () => useTerminal.getState().tabs.map((t) => t.id)

/** The host's terminals no tab shows. */
export function unclaimedNow(): LiveTerminal[] {
  return unclaimedTerminals(useBackgroundTerminals.getState().live, openIds())
}

let noted = false
/** After a restore: says once per run that the host runs terminals no tab shows. */
export async function noteUnclaimedTerminals(): Promise<void> {
  await fetchLive()
  const notice = unclaimedNotice(unclaimedNow().length)
  if (noted || !notice) return
  noted = true
  showToast(notice)
}

/** Learns whether background terminals are in use, for the command's availability. */
export function refreshBackgroundTerminals(): Promise<unknown> {
  return fetchLive()
}

export function openBackgroundTerminals(): void {
  useBackgroundTerminals.setState({ open: true })
  void fetchLive()
}

export function closeBackgroundTerminals(): void {
  useBackgroundTerminals.setState({ open: false })
}

/** Opens a tab showing the host's terminal id. */
export function attachBackgroundTerminal(id: string): void {
  useTerminal.getState().create(activeRoot(), { attachId: id })
}

/** Ends the host's terminal id (its shell is hung up, like closing its tab). */
export async function endBackgroundTerminal(id: string): Promise<void> {
  try {
    await TerminalBackend.Close(id)
  } catch (err) {
    showToast(`Could not end the terminal: ${errorMessage(err)}`, 'error')
  }
  await fetchLive()
}
