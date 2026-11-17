import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { createEventHold } from './eventHold'

describe('createEventHold', () => {
  it('does not keep events for an id that is not held', () => {
    const hold = createEventHold<number>(3)
    assert.equal(hold.offer('a', 1), false)
    assert.deepEqual(hold.end('a'), [])
  })

  it('hands held events back in order, once', () => {
    const hold = createEventHold<number>(10)
    hold.begin('a')
    assert.equal(hold.offer('a', 1), true)
    assert.equal(hold.offer('b', 9), false)
    assert.equal(hold.offer('a', 2), true)
    assert.deepEqual(hold.end('a'), [1, 2])
    assert.deepEqual(hold.end('a'), [])
    assert.equal(hold.offer('a', 3), false) // released: delivered normally again
  })

  it('bounds a held list: the overflow is dropped but still counts as held', () => {
    const hold = createEventHold<number>(2)
    hold.begin('a')
    for (const n of [1, 2, 3, 4]) assert.equal(hold.offer('a', n), true)
    assert.deepEqual(hold.end('a'), [1, 2])
  })

  it('keeps what was held when begun twice', () => {
    const hold = createEventHold<number>(5)
    hold.begin('a')
    hold.offer('a', 1)
    hold.begin('a')
    assert.deepEqual(hold.end('a'), [1])
  })
})
