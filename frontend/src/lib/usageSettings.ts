// Settings ▸ Usage (Go: internal/settings/usage.go): retention of the daily
// aggregates, budgets, and per-model prices for agents that report no cost.

export interface ModelPrice {
  /** USD per million input tokens. */
  in: number
  /** USD per million output tokens. */
  out: number
}

export interface UsageSettings {
  retentionDays: number
  prices: Record<string, ModelPrice>
  dailyBudgetUsd: number
  dailyBudgetTokens: number
  taskBudgetUsd: number
  taskBudgetTokens: number
  pauseOverBudget: boolean
}

/** Same limits as Go's CheckUsage. */
export const USAGE_LIMITS = {
  retentionDays: { min: 7, max: 3650 },
  budgetUsd: { min: 0, max: 1_000_000 },
  budgetTokens: { min: 0, max: 1_000_000_000_000 },
  price: { min: 0, max: 10_000 },
  prices: 200,
  modelName: 200,
} as const

export const DEFAULT_USAGE: UsageSettings = Object.freeze({
  retentionDays: 90,
  prices: Object.freeze({}) as Record<string, ModelPrice>,
  dailyBudgetUsd: 0,
  dailyBudgetTokens: 0,
  taskBudgetUsd: 0,
  taskBudgetTokens: 0,
  pauseOverBudget: false,
}) as UsageSettings

export const validModelName = (m: string): boolean =>
  m !== '' && m === m.trim() && m.length <= USAGE_LIMITS.modelName && !/[\x00-\x1f\x7f]/.test(m)

const validPrice = (n: unknown): n is number =>
  typeof n === 'number' && Number.isFinite(n) && n >= USAGE_LIMITS.price.min && n <= USAGE_LIMITS.price.max

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

function inRange(v: unknown, lim: { min: number; max: number }, def: number): number {
  return typeof v === 'number' && Number.isFinite(v) && v >= lim.min && v <= lim.max ? v : def
}

/** Settings from untrusted JSON, like Go's normalizeUsage. */
export function coerceUsage(v: unknown): UsageSettings {
  if (!isObject(v)) return DEFAULT_USAGE
  const d = DEFAULT_USAGE
  const prices: Record<string, ModelPrice> = {}
  if (isObject(v.prices)) {
    for (const [model, p] of Object.entries(v.prices)) {
      if (Object.keys(prices).length >= USAGE_LIMITS.prices) break
      if (validModelName(model) && isObject(p) && validPrice(p.in ?? 0) && validPrice(p.out ?? 0)) {
        prices[model] = { in: (p.in as number) ?? 0, out: (p.out as number) ?? 0 }
      }
    }
  }
  return {
    retentionDays: Math.round(inRange(v.retentionDays, USAGE_LIMITS.retentionDays, d.retentionDays)),
    prices,
    dailyBudgetUsd: inRange(v.dailyBudgetUsd, USAGE_LIMITS.budgetUsd, 0),
    dailyBudgetTokens: Math.round(inRange(v.dailyBudgetTokens, USAGE_LIMITS.budgetTokens, 0)),
    taskBudgetUsd: inRange(v.taskBudgetUsd, USAGE_LIMITS.budgetUsd, 0),
    taskBudgetTokens: Math.round(inRange(v.taskBudgetTokens, USAGE_LIMITS.budgetTokens, 0)),
    pauseOverBudget: typeof v.pauseOverBudget === 'boolean' ? v.pauseOverBudget : d.pauseOverBudget,
  }
}

/**
 * Prices from the editor's text, one "model in out" per line (USD per million
 * tokens, "#" starts a comment). Returns the prices and the lines that are not valid.
 */
export function parsePrices(text: string): { prices: Record<string, ModelPrice>; invalid: string[] } {
  const prices: Record<string, ModelPrice> = {}
  const invalid: string[] = []
  for (const raw of text.split('\n')) {
    const line = raw.replace(/#.*/, '').trim()
    if (!line) continue
    const parts = line.split(/\s+/)
    const [model, inp, out] = parts
    const pIn = Number(inp)
    const pOut = Number(out ?? '0')
    if (parts.length > 3 || !model || !validModelName(model) || inp === undefined || !validPrice(pIn) || !validPrice(pOut)) {
      invalid.push(raw.trim())
    } else if (Object.keys(prices).length < USAGE_LIMITS.prices) {
      prices[model] = { in: pIn, out: pOut }
    } else {
      invalid.push(raw.trim())
    }
  }
  return { prices, invalid }
}

/** The editor's text for prices, sorted by model. */
export function formatPrices(prices: Record<string, ModelPrice>): string {
  return Object.keys(prices)
    .sort()
    .map((m) => `${m} ${prices[m].in} ${prices[m].out}`)
    .join('\n')
}
