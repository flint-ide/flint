import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { createDraftReporter, draftFlags, type Timers } from './draftReport'

/** Manual clock: timers run when advance() passes their time. */
function fakeClock() {
  let now = 0
  let next = 1
  const jobs = new Map<number, { at: number; fn: () => void }>()
  const timers: Timers = {
    set: (fn, ms) => {
      jobs.set(next, { at: now + ms, fn })
      return next++
    },
    clear: (h) => void jobs.delete(h as number),
  }
  const advance = (ms: number) => {
    now += ms
    for (const [id, j] of [...jobs]) {
      if (j.at <= now) {
        jobs.delete(id)
        j.fn()
      }
    }
  }
  return { timers, advance }
}

describe('draftFlags', () => {
  it('flags non-blank drafts and leaves dormant chats out', () => {
    assert.deepEqual(
      draftFlags({ a: { draft: 'hi' }, b: { draft: '  \n' }, c: { draft: 'x', dormant: true }, d: { draft: '' } }),
      { a: true, b: false, d: false },
    )
  })
})

describe('createDraftReporter', () => {
  const setup = () => {
    const clock = fakeClock()
    const sent: Array<[string, boolean]> = []
    const r = createDraftReporter((id, open) => sent.push([id, open]), 300, clock.timers)
    return { ...clock, sent, r }
  }

  it('reports a draft opening and closing once each, after the delay', () => {
    const { r, advance, sent } = setup()
    r.update({ a: false })
    advance(1000)
    assert.deepEqual(sent, [], 'an empty draft is the default: nothing to say')
    r.update({ a: true })
    advance(299)
    assert.deepEqual(sent, [])
    advance(1)
    assert.deepEqual(sent, [['a', true]])
    r.update({ a: true })
    advance(1000)
    assert.equal(sent.length, 1, 'no repeat while it stays open')
    r.update({ a: false })
    advance(300)
    assert.deepEqual(sent, [['a', true], ['a', false]])
  })

  it('sends nothing for a draft that was sent and cleared within the delay', () => {
    const { r, advance, sent } = setup()
    r.update({ a: true })
    advance(100)
    r.update({ a: false })
    advance(1000)
    assert.deepEqual(sent, [])
  })

  it('keeps the timer running while unrelated updates arrive', () => {
    const { r, advance, sent } = setup()
    r.update({ a: true })
    for (let i = 0; i < 5; i++) {
      advance(100)
      r.update({ a: true })
    }
    assert.deepEqual(sent, [['a', true]])
  })

  it('forgets a closed chat and its pending report', () => {
    const { r, advance, sent } = setup()
    r.update({ a: true })
    r.update({})
    advance(1000)
    assert.deepEqual(sent, [])
    r.update({ a: true }) // an id may come back (a new chat): reported afresh
    advance(300)
    assert.deepEqual(sent, [['a', true]])
  })
})
