// The agent and language-server parts of settings.json (Go:
// internal/settings/tools.go). Go validates every value; these helpers only
// shape what the Settings tab edits and read values with their defaults.

/** One agent CLI: '' path = looked up on the PATH, '' model = the CLI's default. */
export interface AgentSettings {
  path: string
  model: string
  /** Extra arguments, before Flint's own flags; ignored in Restricted Mode. */
  args: string[]
}

/** One language's server: '' command = the first installed known server. */
export interface LanguageServerSettings {
  disabled: boolean
  command: string
  /** Added after the server's own arguments; ignored in Restricted Mode. */
  args: string[]
}

export const EMPTY_AGENT: AgentSettings = Object.freeze({ path: '', model: '', args: Object.freeze([]) as unknown as string[] })
export const EMPTY_SERVER: LanguageServerSettings = Object.freeze({ disabled: false, command: '', args: Object.freeze([]) as unknown as string[] })

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
const str = (v: unknown): string => (typeof v === 'string' ? v : '')

export function coerceAgents(v: unknown): Record<string, AgentSettings> {
  const out: Record<string, AgentSettings> = {}
  if (!isObject(v)) return out
  for (const [name, a] of Object.entries(v)) {
    if (isObject(a)) out[name] = { path: str(a.path), model: str(a.model), args: strings(a.args) }
  }
  return out
}

export function coerceServers(v: unknown): Record<string, LanguageServerSettings> {
  const out: Record<string, LanguageServerSettings> = {}
  if (!isObject(v)) return out
  for (const [language, l] of Object.entries(v)) {
    if (isObject(l)) out[language] = { disabled: l.disabled === true, command: str(l.command), args: strings(l.args) }
  }
  return out
}

/** One argument per line (so arguments may contain spaces); blank lines are skipped. */
export function parseArgs(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
}

export const formatArgs = (args: readonly string[]): string => args.join('\n')

/** The agents map with name's entry changed (a new object; the input is untouched). */
export function withAgent(agents: Record<string, AgentSettings>, name: string, change: Partial<AgentSettings>): Record<string, AgentSettings> {
  return { ...agents, [name]: { ...EMPTY_AGENT, ...agents[name], ...change } }
}

/** The language-server map with language's entry changed. */
export function withServer(
  servers: Record<string, LanguageServerSettings>,
  language: string,
  change: Partial<LanguageServerSettings>,
): Record<string, LanguageServerSettings> {
  return { ...servers, [language]: { ...EMPTY_SERVER, ...servers[language], ...change } }
}

/** True when an entry is all defaults (shown as "Default" in Settings). */
export const isDefaultAgent = (a: AgentSettings | undefined) => !a || (a.path === '' && a.model === '' && a.args.length === 0)
export const isDefaultServer = (l: LanguageServerSettings | undefined) => !l || (!l.disabled && l.command === '' && l.args.length === 0)
