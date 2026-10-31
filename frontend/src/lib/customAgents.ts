// Custom agents: CLIs Flint talks to over the Agent Client Protocol
// ("customAgents" in settings.json; Go: internal/settings/customagents.go).
// Go validates every value; these helpers shape what Settings edits.

export interface CustomAgent {
  /** Lowercase id used by the agent pickers and "defaultAgent". */
  id: string
  /** Shown in Settings; '' shows the id. */
  name: string
  /** A command name on the login PATH or an absolute path. */
  command: string
  /** Arguments that start the CLI in ACP mode. */
  args: string[]
}

/** A one-click fill-in for a CLI whose ACP mode is known. */
export interface CustomAgentPreset extends CustomAgent {
  /** Where to get the CLI and how to log in. */
  hint: string
}

/**
 * Agents whose ACP flag was checked against the CLI itself. Each logs in with
 * its own command, so no credentials end up in settings.json.
 */
export const PRESETS: readonly CustomAgentPreset[] = Object.freeze([
  {
    id: 'gemini',
    name: 'Gemini CLI',
    command: 'gemini',
    args: ['--experimental-acp'],
    hint: 'npm install -g @google/gemini-cli, then run gemini once in a terminal to log in.',
  },
  {
    id: 'opencode',
    name: 'OpenCode',
    command: 'opencode',
    args: ['acp'],
    hint: 'Install from opencode.ai, then run opencode auth login in a terminal.',
  },
])

/** Same limits as Go's settings (MaxCustomAgents, customAgentIDPattern). */
export const MAX_CUSTOM_AGENTS = 16
const ID_PATTERN = /^[a-z][a-z0-9-]{0,31}$/
const BUILT_IN = ['claude', 'codex']

export const EMPTY_CUSTOM_AGENT: CustomAgent = Object.freeze({ id: '', name: '', command: '', args: Object.freeze([]) as unknown as string[] })

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const str = (v: unknown): string => (typeof v === 'string' ? v : '')

/** Custom agents from untrusted JSON: entries without an id or command are dropped. */
export function coerceCustomAgents(v: unknown): CustomAgent[] {
  if (!Array.isArray(v)) return []
  return v.filter(isObject).flatMap((c): CustomAgent[] => {
    const agent = {
      id: str(c.id),
      name: str(c.name),
      command: str(c.command),
      args: Array.isArray(c.args) ? c.args.filter((a): a is string => typeof a === 'string') : [],
    }
    return agent.id && agent.command ? [agent] : []
  })
}

/** The name to show for a custom agent. */
export const displayName = (c: CustomAgent): string => c.name || c.id

/** Whether id is free for a new agent (well-formed, not built in, not taken). */
export function idAvailable(id: string, list: readonly CustomAgent[], except = -1): boolean {
  return ID_PATTERN.test(id) && !BUILT_IN.includes(id) && !list.some((c, i) => i !== except && c.id === id)
}

/**
 * An id derived from text (a name or a command path): lowercase letters,
 * digits and dashes, made unique with a number when taken.
 */
export function suggestId(text: string, list: readonly CustomAgent[]): string {
  const base =
    (text.split('/').pop() ?? '')
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^[^a-z]+|-+$/g, '')
      .slice(0, 28) || 'agent'
  if (idAvailable(base, list)) return base
  for (let n = 2; ; n++) {
    if (idAvailable(`${base}-${n}`, list)) return `${base}-${n}`
  }
}

/** The list with the entry at index changed (a new array; the input is untouched). */
export function withCustomAgent(list: readonly CustomAgent[], index: number, change: Partial<CustomAgent>): CustomAgent[] {
  return list.map((c, i) => (i === index ? { ...c, ...change } : c))
}

/** The list without the entry at index. */
export const withoutCustomAgent = (list: readonly CustomAgent[], index: number): CustomAgent[] => list.filter((_, i) => i !== index)

/**
 * The default agent after a custom agent's id changed (from → to) or it was
 * removed (to = ''): a default that pointed at it follows, else stays.
 */
export const followDefault = (defaultAgent: string, from: string, to: string): string => (defaultAgent === from ? to : defaultAgent)

/**
 * Tooltip of "Resolve with …". Conflicts are resolved in a terminal, which a
 * custom ACP agent can't drive, so a native agent may stand in for the default.
 */
export function conflictHint(preferred: string, conflictAgent: string): string {
  const base = `Start ${conflictAgent} in a new terminal to resolve these conflicts`
  return preferred && preferred !== conflictAgent ? `${base} (${preferred} has no terminal mode)` : base
}

/** Presets not added yet (by id or command). */
export const unusedPresets = (list: readonly CustomAgent[]): CustomAgentPreset[] =>
  PRESETS.filter((p) => !list.some((c) => c.id === p.id || c.command === p.command))
