// The completions and inline edit parts of settings.json (Go:
// internal/settings/editorai.go). Go validates every value; these helpers only
// read values with their defaults and give the Settings tab its bounds.

export type CompletionProvider = 'ollama' | 'openai'

/** Inline suggestions from a code model (off by default). */
export interface CompletionSettings {
  enabled: boolean
  provider: CompletionProvider
  /** Base URL; http only for localhost, 127.0.0.1 and ::1. */
  endpoint: string
  model: string
  /** Typing pause before a suggestion is asked for. */
  debounceMs: number
  /** Longest suggestion, in tokens. */
  maxTokens: number
  /** Lines before and after the cursor that are sent. */
  contextLines: number
}

/** Edit with Agent (⌘I) on the selection. */
export interface InlineEditSettings {
  /** Lines before and after the selection the agent sees. */
  contextLines: number
}

/** Mirrors Go's defaultCompletions. */
export const DEFAULT_COMPLETIONS: CompletionSettings = Object.freeze({
  enabled: false,
  provider: 'ollama',
  endpoint: 'http://localhost:11434',
  model: 'qwen2.5-coder:1.5b-base',
  debounceMs: 300,
  maxTokens: 64,
  contextLines: 100,
})

/** Mirrors Go's defaultInlineEdit. */
export const DEFAULT_INLINE_EDIT: InlineEditSettings = Object.freeze({ contextLines: 60 })

/** Same bounds as Go (Min/MaxCompletion*, MaxInlineEditContextLines). */
export const COMPLETION_LIMITS = {
  debounceMs: { min: 50, max: 5000 },
  maxTokens: { min: 1, max: 512 },
  contextLines: { min: 1, max: 1000 },
} as const
export const INLINE_EDIT_LIMITS = { contextLines: { min: 0, max: 500 } } as const

/** Default endpoint of each provider (offered when switching). */
export const PROVIDER_ENDPOINTS: Record<CompletionProvider, string> = {
  ollama: 'http://localhost:11434',
  openai: 'http://localhost:8080',
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const num = (v: unknown, lim: { min: number; max: number }, d: number) =>
  typeof v === 'number' && Number.isFinite(v) && v >= lim.min && v <= lim.max ? Math.round(v) : d

/** Plain http only to this machine, like Go's CheckEndpoint (Go has the last word). */
export function validEndpoint(endpoint: string): boolean {
  let u: URL
  try {
    u = new URL(endpoint)
  } catch {
    return false
  }
  if (u.username || u.password || u.search || u.hash || endpoint.length > 256) return false
  if (u.protocol === 'https:') return !!u.hostname
  return u.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(u.hostname)
}

export const validModel = (m: string) => m.length > 0 && m.length <= 200 && /^[A-Za-z0-9][A-Za-z0-9._:/@+-]*$/.test(m)

export function coerceCompletions(v: unknown): CompletionSettings {
  const d = DEFAULT_COMPLETIONS
  if (!isObject(v)) return d
  const provider = v.provider === 'openai' || v.provider === 'ollama' ? v.provider : d.provider
  return {
    enabled: typeof v.enabled === 'boolean' ? v.enabled : d.enabled,
    provider,
    endpoint: typeof v.endpoint === 'string' && validEndpoint(v.endpoint) ? v.endpoint : d.endpoint,
    model: typeof v.model === 'string' && validModel(v.model) ? v.model : d.model,
    debounceMs: num(v.debounceMs, COMPLETION_LIMITS.debounceMs, d.debounceMs),
    maxTokens: num(v.maxTokens, COMPLETION_LIMITS.maxTokens, d.maxTokens),
    contextLines: num(v.contextLines, COMPLETION_LIMITS.contextLines, d.contextLines),
  }
}

export function coerceInlineEdit(v: unknown): InlineEditSettings {
  if (!isObject(v)) return DEFAULT_INLINE_EDIT
  return { contextLines: num(v.contextLines, INLINE_EDIT_LIMITS.contextLines, DEFAULT_INLINE_EDIT.contextLines) }
}
