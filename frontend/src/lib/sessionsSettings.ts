// Settings of background terminals (Go: internal/settings/sessions.go): with
// `background` on, terminals run in a separate host process and keep running
// when Flint's window closes. Go validates and clamps; this mirrors it for
// the Settings page.

/** The part of settings.json under "sessions". */
export interface SessionsSettings {
  /** Run terminals in the background host (read when Flint starts). */
  background: boolean
  /** Recent output the host keeps per terminal for a window that attaches, in KiB. */
  replayKiB: number
}

/** Mirrors Go's defaultSessions. */
export const DEFAULT_SESSIONS: SessionsSettings = Object.freeze({ background: false, replayKiB: 1024 })

/** Same bounds as Go (MinReplayKiB, MaxReplayKiB). */
export const REPLAY_KIB = { min: 64, max: 8192 } as const

/** Reads untrusted settings JSON the way Go's normalizeSessions does. */
export function coerceSessions(v: unknown): SessionsSettings {
  const o = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {}
  const kib = o.replayKiB
  const replayKiB =
    typeof kib !== 'number' || !Number.isFinite(kib) || kib === 0
      ? DEFAULT_SESSIONS.replayKiB
      : Math.min(REPLAY_KIB.max, Math.max(REPLAY_KIB.min, Math.round(kib)))
  return {
    background: typeof o.background === 'boolean' ? o.background : DEFAULT_SESSIONS.background,
    replayKiB,
  }
}
