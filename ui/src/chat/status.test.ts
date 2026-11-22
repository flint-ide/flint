import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { contextLevel, isNearBottom, isRunning, toolStatus } from './status'

describe('isNearBottom', () => {
  it('follows at the bottom and within 40 px of it', () => {
    assert.equal(isNearBottom({ scrollHeight: 1000, scrollTop: 600, clientHeight: 400 }), true)
    assert.equal(isNearBottom({ scrollHeight: 1000, scrollTop: 561, clientHeight: 400 }), true)
  })
  it('stops following once the reader scrolls further up', () => {
    assert.equal(isNearBottom({ scrollHeight: 1000, scrollTop: 560, clientHeight: 400 }), false)
  })
  it('takes a custom threshold', () => {
    assert.equal(isNearBottom({ scrollHeight: 1000, scrollTop: 500, clientHeight: 400 }, 200), true)
  })
})

describe('toolStatus', () => {
  it('keeps known states', () => {
    assert.equal(toolStatus('completed'), 'completed')
    assert.equal(toolStatus('cancelled'), 'cancelled')
  })
  it('treats missing and unknown states as pending', () => {
    assert.equal(toolStatus(undefined), 'pending')
    assert.equal(toolStatus(null), 'pending')
    assert.equal(toolStatus('weird'), 'pending')
  })
})

describe('isRunning', () => {
  it('is true for pending and in progress only', () => {
    assert.equal(isRunning('pending'), true)
    assert.equal(isRunning('in_progress'), true)
    assert.equal(isRunning('failed'), false)
  })
})

describe('contextLevel', () => {
  it('turns amber at 70 and red at 90', () => {
    assert.equal(contextLevel(69), 'ok')
    assert.equal(contextLevel(70), 'warn')
    assert.equal(contextLevel(89), 'warn')
    assert.equal(contextLevel(90), 'full')
  })
})
