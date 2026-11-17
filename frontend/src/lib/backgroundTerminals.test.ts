import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { backgroundTerminalLabel, restoreMode, unclaimedNotice, unclaimedTerminals } from './backgroundTerminals'

describe('restoreMode', () => {
  const live = new Set(['a', 'b'])
  const table: [string, string | undefined, string[], 'attach' | 'respawn'][] = [
    ['live, unclaimed', 'a', [], 'attach'],
    ['not live (setting off, host gone)', 'z', [], 'respawn'],
    ['saved before ids were kept', undefined, [], 'respawn'],
    ['empty id', '', [], 'respawn'],
    ['already claimed by another tab', 'a', ['a'], 'respawn'],
    ['another id claimed', 'b', ['a'], 'attach'],
  ]
  for (const [name, id, claimed, want] of table) {
    it(name, () => assert.equal(restoreMode(id, live, new Set(claimed)), want))
  }
  it('respawns everything when nothing is live', () => assert.equal(restoreMode('a', new Set(), new Set()), 'respawn'))
})

describe('unclaimedTerminals', () => {
  const live = [
    { id: 'a', running: true },
    { id: 'b', running: false },
    { id: 'c', running: true },
  ]
  it('lists what no tab shows, in order', () => {
    assert.deepEqual(unclaimedTerminals(live, ['b', 'x']), [live[0], live[2]])
    assert.deepEqual(unclaimedTerminals(live, new Set(['a', 'b', 'c'])), [])
    assert.deepEqual(unclaimedTerminals([], ['a']), [])
  })
})

describe('labels', () => {
  it('names a terminal by position, short id and state', () => {
    assert.equal(backgroundTerminalLabel({ id: '0123456789abcdef', running: true }, 0), 'Terminal 1 (01234567) — running')
    assert.equal(backgroundTerminalLabel({ id: 'x', running: false }, 2), 'Terminal 3 (x) — ended')
  })
  it('tells about unclaimed terminals only when there are some', () => {
    assert.equal(unclaimedNotice(0), '')
    assert.match(unclaimedNotice(1), /^A background terminal is/)
    assert.match(unclaimedNotice(3), /^3 background terminals are/)
  })
})
