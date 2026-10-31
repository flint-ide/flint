// AWS profile labels and commands, pure so they are testable.
import type { awsprof } from '../wails/models'

export type AwsProfile = awsprof.Profile

export type Tone = 'ok' | 'warn' | 'bad' | 'muted'

/** Credentials expiring sooner than this are shown as a warning. */
const SOON_MS = 15 * 60_000

const KIND_LABELS: Record<string, string> = {
  sso: 'SSO',
  temporary: 'session keys',
  keys: 'access keys',
  role: 'assumed role',
  process: 'credential process',
  config: 'settings only',
}

export const kindLabel = (kind: string) => KIND_LABELS[kind] ?? kind

/** "2h 05m", "12m", "45s". */
export function timeLeft(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ${String(m % 60).padStart(2, '0')}m`
  return `${Math.floor(h / 24)}d`
}

/** The status badge of a profile now (expiry is recomputed on the client, so it counts down). */
export function profileStatus(p: Pick<AwsProfile, 'status' | 'expiresAt' | 'source'>, now: number): { text: string; tone: Tone; title: string } {
  const expires = p.expiresAt ? Date.parse(p.expiresAt) : NaN
  if (!Number.isNaN(expires)) {
    const left = expires - now
    const at = new Date(expires).toLocaleString()
    if (left <= 0) return { text: 'expired', tone: 'bad', title: `Expired ${at}` }
    return { text: timeLeft(left), tone: left < SOON_MS ? 'warn' : 'ok', title: `Valid until ${at}` }
  }
  switch (p.status) {
    case 'logged-out':
      return { text: 'logged out', tone: 'bad', title: 'No cached login: log in first' }
    case 'static':
      return { text: 'keys', tone: 'muted', title: 'Long-term access keys (no expiry)' }
    default:
      return { text: '—', tone: 'muted', title: p.source ? `Uses the credentials of ${p.source}` : 'Checked when used' }
  }
}

/** A command line for a shell, single-quoting what needs it. */
export function shellCommand(argv: readonly string[]): string {
  return argv.map((a) => (/^[A-Za-z0-9_./:=@+,-]+$/.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`)).join(' ')
}

/** Environment of a terminal working as a profile. */
export function profileEnv(p: Pick<AwsProfile, 'name' | 'region'>): Record<string, string> {
  const env: Record<string, string> = { AWS_PROFILE: p.name }
  if (p.region) env.AWS_REGION = p.region
  return env
}

/** Same rule as Go's awsprof.ValidName. */
export const validProfileName = (name: string) => name.length <= 128 && /^[A-Za-z0-9_.@+=,/][A-Za-z0-9_.@+=,/-]*$/.test(name)
