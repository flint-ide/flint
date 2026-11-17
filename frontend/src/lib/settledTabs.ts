// Settled chat tabs: hidden from the tab strip until their agent needs the user.
// Pure, so the reveal rule and the choice of the next tab are testable.
import { needsUser, type AgentRow } from './agentList'

/**
 * Which settled chats must be drawn again. Uses the Agents panel's states, not the
 * raw transcript: "finished" there means ended while the tab was not open, so a chat
 * settled after its last turn does not come straight back.
 */
export function settledToReveal(settled: readonly string[], rows: readonly AgentRow[]): string[] {
  const hidden = new Set(settled)
  return rows.flatMap((r) => (r.sessionId && hidden.has(r.sessionId) && needsUser(r.state) ? [r.sessionId] : []))
}

/** The tab to show instead of `id` when it leaves the strip: the next drawn one, else the previous. */
export function neighbourTab(ids: readonly string[], hidden: ReadonlySet<string>, id: string): string | null {
  const at = ids.indexOf(id)
  if (at < 0) return null
  const drawn = (x: string) => x !== id && !hidden.has(x)
  return ids.slice(at + 1).find(drawn) ?? ids.slice(0, at).findLast(drawn) ?? null
}
