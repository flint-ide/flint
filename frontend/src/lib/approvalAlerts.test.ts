import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { approvalKey, approvalNotice, badgeCount, emptyApprovals, focused, opened, settled, settledSession } from './approvalAlerts'

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

describe('approval tracking', () => {
  const key = approvalKey('s1', 'r1')

  it('notifies a request once', () => {
    const first = opened(emptyApprovals, key, true)
    assert.equal(first.notify, true)
    assert.equal(opened(first.state, key, true).notify, false)
  })
  it('does not notify or count while the window has focus', () => {
    const r = opened(emptyApprovals, key, false)
    assert.equal(r.notify, false)
    assert.equal(badgeCount(2, r.state), 2)
  })
  it('counts open requests in the badge and drops them when settled', () => {
    const a = opened(opened(emptyApprovals, key, true).state, approvalKey('s2', 'r9'), true).state
    assert.equal(badgeCount(1, a), 3)
    assert.equal(badgeCount(1, settled(a, key)), 2)
  })
  it('forgets all requests of a session whose turn ended', () => {
    const a = opened(opened(emptyApprovals, key, true).state, approvalKey('s2', 'r9'), true).state
    assert.equal(badgeCount(0, settledSession(a, 's1')), 1)
  })
  it('refocusing clears the count but remembers the request', () => {
    const a = focused(opened(emptyApprovals, key, true).state)
    assert.equal(badgeCount(0, a), 0)
    assert.equal(opened(a, key, true).notify, false)
  })
})
