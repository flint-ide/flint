// Settings of agent orchestration: agents that start and direct other agents
// (Go: internal/settings/orchestration.go). The tools are off until
// `enabled`; Go validates and clamps, this mirrors it for the Settings page.

/** The part of settings.json under "orchestration". */
export interface OrchestrationSettings {
  /** Offer the orchestration tools to agents (each chat still needs the user's grant). */
  enabled: boolean
  /** Agents one chat may have started at a time. */
  maxChildren: number
  /** Levels of agents that may start agents. */
  maxDepth: number
  /** Longest one wait_for_agent call, in seconds. */
  maxWaitSeconds: number
  /** Tell a chat when an agent it started waits, fails or finishes. */
  notifyParent: boolean
  /** Most lines read_agent_output returns. */
  outputLines: number
  /** Most turns agents may start in each other before the user writes in the group again. */
  maxAutoTurns: number
}

/** Mirrors Go's defaultOrchestration. */
export const DEFAULT_ORCHESTRATION: OrchestrationSettings = Object.freeze({
  enabled: false,
  maxChildren: 4,
  maxDepth: 2,
  maxWaitSeconds: 600,
  notifyParent: true,
  outputLines: 200,
  maxAutoTurns: 40,
})

/** Same bounds as Go (MaxOrchestration*). */
export const ORCHESTRATION_LIMITS = {
  maxChildren: { min: 1, max: 16 },
  maxDepth: { min: 1, max: 4 },
  maxWaitSeconds: { min: 1, max: 600 },
  outputLines: { min: 1, max: 2000 },
  maxAutoTurns: { min: 1, max: 400 },
} as const

type NumberKey = keyof typeof ORCHESTRATION_LIMITS

function clamp(key: NumberKey, v: unknown): number {
  if (typeof v !== 'number' || !Number.isFinite(v) || v === 0) return DEFAULT_ORCHESTRATION[key]
  const { min, max } = ORCHESTRATION_LIMITS[key]
  return Math.min(max, Math.max(min, Math.round(v)))
}

/** Reads untrusted settings JSON the way Go's normalizeOrchestration does. */
export function coerceOrchestration(v: unknown): OrchestrationSettings {
  const o = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {}
  const d = DEFAULT_ORCHESTRATION
  return {
    enabled: typeof o.enabled === 'boolean' ? o.enabled : d.enabled,
    maxChildren: clamp('maxChildren', o.maxChildren),
    maxDepth: clamp('maxDepth', o.maxDepth),
    maxWaitSeconds: clamp('maxWaitSeconds', o.maxWaitSeconds),
    notifyParent: typeof o.notifyParent === 'boolean' ? o.notifyParent : d.notifyParent,
    outputLines: clamp('outputLines', o.outputLines),
    maxAutoTurns: clamp('maxAutoTurns', o.maxAutoTurns),
  }
}
