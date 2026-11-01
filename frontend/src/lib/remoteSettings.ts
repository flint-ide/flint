// The "remote" part of settings.json (Go: internal/settings/remote.go): Flint
// Remote, the API a paired phone uses through the user's own tunnel. Go
// validates it; this only shapes it for the Settings tab and fills in defaults.

export interface RemoteSettings {
  enabled: boolean
  /** Port on 127.0.0.1, fixed so a tunnel can target it. */
  port: number
  /** Extra host names requests may be addressed to (e.g. the tailscale serve name). */
  allowedHosts: string[]
  /** Events kept per chat for a phone that connects mid-conversation. */
  replayEvents: number
  replayKB: number
  /** Each paired device's request budget. */
  requestsPerMinute: number
}

/** Same bounds as Go's settings/remote.go. */
export const REMOTE_LIMITS = {
  port: { min: 1024, max: 65535 },
  replayEvents: { min: 100, max: 20000 },
  replayKB: { min: 256, max: 65536 },
  requestsPerMinute: { min: 10, max: 6000 },
  hosts: 16,
}

/** Mirrors Go's defaultRemote. */
export const DEFAULT_REMOTE: RemoteSettings = Object.freeze({
  enabled: false,
  port: 7419,
  allowedHosts: Object.freeze([]) as unknown as string[],
  replayEvents: 2000,
  replayKB: 4096,
  requestsPerMinute: 120,
}) as RemoteSettings

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

const LABEL = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/

/** Mirrors Go's CheckRemoteHost: a plain DNS name, no scheme, port or wildcard. */
export function validRemoteHost(name: string): boolean {
  if (name === '' || name.length > 253) return false
  return name.replace(/\.$/, '').split('.').every((l) => LABEL.test(l))
}

const normalizeHost = (h: string) => h.trim().replace(/\.$/, '').toLowerCase()

/** One host per line → the hosts to save (lowercased, unique) and the invalid lines. */
export function parseHosts(text: string): { hosts: string[]; invalid: string[] } {
  const hosts: string[] = []
  const invalid: string[] = []
  for (const line of text.split('\n')) {
    const h = normalizeHost(line)
    if (!h || hosts.includes(h)) continue
    if (validRemoteHost(h)) hosts.push(h)
    else invalid.push(line.trim())
  }
  if (hosts.length > REMOTE_LIMITS.hosts) invalid.push(...hosts.splice(REMOTE_LIMITS.hosts))
  return { hosts, invalid }
}

function int(v: unknown, lim: { min: number; max: number }, d: number): number {
  return typeof v === 'number' && Number.isInteger(v) && v >= lim.min && v <= lim.max ? v : d
}

/** Settings from untrusted JSON: invalid values get the defaults, like Go's Normalize. */
export function coerceRemote(v: unknown): RemoteSettings {
  const d = DEFAULT_REMOTE
  if (!isObject(v)) return d
  const hosts = Array.isArray(v.allowedHosts)
    ? parseHosts(v.allowedHosts.filter((h): h is string => typeof h === 'string').join('\n')).hosts
    : []
  return {
    enabled: typeof v.enabled === 'boolean' ? v.enabled : d.enabled,
    port: int(v.port, REMOTE_LIMITS.port, d.port),
    allowedHosts: hosts,
    replayEvents: int(v.replayEvents, REMOTE_LIMITS.replayEvents, d.replayEvents),
    replayKB: int(v.replayKB, REMOTE_LIMITS.replayKB, d.replayKB),
    requestsPerMinute: int(v.requestsPerMinute, REMOTE_LIMITS.requestsPerMinute, d.requestsPerMinute),
  }
}

/** m:ss left of a pairing (never negative). */
export function formatCountdown(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}
