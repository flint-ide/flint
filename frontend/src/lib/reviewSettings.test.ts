import { it as test } from 'node:test'
import assert from 'node:assert/strict'
import { autoApproveArgs, coerceAgentReview, DEFAULT_AGENT_REVIEW } from './reviewSettings'
import { coerceSettings } from './settings'

test('coerceAgentReview keeps valid values and defaults the rest', () => {
  assert.deepEqual(coerceAgentReview({ scope: 'all', maxDiffKB: 256, agent: 'codex', timeoutSeconds: 600 }), {
    scope: 'all', maxDiffKB: 256, agent: 'codex', timeoutSeconds: 600,
  })
  assert.deepEqual(coerceAgentReview({ scope: 'staged', maxDiffKB: 1, agent: 3, timeoutSeconds: 'x' }), DEFAULT_AGENT_REVIEW)
  assert.deepEqual(coerceAgentReview(null), DEFAULT_AGENT_REVIEW)
})

test('autoApproveArgs matches Go', () => {
  assert.deepEqual(autoApproveArgs(['--experimental-acp']), [])
  assert.deepEqual(autoApproveArgs(['--experimental-acp', '--yolo']), ['--yolo'])
  assert.deepEqual(autoApproveArgs(['-y', '--model', 'x']), ['-y'])
  assert.deepEqual(autoApproveArgs(['--approval-mode', 'yolo']), ['--approval-mode yolo'])
  assert.deepEqual(autoApproveArgs(['--approval-mode=auto_edit']), ['--approval-mode auto_edit'])
  assert.deepEqual(autoApproveArgs(['--approval-mode', 'default']), [])
  assert.deepEqual(autoApproveArgs(['--yolo-ish', 'yolo']), [])
})

test('settings carry agentReview', () => {
  assert.deepEqual(coerceSettings({}).agentReview, DEFAULT_AGENT_REVIEW)
  assert.equal(coerceSettings({ agentReview: { scope: 'all' } }).agentReview.scope, 'all')
})
