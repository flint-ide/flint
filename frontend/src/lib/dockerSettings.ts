// The "docker" part of settings.json (Go: internal/settings/docker.go).

export interface DockerSettings {
  /** Where the engine is (unix://…, tcp://…, https://…); '' finds it like the docker CLI. */
  host: string
  /** Past lines a container's log tab starts with. */
  logTailLines: number
}

/** Mirrors Go's defaultDocker. */
export const DEFAULT_DOCKER: DockerSettings = Object.freeze({ host: '', logTailLines: 500 })

/** Same bounds as Go (Min/MaxDockerLogTailLines). */
export const DOCKER_LOG_TAIL = { min: 1, max: 10000 } as const

/** Same rule as Go's CheckDocker: empty, a path, or one of the four schemes. */
export function validDockerHost(v: string): boolean {
  const h = v.trim()
  if (h === '') return true
  if (h.length > 512 || /[\s\0]/.test(h)) return false
  return h.startsWith('/') || /^(unix|tcp|http|https):\/\/\S+$/.test(h)
}

export function coerceDocker(v: unknown): DockerSettings {
  const o = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {}
  const host = typeof o.host === 'string' && validDockerHost(o.host) ? o.host.trim() : DEFAULT_DOCKER.host
  const n = typeof o.logTailLines === 'number' && Number.isFinite(o.logTailLines) && o.logTailLines !== 0 ? Math.round(o.logTailLines) : DEFAULT_DOCKER.logTailLines
  return { host, logTailLines: Math.min(DOCKER_LOG_TAIL.max, Math.max(DOCKER_LOG_TAIL.min, n)) }
}
