// Tells Go which chats have an unfinished message in their input, so that
// another agent's message is refused instead of landing under the user's
// hands (AgentChatAPI.DraftChanged).

/** Which chats have a draft. A dormant chat is not known to Go yet, so it is left out. */
export function draftFlags(sessions: Record<string, { draft: string; dormant?: boolean }>): Record<string, boolean> {
  const flags: Record<string, boolean> = {}
  for (const [id, s] of Object.entries(sessions)) {
    if (!s.dormant) flags[id] = s.draft.trim() !== ''
  }
  return flags
}

export interface Timers {
  set: (fn: () => void, ms: number) => unknown
  clear: (handle: unknown) => void
}

const realTimers: Timers = { set: (fn, ms) => setTimeout(fn, ms), clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>) }

/**
 * Sends a chat's draft flag to `send` when it differs from what was last sent,
 * after it has stayed so for `delayMs` (typing flips it only once, and a
 * quick send-and-clear sends nothing). Chats missing from an update are
 * forgotten.
 */
export function createDraftReporter(
  send: (id: string, open: boolean) => void,
  delayMs = 300,
  timers: Timers = realTimers,
): { update: (flags: Record<string, boolean>) => void } {
  const reported = new Map<string, boolean>()
  const pending = new Map<string, { handle: unknown; open: boolean }>()

  const cancel = (id: string) => {
    const p = pending.get(id)
    if (p) timers.clear(p.handle)
    pending.delete(id)
  }

  return {
    update: (flags) => {
      for (const id of [...pending.keys(), ...reported.keys()]) {
        if (!(id in flags)) {
          cancel(id)
          reported.delete(id)
        }
      }
      for (const [id, open] of Object.entries(flags)) {
        if (pending.get(id)?.open === open) continue // already on its way: keep its timer running
        cancel(id)
        if (open === (reported.get(id) ?? false)) continue
        const handle = timers.set(() => {
          pending.delete(id)
          reported.set(id, open)
          send(id, open)
        }, delayMs)
        pending.set(id, { handle, open })
      }
    },
  }
}
