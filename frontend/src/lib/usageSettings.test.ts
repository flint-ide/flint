import { it as test } from 'node:test'
import assert from 'node:assert/strict'
import { coerceUsage, DEFAULT_USAGE, formatPrices, parsePrices } from './usageSettings'

test('coerceUsage keeps valid values and defaults the rest', () => {
  assert.deepEqual(coerceUsage(undefined), DEFAULT_USAGE)
  const u = coerceUsage({
    retentionDays: 2, dailyBudgetUsd: 5, taskBudgetTokens: -1, pauseOverBudget: true,
    prices: { 'gpt-5': { in: 1.25, out: 10 }, ' bad': { in: 1 }, huge: { in: 1e9 } },
  })
  assert.equal(u.retentionDays, 90)
  assert.equal(u.dailyBudgetUsd, 5)
  assert.equal(u.taskBudgetTokens, 0)
  assert.equal(u.pauseOverBudget, true)
  assert.deepEqual(u.prices, { 'gpt-5': { in: 1.25, out: 10 } })
})

test('prices parse from "model in out" lines and format back', () => {
  const { prices, invalid } = parsePrices('gpt-5 1.25 10\n# comment\n\ngpt-5-mini 0.25  # out defaults to 0\nbad line here x\nneg -1 2')
  assert.deepEqual(prices, { 'gpt-5': { in: 1.25, out: 10 }, 'gpt-5-mini': { in: 0.25, out: 0 } })
  assert.deepEqual(invalid, ['bad line here x', 'neg -1 2'])
  assert.equal(formatPrices(prices), 'gpt-5 1.25 10\ngpt-5-mini 0.25 0')
})
