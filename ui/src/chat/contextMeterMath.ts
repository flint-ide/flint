// Pure maths behind ContextMeter: percentages, part shares, token formatting and the
// ring's stroke. Free of React and the DOM so node:test covers them.

export type ContextPart = { id: string; label: string; tokens: number }
export type PartShare = ContextPart & { share: number }

function finiteOrZero(n: number | null | undefined): number {
  return typeof n === 'number' && Number.isFinite(n) ? n : 0
}

/** Float drift (1.4e-14 % left over by scaling) counts as nothing. */
const EPSILON = 1e-9

function snap(percent: number): number {
  return Math.abs(percent) < EPSILON ? 0 : percent
}

function clamp(n: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, n))
}

/** True when the agent reported a usable (finite, positive) context window. */
export function hasLimit(limit: number | null | undefined): limit is number {
  return typeof limit === 'number' && Number.isFinite(limit) && limit > 0
}

/** The fill as a whole percentage, clamped to 0-100; 0 without a usable limit. */
export function contextPercent(used: number, limit: number | null | undefined): number {
  if (!hasLimit(limit)) return 0
  return Math.round(clamp((finiteOrZero(used) / limit) * 100, 0, 100))
}

/** A compact token count: "980", "12.4k", "1.2M". */
export function formatTokens(tokens: number): string {
  const n = Math.round(clamp(finiteOrZero(tokens), 0, Number.MAX_SAFE_INTEGER))
  if (n < 1000) return String(n)
  const k = Math.round((n / 1000) * 10) / 10
  if (k < 1000) return `${k}k`
  return `${Math.round((n / 1_000_000) * 10) / 10}M`
}

/** "62%, 124k of 200k tokens" (or with another separator, for a tooltip). */
export function usageText(used: number, limit: number, separator = ', '): string {
  const tokens = `${formatTokens(used)} of ${formatTokens(limit)} tokens`
  return `${contextPercent(used, limit)}%${separator}${tokens}`
}

/**
 * Each part's share of the window, in percent and in order. When the parts add up to more
 * than the limit they are scaled down together, so the total never passes 100.
 */
export function partShares(parts: readonly ContextPart[], limit: number | null | undefined): PartShare[] {
  if (!hasLimit(limit)) return []
  const tokens = parts.map((p) => Math.max(0, finiteOrZero(p.tokens)))
  const sum = tokens.reduce((a, b) => a + b, 0)
  const base = Math.max(limit, sum)
  return parts.map((p, i) => ({ ...p, share: snap((tokens[i] / base) * 100) }))
}

export type RingGeometry = {
  center: number
  radius: number
  circumference: number
  /** Length of the drawn arc. */
  filled: number
  dashArray: string
}

/** The ring's circle: the arc is `percent` of the circumference, starting at the top (rotate -90deg in CSS). */
export function ringGeometry(size: number, percent: number, stroke: number): RingGeometry {
  const radius = Math.max(0, (size - stroke) / 2)
  const circumference = 2 * Math.PI * radius
  const filled = (clamp(finiteOrZero(percent), 0, 100) / 100) * circumference
  return { center: size / 2, radius, circumference, filled, dashArray: `${filled} ${circumference}` }
}

/** A share for the legend: whole percent, "<1%" for a sliver that would round to nothing. */
export function formatShare(share: number): string {
  const n = clamp(finiteOrZero(share), 0, 100)
  if (n > 0 && n < 1) return '<1%'
  return `${Math.round(n)}%`
}

/** How many colours the parts cycle through (see `data-part-index` in ui/css/components.css). */
export const PART_COLORS = 6

/** The free space left in the window, in percent of it; 0 when the parts fill it. */
export function freeShare(shares: readonly PartShare[]): number {
  return snap(Math.max(0, 100 - shares.reduce((sum, s) => sum + s.share, 0)))
}

export type UsageBreakdown = {
  shares: PartShare[]
  /** Percent of the window used but not explained by the parts. */
  other: number
  otherTokens: number
  /** Percent of the window left over: what the bar's unfilled track shows. */
  free: number
}

/** The bar's segments and legend: the parts, an "other" for the rest of `used`, and what is free, so the bar always agrees with the ring. */
export function usageBreakdown(parts: readonly ContextPart[], used: number, limit: number | null | undefined): UsageBreakdown {
  if (!hasLimit(limit)) return { shares: [], other: 0, otherTokens: 0, free: 100 }
  const shares = partShares(parts, limit)
  const partsTotal = shares.reduce((sum, s) => sum + s.share, 0)
  const usedShare = clamp((finiteOrZero(used) / limit) * 100, 0, 100)
  const other = snap(Math.max(0, usedShare - partsTotal))
  const free = snap(Math.max(0, 100 - Math.max(usedShare, partsTotal)))
  return { shares, other, otherTokens: (limit * other) / 100, free }
}
