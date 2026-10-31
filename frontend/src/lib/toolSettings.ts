// The agent, language-server and refresh parts of settings.json (Go:
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

/** How often polling panels re-read their data, in seconds. */
export interface RefreshSettings {
  kubernetesSeconds: number
  awsSeconds: number
}

export type RefreshKey = keyof RefreshSettings

export const EMPTY_AGENT: AgentSettings = Object.freeze({ path: '', model: '', args: Object.freeze([]) as unknown as string[] })
export const EMPTY_SERVER: LanguageServerSettings = Object.freeze({ disabled: false, command: '', args: Object.freeze([]) as unknown as string[] })

/** Mirrors Go's defaultRefresh. */
export const DEFAULT_REFRESH: RefreshSettings = Object.freeze({ kubernetesSeconds: 5, awsSeconds: 30 })

/** Same bounds as Go's settings (Min/Max*Seconds). */
export const REFRESH_LIMITS: Record<RefreshKey, { min: number; max: number }> = {
  kubernetesSeconds: { min: 2, max: 600 },
  awsSeconds: { min: 5, max: 3600 },
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
const str = (v: unknown): string => (typeof v === 'string' ? v : '')

/** Clamps n into key's range; anything not a number is the default. */
export function clampRefresh(key: RefreshKey, n: unknown): number {
  if (typeof n !== 'number' || !Number.isFinite(n) || n === 0) return DEFAULT_REFRESH[key]
  const { min, max } = REFRESH_LIMITS[key]
  return Math.min(max, Math.max(min, Math.round(n)))
}

/** The polling interval of key in milliseconds. */
export function refreshMs(refresh: RefreshSettings, key: RefreshKey): number {
  return clampRefresh(key, refresh[key]) * 1000
}

export function coerceRefresh(v: unknown): RefreshSettings {
  const r = isObject(v) ? v : {}
  return { kubernetesSeconds: clampRefresh('kubernetesSeconds', r.kubernetesSeconds), awsSeconds: clampRefresh('awsSeconds', r.awsSeconds) }
}

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
