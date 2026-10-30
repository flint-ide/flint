// Formatting and aggregation of agent usage (tokens, cost, context fill).
import { zeroTotals, type ChatState, type UsageTotals } from './agentReducer'

/** A chat's totals; zero for chats saved before usage was tracked. */
export const totalsOf = (chat: ChatState): UsageTotals => chat.totals ?? zeroTotals

/** Project-level sum: tokens and cost add up; context is not summable and dropped. */
export function sumTotals(chats: ChatState[]): UsageTotals {
  return chats.reduce<UsageTotals>((acc, c) => {
    const t = totalsOf(c)
    return {
      inputTokens: acc.inputTokens + t.inputTokens,
      outputTokens: acc.outputTokens + t.outputTokens,
      costUsd: acc.costUsd + t.costUsd,
      contextTokens: 0,
      contextWindow: 0,
    }
  }, zeroTotals)
}

export const hasUsage = (t: UsageTotals): boolean => t.inputTokens + t.outputTokens + t.costUsd > 0

/** 1234 -> "1.2k", 1_500_000 -> "1.5M". */
export function formatTokens(n: number): string {
  if (n < 1000) return String(Math.round(n))
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

export function formatCost(usd: number): string {
  return `$${usd.toFixed(usd < 1 ? 3 : 2)}`
}

/** Context fill in whole percent (0-100), or null when the window is unknown. */
export function contextPercent(t: UsageTotals): number | null {
  if (t.contextWindow <= 0 || t.contextTokens <= 0) return null
  return Math.min(100, Math.round((t.contextTokens / t.contextWindow) * 100))
}

/** Multi-line tooltip with the details behind the compact summary. */
export function usageTooltip(t: UsageTotals): string {
  const pct = contextPercent(t)
  const lines = [
    `Input: ${t.inputTokens.toLocaleString()} tokens (incl. cache)`,
    `Output: ${t.outputTokens.toLocaleString()} tokens`,
  ]
  if (t.costUsd > 0) lines.push(`Cost: ${formatCost(t.costUsd)}`)
  if (pct !== null) {
    lines.push(`Context: ${t.contextTokens.toLocaleString()} / ${t.contextWindow.toLocaleString()} tokens (${pct}%)`)
  }
  return lines.join('\n')
}
