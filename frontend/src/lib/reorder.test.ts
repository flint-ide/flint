import { afterEach, describe, it, mock } from 'node:test'
import assert from 'node:assert/strict'
import { createReorderer } from './reorder'

type Ev = { seq?: number; v: string }

const collect = (maxWait?: number) => {
  const out: string[] = []
  const push = createReorderer<Ev>((s, e) => out.push(`${s}:${e.v}`), maxWait)
  return { out, push }
}

afterEach(() => mock.timers.reset())

describe('createReorderer', () => {
  it('passes in-order events straight through', () => {
    const { out, push } = collect()
    push('a', { seq: 1, v: 'x' })
    push('a', { seq: 2, v: 'y' })
    assert.deepEqual(out, ['a:x', 'a:y'])
  })

  it('holds early events until the gap is filled', () => {
    const { out, push } = collect()
    push('a', { seq: 3, v: 'c' })
    push('a', { seq: 2, v: 'b' })
    assert.deepEqual(out, [])
    push('a', { seq: 1, v: 'a' })
    assert.deepEqual(out, ['a:a', 'a:b', 'a:c'])
  })

  it('keeps streams independent and drops duplicates', () => {
    const { out, push } = collect()
    push('a', { seq: 1, v: '1' })
    push('b', { seq: 1, v: '1' })
    push('a', { seq: 1, v: 'dup' })
    assert.deepEqual(out, ['a:1', 'b:1'])
  })

  it('flushes after a lost event instead of stalling', () => {
    mock.timers.enable({ apis: ['setTimeout'] })
    const { out, push } = collect(100)
    push('a', { seq: 1, v: '1' })
    push('a', { seq: 3, v: '3' })
    push('a', { seq: 4, v: '4' })
    assert.deepEqual(out, ['a:1'])
    mock.timers.tick(150)
    assert.deepEqual(out, ['a:1', 'a:3', 'a:4'])
    push('a', { seq: 5, v: '5' })
    assert.equal(out.at(-1), 'a:5')
  })

  it('delivers unnumbered events immediately', () => {
    const { out, push } = collect()
    push('a', { v: 'plain' })
    assert.deepEqual(out, ['a:plain'])
  })

  it('forget drops buffered events and pending timers, then starts afresh', () => {
    mock.timers.enable({ apis: ['setTimeout'] })
    const { out, push } = collect(100)
    push('a', { seq: 1, v: '1' })
    push('a', { seq: 3, v: '3' }) // waits for 2
    push('b', { seq: 2, v: 'b2' })
    push.forget('a')
    mock.timers.tick(150)
    assert.deepEqual(out, ['a:1', 'b:b2']) // b's gap still flushes; a's buffer is gone
    push('a', { seq: 1, v: 'again' }) // a new stream under the same name
    assert.equal(out.at(-1), 'a:again')
  })

  it('forget from inside apply stops draining that stream', () => {
    const out: string[] = []
    const push = createReorderer<Ev>((s, e) => {
      out.push(e.v)
      if (e.v === 'exit') push.forget(s)
    })
    push('a', { seq: 3, v: 'late' })
    push('a', { seq: 2, v: 'exit' })
    push('a', { seq: 1, v: 'first' })
    assert.deepEqual(out, ['first', 'exit'])
    push('a', { seq: 1, v: 'fresh' })
    assert.deepEqual(out, ['first', 'exit', 'fresh'])
  })
})
