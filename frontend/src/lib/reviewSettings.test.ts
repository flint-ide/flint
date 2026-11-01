import { it as test } from 'node:test'
import assert from 'node:assert/strict'
import { coerceAgentReview, DEFAULT_AGENT_REVIEW } from './reviewSettings'
import { coerceSettings } from './settings'

test('coerceAgentReview keeps valid values and defaults the rest', () => {
  assert.deepEqual(coerceAgentReview({ scope: 'all', maxDiffKB: 256, agent: 'codex', timeoutSeconds: 600 }), {
    scope: 'all', maxDiffKB: 256, agent: 'codex', timeoutSeconds: 600,
  })
  assert.deepEqual(coerceAgentReview({ scope: 'staged', maxDiffKB: 1, agent: 3, timeoutSeconds: 'x' }), DEFAULT_AGENT_REVIEW)
  assert.deepEqual(coerceAgentReview(null), DEFAULT_AGENT_REVIEW)
})

test('settings carry agentReview', () => {
  assert.deepEqual(coerceSettings({}).agentReview, DEFAULT_AGENT_REVIEW)
  assert.equal(coerceSettings({ agentReview: { scope: 'all' } }).agentReview.scope, 'all')
})
