import { it as test } from 'node:test'
import assert from 'node:assert/strict'
import {
  alertText, barHeights, chartMetric, costLabel, hasUnpricedTokens, isPaused, projectName, totalsLabel, type BudgetAlert, type UsageTotals,
} from './usageView'

const t = (over: Partial<UsageTotals>): UsageTotals => ({ inputTokens: 0, outputTokens: 0, costUsd: 0, turns: 1, ...over })

test('cost labels never invent a price and mark estimates', () => {
  assert.equal(costLabel(t({ inputTokens: 1000 })), '')
  assert.equal(costLabel(t({ costUsd: 1.5 })), '$1.50')
  assert.equal(costLabel(t({ estimatedUsd: 0.4 })), '~$0.400 est.')
  assert.equal(costLabel(t({ costUsd: 2, estimatedUsd: 0.4 })), '$2.00 + ~$0.400 est.')
  assert.equal(totalsLabel(t({ inputTokens: 12000, outputTokens: 300 })), '12k tokens')
  assert.equal(totalsLabel(t({ inputTokens: 900, costUsd: 0.12 })), '900 tokens · $0.120')
})

test('unpriced tokens are flagged only without an estimate', () => {
  assert.equal(hasUnpricedTokens(t({ unpricedInputTokens: 5 })), true)
  assert.equal(hasUnpricedTokens(t({ unpricedInputTokens: 5, estimatedUsd: 0.1 })), false)
  assert.equal(hasUnpricedTokens(t({ costUsd: 1 })), false)
})

test('chart shows money when known, else tokens', () => {
  assert.equal(chartMetric([{ totals: t({ inputTokens: 5 }) }]), 'tokens')
  assert.equal(chartMetric([{ totals: t({ inputTokens: 5 }) }, { totals: t({ estimatedUsd: 0.1 }) }]), 'cost')
})

test('bar heights scale to the largest and keep small values visible', () => {
  assert.deepEqual(barHeights([0, 0]), [0, 0])
  assert.deepEqual(barHeights([0, 50, 100]), [0, 50, 100])
  assert.deepEqual(barHeights([1, 1000]), [2, 100])
})

test('alert text names the scope and the amounts', () => {
  const a: BudgetAlert = { scope: 'day', key: '2026-11-01', kind: 'usd', limit: 5, spent: 5.2, estimated: true, overridden: false }
  assert.equal(alertText(a), 'Daily budget reached: $5.20 of $5.00 (partly estimated)')
  assert.equal(
    alertText({ ...a, scope: 'task', kind: 'tokens', limit: 1_000_000, spent: 1_200_000, estimated: false }, 'Rate limits'),
    'Task budget of “Rate limits” reached: 1.2M of 1.0M tokens',
  )
  assert.equal(isPaused([a], true), true)
  assert.equal(isPaused([{ ...a, overridden: true }], true), false)
  assert.equal(isPaused([a], false), false)
})

test('project name is the folder name', () => {
  assert.equal(projectName('/work/flint'), 'flint')
  assert.equal(projectName('/'), '/')
})
