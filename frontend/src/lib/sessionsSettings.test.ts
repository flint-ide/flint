import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceSessions, DEFAULT_SESSIONS } from './sessionsSettings'

describe('coerceSessions', () => {
  it('is off by default and fills in what is missing', () => {
    assert.deepEqual(DEFAULT_SESSIONS, { background: false, replayKiB: 1024 })
    assert.deepEqual(coerceSessions(undefined), DEFAULT_SESSIONS)
    assert.deepEqual(coerceSessions({ background: true }), { background: true, replayKiB: 1024 })
  })

  it('clamps the replay size like Go', () => {
    const table: [unknown, number][] = [
      [1, 64],
      [100000, 8192],
      [2048, 2048],
      [0, 1024],
      ['big', 1024],
      [Number.NaN, 1024],
      [100.6, 101],
    ]
    for (const [replayKiB, want] of table) assert.equal(coerceSessions({ replayKiB }).replayKiB, want, String(replayKiB))
    assert.equal(coerceSessions({ background: 'yes' }).background, false)
  })
})
