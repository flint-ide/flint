import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { approvalNotice } from './approvalAlerts'

describe('approvalNotice', () => {
  it('names the agent, the project and what is asked', () => {
    const n = approvalNotice('claude', '/src/my-app', { id: 'r', toolCall: { id: 't', title: 'Run npm test' }, options: [] })
    assert.deepEqual(n, { title: 'claude is waiting for your approval', body: 'my-app: Run npm test' })
  })
  it('falls back to the kind of tool, then to a generic text', () => {
    assert.equal(approvalNotice('codex', '/p', { id: 'r', toolCall: { id: 't', kind: 'edit' }, options: [] }).body, 'p: edit')
    assert.equal(approvalNotice('codex', '/p', { id: 'r', toolCall: { id: 't' }, options: [] }).body, 'p: a tool call')
  })
  it('keeps one short line', () => {
    const body = approvalNotice('a', '/p', { id: 'r', toolCall: { id: 't', title: `x\n${'y'.repeat(300)}` }, options: [] }).body
    assert.ok(!body.includes('\n'))
    assert.ok(body.length <= 140)
  })
})
