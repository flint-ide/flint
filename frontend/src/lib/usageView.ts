// Pure helpers of the Usage panel and the budget warning (UsageAPI in Go):
// money and token labels, chart scaling, alert text. Framework-free, so they
// are testable with node:test.
import { formatCost, formatTokens } from './usage'

/** Mirrors Go's usage.Totals. */
export interface UsageTotals {
  inputTokens: number
  outputTokens: number
  cacheReadTokens?: number
  cacheWriteTokens?: number
  costUsd: number
  unpricedInputTokens?: number
  unpricedOutputTokens?: number
  turns: number
  estimatedUsd?: number
}

/** Mirrors Go's usage.Alert. */
export interface BudgetAlert {
  scope: string
  key: string
  kind: string
  limit: number
  spent: number
  estimated: boolean
  overridden: boolean
}

export const tokensOf = (t: UsageTotals): number => t.inputTokens + t.outputTokens

/** Money spent, reported plus estimated. */
export const spentOf = (t: UsageTotals): number => t.costUsd + (t.estimatedUsd ?? 0)

/** True when part of t's tokens has no known cost at all. */
export const hasUnpricedTokens = (t: UsageTotals): boolean =>
  (t.unpricedInputTokens ?? 0) + (t.unpricedOutputTokens ?? 0) > 0 && !(t.estimatedUsd && t.estimatedUsd > 0)

/**
 * The money label: "$1.20" when reported, "~$0.40 est." when only estimated,
 * "$1.20 + ~$0.40 est." for both, and "" when no cost is known (show tokens only).
 */
export function costLabel(t: UsageTotals): string {
  const est = t.estimatedUsd ?? 0
  if (t.costUsd > 0 && est > 0) return `${formatCost(t.costUsd)} + ~${formatCost(est)} est.`
  if (est > 0) return `~${formatCost(est)} est.`
  if (t.costUsd > 0) return formatCost(t.costUsd)
  return ''
}

/** "12.3k tokens · $0.120" or just the tokens when no cost is known. */
export function totalsLabel(t: UsageTotals): string {
  const cost = costLabel(t)
  const tokens = `${formatTokens(tokensOf(t))} tokens`
  return cost ? `${tokens} · ${cost}` : tokens
}

/** Detail lines for a tooltip. */
export function totalsTooltip(t: UsageTotals): string {
  const lines = [
    `Input: ${t.inputTokens.toLocaleString()} tokens${t.cacheReadTokens || t.cacheWriteTokens ? ` (cache read ${(t.cacheReadTokens ?? 0).toLocaleString()}, written ${(t.cacheWriteTokens ?? 0).toLocaleString()})` : ''}`,
    `Output: ${t.outputTokens.toLocaleString()} tokens`,
    `Turns: ${t.turns}`,
  ]
  if (t.costUsd > 0) lines.push(`Reported cost: ${formatCost(t.costUsd)}`)
  if ((t.estimatedUsd ?? 0) > 0) lines.push(`Estimated from your prices: ${formatCost(t.estimatedUsd ?? 0)}`)
  if (hasUnpricedTokens(t)) lines.push('Some tokens have no reported cost and no price in Settings ▸ Usage')
  return lines.join('\n')
}

export type ChartMetric = 'cost' | 'tokens'

/** The chart shows money when any is known in the range, else tokens. */
export function chartMetric(days: { totals: UsageTotals }[]): ChartMetric {
  return days.some((d) => spentOf(d.totals) > 0) ? 'cost' : 'tokens'
}

export const metricValue = (t: UsageTotals, m: ChartMetric): number => (m === 'cost' ? spentOf(t) : tokensOf(t))

export const metricLabel = (v: number, m: ChartMetric): string => (m === 'cost' ? formatCost(v) : formatTokens(v))

/** Bar heights in percent of the largest value (a non-zero value is at least 2% so it shows). */
export function barHeights(values: number[]): number[] {
  const top = Math.max(0, ...values)
  if (top <= 0) return values.map(() => 0)
  return values.map((v) => (v <= 0 ? 0 : Math.max(2, (v / top) * 100)))
}

/** Last path element of a project folder. */
export const projectName = (path: string): string => path.slice(path.lastIndexOf('/') + 1) || path

/** "Nov 1" for "2026-11-01" (local day, no time zone shift). */
export function shortDay(day: string): string {
  const [y, m, d] = day.split('-').map(Number)
  if (!y || !m || !d) return day
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

/** One line about a spent budget, for the chat header and notifications. */
export function alertText(a: BudgetAlert, taskTitle = ''): string {
  const what = a.scope === 'task' ? `Task budget${taskTitle ? ` of “${taskTitle}”` : ''}` : 'Daily budget'
  if (a.kind === 'tokens') return `${what} reached: ${formatTokens(a.spent)} of ${formatTokens(a.limit)} tokens`
  return `${what} reached: ${formatCost(a.spent)} of ${formatCost(a.limit)}${a.estimated ? ' (partly estimated)' : ''}`
}

/** Whether new turns are paused by the alerts (pause on, one not overridden). */
export const isPaused = (alerts: BudgetAlert[], pause: boolean): boolean => pause && alerts.some((a) => !a.overridden)
