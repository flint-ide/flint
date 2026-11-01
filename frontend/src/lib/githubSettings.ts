// settings.github (internal/settings/github.go): the GitHub host, the OAuth
// App Flint signs in with, the scopes it asks for and how long a read waits
// for GitHub's "retry after". Pure, so it is testable with node:test.

export interface GitHubSettings {
  /** github.com or a GitHub Enterprise Server host (ghe.example.com). */
  host: string
  /** The OAuth App's client id (Device Flow enabled); '' until one is configured. */
  clientId: string
  scopes: string[]
  maxRetryWaitSeconds: number
}

export const DEFAULT_GITHUB_SCOPES = Object.freeze(['repo', 'read:org', 'workflow']) as unknown as string[]

export const DEFAULT_GITHUB: GitHubSettings = Object.freeze({
  host: 'github.com',
  clientId: '',
  scopes: DEFAULT_GITHUB_SCOPES,
  maxRetryWaitSeconds: 60,
})

/** Same bounds as Go (MaxGitHubRetryWait). */
export const GITHUB_RETRY_WAIT = { min: 0, max: 300 }

const LABEL = /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/

/** Mirrors Go's CheckGitHubHost: a host name with an optional port, nothing else. */
export function validGitHubHost(host: string): boolean {
  if (host === '' || host.length > 253) return false
  let name = host
  const colon = host.lastIndexOf(':')
  if (colon >= 0) {
    const port = host.slice(colon + 1)
    if (!/^[1-9]\d{0,4}$/.test(port) || Number(port) > 65535) return false
    name = host.slice(0, colon)
  }
  return name.split('.').every((l) => LABEL.test(l))
}

/** Mirrors Go's clientIDRE (empty is allowed: sign-in is then unavailable). */
export const validClientId = (id: string): boolean => id === '' || (id.length <= 100 && /^[A-Za-z0-9._-]+$/.test(id))

/** Mirrors Go's scopeRE. */
export const validScope = (s: string): boolean => /^[a-z][a-z0-9_]*(?::[a-z][a-z0-9_]*)?$/.test(s)

/** Scopes typed as "repo, read:org workflow" → the valid ones (unique) and the rest. */
export function parseScopes(text: string): { scopes: string[]; invalid: string[] } {
  const scopes: string[] = []
  const invalid: string[] = []
  for (const s of text.split(/[\s,]+/).filter(Boolean)) {
    if (!validScope(s)) invalid.push(s)
    else if (!scopes.includes(s)) scopes.push(s)
  }
  return { scopes: scopes.slice(0, 20), invalid }
}

export function coerceGitHub(v: unknown): GitHubSettings {
  const o = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {}
  const host = typeof o.host === 'string' ? o.host.trim().toLowerCase() : ''
  const clientId = typeof o.clientId === 'string' ? o.clientId.trim() : ''
  const wait = o.maxRetryWaitSeconds
  return {
    host: validGitHubHost(host) ? host : DEFAULT_GITHUB.host,
    clientId: validClientId(clientId) ? clientId : '',
    scopes: Array.isArray(o.scopes) ? parseScopes(o.scopes.filter((s) => typeof s === 'string').join(' ')).scopes : [...DEFAULT_GITHUB_SCOPES],
    maxRetryWaitSeconds:
      typeof wait === 'number' && Number.isInteger(wait) && wait >= GITHUB_RETRY_WAIT.min && wait <= GITHUB_RETRY_WAIT.max ? wait : DEFAULT_GITHUB.maxRetryWaitSeconds,
  }
}

/**
 * Whether an error from Go means "not signed in to GitHub" (github.ErrNotAuthenticated),
 * where the UI offers Sign in with GitHub in place.
 */
export const isSignInError = (message: string | null | undefined): boolean => !!message && /not signed in to GitHub/i.test(message)
