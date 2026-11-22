import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { isActivationKey, processingState, visibleParts } from './listParts'

describe('visibleParts', () => {
  it('drops the parts that would leave an empty slot and a stray separator', () => {
    assert.deepEqual(visibleParts(['a', null, undefined, false, '', 'b']), ['a', 'b'])
  })
  it('keeps 0, a real count', () => {
    assert.deepEqual(visibleParts([0, 1]), [0, 1])
  })
  it('takes no parts at all', () => {
    assert.deepEqual(visibleParts(undefined), [])
    assert.deepEqual(visibleParts([]), [])
  })
  it('returns a new array', () => {
    const parts = ['a']
    assert.notEqual(visibleParts(parts), parts)
  })
})

describe('isActivationKey', () => {
  it('is Enter and Space only', () => {
    assert.equal(isActivationKey('Enter'), true)
    assert.equal(isActivationKey(' '), true)
    assert.equal(isActivationKey('ArrowDown'), false)
    assert.equal(isActivationKey('Escape'), false)
  })
})

describe('processingState', () => {
  it('maps a CI failure to the badge’s error', () => {
    assert.equal(processingState('failure'), 'error')
    assert.equal(processingState('pending'), 'pending')
    assert.equal(processingState('success'), 'success')
  })
})
