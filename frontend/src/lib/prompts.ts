// The pure parts of the prompt library (Go: internal/prompts): when the chat
// draft opens the "/" picker, which prompts match, and how they are labelled.

/** The fields of a prompt the picker needs (the Go binding's Prompt fits). */
export interface PromptInfo {
  name: string
  description: string
  /** Only offered in chats with this agent ('' = every agent). */
  agent: string
  args: string[]
  source: string
  /** A project prompt from a folder in Restricted Mode. */
  untrusted: boolean
}

/**
 * The picker's query when the draft is a single "/word" (possibly just "/"),
 * else null. Anything after a space or a newline closes the picker, so
 * "/compact focus on tests" goes to the agent as typed.
 */
export function slashQuery(draft: string): string | null {
  const m = /^\/(\S*)$/.exec(draft)
  return m ? m[1] : null
}

/** Whether a prompt is offered in a chat with agent ('' = any chat). */
export function forAgent(p: PromptInfo, agent: string): boolean {
  return !p.agent || !agent || p.agent === agent
}

/**
 * Prompts matching query, best first: the exact name, names starting with it,
 * names containing it, then descriptions containing it. Case-insensitive.
 */
export function matchPrompts<T extends PromptInfo>(list: readonly T[], query: string, agent: string): T[] {
  const q = query.toLowerCase()
  const rank = (p: T): number => {
    const name = p.name.toLowerCase()
    if (name === q) return 0
    if (name.startsWith(q)) return 1
    if (name.includes(q)) return 2
    if (p.description.toLowerCase().includes(q)) return 3
    return -1
  }
  return list
    .filter((p) => forAgent(p, agent))
    .map((p) => ({ p, r: rank(p) }))
    .filter((x) => x.r >= 0)
    .sort((a, b) => a.r - b.r || a.p.name.localeCompare(b.p.name))
    .map((x) => x.p)
}

/** Where a prompt comes from, as the picker shows it. */
export function sourceLabel(p: PromptInfo): string {
  if (p.untrusted) return 'untrusted repo'
  switch (p.source) {
    case 'project':
      return 'project'
    case 'personal':
      return 'personal'
    default:
      return 'built-in'
  }
}

/** The "prompts" part of settings.json (Go: internal/settings/prompts.go). */
export interface PromptSettings {
  /** Offer the prompts shipped with Flint (/review, /test, …). */
  builtins: boolean
}

export const DEFAULT_PROMPT_SETTINGS: PromptSettings = Object.freeze({ builtins: true })

/** Settings from untrusted JSON: a missing or invalid value gets the default. */
export function coercePromptSettings(v: unknown): PromptSettings {
  const builtins = typeof v === 'object' && v !== null && 'builtins' in v ? (v as { builtins: unknown }).builtins : undefined
  return { builtins: typeof builtins === 'boolean' ? builtins : DEFAULT_PROMPT_SETTINGS.builtins }
}

/** Same rule as Go's prompts.ValidName. */
export function validPromptName(name: string): boolean {
  return name.length <= 64 && /^[A-Za-z0-9][A-Za-z0-9_-]*$/.test(name)
}

/** The most recent file tab: the active one, else the last one visited. */
export function lastFileTab<T extends { id: string; kind: string }>(tabs: readonly T[], activeId: string | null, backStack: readonly string[]): T | null {
  for (const id of [activeId, ...[...backStack].reverse()]) {
    const tab = id ? tabs.find((t) => t.id === id) : undefined
    if (tab?.kind === 'file') return tab
  }
  return null
}
