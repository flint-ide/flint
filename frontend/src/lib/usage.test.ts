import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { emptyChat, type ChatState, type UsageTotals } from './agentReducer'
import { trimChat } from './sessionSnapshot'
import { contextPercent, formatCost, formatTokens, hasUsage, sumTotals, totalsOf, usageTooltip } from './usage'

const totals = (t: Partial<UsageTotals>): UsageTotals => ({
  inputTokens: 0, outputTokens: 0, costUsd: 0, contextTokens: 0, contextWindow: 0, ...t,
})
const chatWith = (t?: UsageTotals): ChatState => ({ ...emptyChat, totals: t })

describe('usage totals', () => {
  it('treats chats saved without totals as zero', () => {
    const old = JSON.parse(JSON.stringify(emptyChat)) as ChatState // a snapshot from before usage tracking
    assert.equal(hasUsage(totalsOf(old)), false)
    assert.equal(hasUsage(sumTotals([old])), false)
  })

  it('survives trimming for the snapshot', () => {
    const t = totals({ inputTokens: 5, costUsd: 0.5 })
    assert.deepEqual(trimChat(chatWith(t)).totals, t)
  })

  it('sums tokens and cost over chats and drops the context reading', () => {
    const sum = sumTotals([
      chatWith(totals({ inputTokens: 100, outputTokens: 10, costUsd: 0.25, contextTokens: 50, contextWindow: 200 })),
      chatWith(),
      chatWith(totals({ inputTokens: 1, outputTokens: 2, costUsd: 0.5 })),
    ])
    assert.deepEqual(sum, totals({ inputTokens: 101, outputTokens: 12, costUsd: 0.75 }))
    assert.equal(hasUsage(sum), true)
  })
})

describe('usage formatting', () => {
  it('abbreviates tokens', () => {
    assert.equal(formatTokens(999), '999')
    assert.equal(formatTokens(1234), '1.2k')
    assert.equal(formatTokens(42_000), '42k')
    assert.equal(formatTokens(1_500_000), '1.5M')
  })

  it('formats cost with more precision below a dollar', () => {
    assert.equal(formatCost(0.0065881), '$0.007')
    assert.equal(formatCost(12.345), '$12.35')
  })

  it('computes the context percentage only when the window is known', () => {
    assert.equal(contextPercent(totals({ contextTokens: 42, contextWindow: 100 })), 42)
    assert.equal(contextPercent(totals({ contextTokens: 300, contextWindow: 100 })), 100)
    assert.equal(contextPercent(totals({ contextTokens: 42 })), null)
    assert.equal(contextPercent(totals({ contextWindow: 100 })), null)
  })

  it('lists cost and context in the tooltip only when known', () => {
    assert.equal(usageTooltip(totals({ inputTokens: 1000, outputTokens: 5 })).includes('Cost'), false)
    const tip = usageTooltip(totals({ inputTokens: 1000, outputTokens: 5, costUsd: 0.1, contextTokens: 50, contextWindow: 100 }))
    assert.ok(tip.includes('Cost: $0.100'))
    assert.ok(tip.includes('(50%)'))
  })
})
