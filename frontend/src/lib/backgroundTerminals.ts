// Background terminals (store/backgroundTerminals.ts): which saved tabs attach
// to a shell the background host kept running, and which of the host's shells
// no tab shows. Framework-free, so it is testable with node:test.

/** A terminal the host holds (Go: LiveTerminal). */
export interface LiveTerminal {
  id: string
  running: boolean
}

/**
 * How a saved terminal tab comes back: 'attach' to the shell the host still
 * runs under its id (no new shell, no saved text, no resume command: the
 * program is still there), or 'respawn' as before background terminals (a new
 * shell under a new id, the saved text dimmed above, the agent CLI typed
 * again). A shell another tab already claimed is never attached twice.
 */
export function restoreMode(savedId: string | undefined, live: ReadonlySet<string>, claimed: ReadonlySet<string>): 'attach' | 'respawn' {
  return savedId && live.has(savedId) && !claimed.has(savedId) ? 'attach' : 'respawn'
}

/** The host's terminals no open tab shows, in the host's order. */
export function unclaimedTerminals(live: readonly LiveTerminal[], open: Iterable<string>): LiveTerminal[] {
  const shown = new Set(open)
  return live.filter((t) => !shown.has(t.id))
}

/** One line for a background terminal in the list: a short id and its state. */
export function backgroundTerminalLabel(t: LiveTerminal, index: number): string {
  return `Terminal ${index + 1} (${t.id.slice(0, 8)}) — ${t.running ? 'running' : 'ended'}`
}

/** The toast after a restore that left some of the host's terminals without a tab; '' for none. */
export function unclaimedNotice(count: number): string {
  if (count === 0) return ''
  const what = count === 1 ? 'A background terminal is' : `${count} background terminals are`
  return `${what} not shown in any tab: open them with Show Background Terminals in the command palette.`
}
