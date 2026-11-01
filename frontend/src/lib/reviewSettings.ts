// The agentReview part of settings.json (Go: internal/settings/review.go). Go
// validates every value; this reads values with their defaults and gives the
// Settings tab its bounds.

export type ReviewScope = 'selected' | 'all'

/** Review Changes with Agent. */
export interface AgentReviewSettings {
  /** What a review covers: the files ticked in the Commit panel, or every change. */
  scope: ReviewScope
  /** Largest diff sent to the agent; larger ones are shortened per file. */
  maxDiffKB: number
  /** The reviewing agent ('' = the default agent). */
  agent: string
  /** A review that takes longer is stopped. */
  timeoutSeconds: number
}

/** Mirrors Go's defaultAgentReview. */
export const DEFAULT_AGENT_REVIEW: AgentReviewSettings = Object.freeze({ scope: 'selected', maxDiffKB: 128, agent: '', timeoutSeconds: 300 })

/** Same bounds as Go (Min/MaxReview*). */
export const AGENT_REVIEW_LIMITS = {
  maxDiffKB: { min: 8, max: 512 },
  timeoutSeconds: { min: 30, max: 1800 },
} as const

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const num = (v: unknown, lim: { min: number; max: number }, d: number) =>
  typeof v === 'number' && Number.isFinite(v) && v >= lim.min && v <= lim.max ? Math.round(v) : d

export function coerceAgentReview(v: unknown): AgentReviewSettings {
  const d = DEFAULT_AGENT_REVIEW
  if (!isObject(v)) return d
  return {
    scope: v.scope === 'all' || v.scope === 'selected' ? v.scope : d.scope,
    maxDiffKB: num(v.maxDiffKB, AGENT_REVIEW_LIMITS.maxDiffKB, d.maxDiffKB),
    agent: typeof v.agent === 'string' ? v.agent : d.agent,
    timeoutSeconds: num(v.timeoutSeconds, AGENT_REVIEW_LIMITS.timeoutSeconds, d.timeoutSeconds),
  }
}
