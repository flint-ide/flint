import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { breakpointsUnder, parseBreakpoints, toggleLine, withLines } from './breakpoints'

describe('breakpoints', () => {
  it('toggles lines, keeping them sorted, and drops empty files', () => {
    let map = toggleLine({}, '/p/a.go', 9)
    map = toggleLine(map, '/p/a.go', 3)
    assert.deepEqual(map, { '/p/a.go': [3, 9] })
    map = toggleLine(toggleLine(map, '/p/a.go', 3), '/p/a.go', 9)
    assert.deepEqual(map, {})
  })

  it('returns the same map when lines did not change', () => {
    const map = { '/p/a.go': [2, 5] }
    assert.equal(withLines(map, '/p/a.go', [5, 2, 5]), map)
    assert.deepEqual(withLines(map, '/p/a.go', [3, 6]), { '/p/a.go': [3, 6] })
  })

  it('parses saved breakpoints, ignoring bad entries', () => {
    assert.deepEqual(parseBreakpoints({ '/p/a.go': [4, 'x', 0, 4], 'rel.go': [1], '/p/b.go': 'no' }), { '/p/a.go': [4] })
    assert.deepEqual(parseBreakpoints(null), {})
    assert.deepEqual(parseBreakpoints([1]), {})
  })

  it('keeps one project when saving', () => {
    assert.deepEqual(breakpointsUnder({ '/p/a.go': [1], '/pp/b.go': [2], '/q/c.go': [3] }, '/p'), { '/p/a.go': [1] })
  })
})
