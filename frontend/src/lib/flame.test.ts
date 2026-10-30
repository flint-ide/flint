import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  fitLabel, formatValue, frameAt, hash01, hitTest, isRuntimeFrame, layoutDepth, layoutFlame, mixHex, packageOf, percent,
  searchFrames, shortName, sortTop, type FlameFrame,
} from './flame'

const f = (name: string, total: number, children: FlameFrame[] = []): FlameFrame => ({
  name,
  total,
  self: total - children.reduce((s, c) => s + c.total, 0),
  children: children.length ? children : undefined,
})

// root 100 = main 100 = (a 60 = b 40) + (c 30) + 10 self
const tree = f('root', 100, [f('main', 100, [f('a', 60, [f('b', 40)]), f('c', 30)])])

const brief = (rects: ReturnType<typeof layoutFlame>) => rects.map((r) => `${r.frame.name}@${r.depth}:${r.x}+${r.w}${r.ancestor ? '^' : ''}`)

describe('layoutFlame', () => {
  it('places children from the parent left edge in proportion to their totals', () => {
    assert.deepEqual(brief(layoutFlame(tree, [], 1000)), ['root@0:0+1000', 'main@1:0+1000', 'a@2:0+600', 'b@3:0+400', 'c@2:600+300'])
  })

  it('zooms into a frame: ancestors full width, the frame spans the width', () => {
    const rects = layoutFlame(tree, [0, 0], 1000)
    assert.deepEqual(brief(rects), ['root@0:0+1000^', 'main@1:0+1000^', 'a@2:0+1000', 'b@3:0+666.6666666666667'])
    assert.deepEqual(rects[3].path, [0, 0, 0])
    assert.equal(layoutDepth(rects), 4)
  })

  it('falls back to the root when the zoom path no longer exists', () => {
    assert.deepEqual(brief(layoutFlame(tree, [5, 1], 100)).slice(0, 2), ['root@0:0+100', 'main@1:0+100'])
  })

  it('skips frames narrower than the minimum, with their children', () => {
    const wide = f('root', 2000, [f('big', 1999), f('tiny', 1, [f('tinier', 1)])])
    assert.deepEqual(brief(layoutFlame(wide, [], 500)), ['root@0:0+500', 'big@1:0+499.75'])
  })

  it('handles an empty profile and zero width', () => {
    assert.deepEqual(brief(layoutFlame(f('root', 0), [], 500)), [])
    assert.deepEqual(layoutFlame(tree, [], 0), [])
  })
})

describe('hitTest and frameAt', () => {
  const rects = layoutFlame(tree, [], 1000)
  it('finds the frame under a point by row and x', () => {
    assert.equal(hitTest(rects, 10, 5, 18)?.frame.name, 'root')
    assert.equal(hitTest(rects, 650, 2 * 18 + 3, 18)?.frame.name, 'c')
    assert.equal(hitTest(rects, 950, 2 * 18 + 3, 18), null) // main's self time
    assert.equal(hitTest(rects, 10, -1, 18), null)
  })
  it('resolves paths', () => {
    assert.equal(frameAt(tree, [0, 1])?.name, 'c')
    assert.equal(frameAt(tree, [0, 9]), null)
  })
})

describe('searchFrames', () => {
  it('matches names case-insensitively and counts nested matches once', () => {
    const rec = f('root', 50, [f('Rec', 50, [f('rec', 30, [f('leaf', 30)])])])
    const r = searchFrames(rec, 'REC')
    assert.equal(r.matched.size, 2)
    assert.equal(r.total, 50)
    assert.equal(searchFrames(rec, '  ').matched.size, 0)
  })
})

describe('formatting', () => {
  it('formats values by unit', () => {
    assert.equal(formatValue(1_500_000_000, 'nanoseconds'), '1.5 s')
    assert.equal(formatValue(12_340_000, 'nanoseconds'), '12.3 ms')
    assert.equal(formatValue(250, 'nanoseconds'), '250 ns')
    assert.equal(formatValue(430_000_000, 'nanoseconds'), '430 ms')
    assert.equal(formatValue(100_000_000, 'nanoseconds'), '100 ms')
    assert.equal(formatValue(20_000_000, 'nanoseconds'), '20 ms')
    assert.equal(formatValue(2_000_000_000, 'nanoseconds'), '2 s')
    assert.equal(formatValue(512, 'bytes'), '512 B')
    assert.equal(formatValue(3 * 1024 * 1024, 'bytes'), '3 MB')
    assert.equal(formatValue(42, 'count'), '42')
    assert.equal(formatValue(123_456, 'count'), '123k')
    assert.equal(percent(1, 3), '33.3%')
    assert.equal(percent(1, 300), '0.33%')
    assert.equal(percent(5, 0), '0%')
  })

  it('shortens Go names', () => {
    assert.equal(shortName('flint/internal/profiling.(*node).child'), 'profiling.(*node).child')
    assert.equal(shortName('main.main'), 'main.main')
    assert.equal(packageOf('github.com/a/b.F.func1'), 'github.com/a/b')
    assert.equal(packageOf('runtime.mallocgc'), 'runtime')
    assert.ok(isRuntimeFrame('runtime.mallocgc'))
    assert.ok(isRuntimeFrame('internal/poll.(*FD).Read'))
    assert.ok(!isRuntimeFrame('flint/internal/x.F'))
    assert.equal(fitLabel('abcdef', 4), 'abc…')
    assert.equal(fitLabel('abc', 4), 'abc')
    assert.equal(fitLabel('abc', 1), '')
  })

  it('mixes colors and hashes stably', () => {
    assert.equal(mixHex('#000000', '#ffffff', 0.5), '#808080')
    assert.equal(mixHex('#102030', '#ffffff', 0), '#102030')
    assert.equal(mixHex('nope', '#ffffff', 1), 'nope')
    assert.equal(hash01('pkg'), hash01('pkg'))
    assert.ok(hash01('a') >= 0 && hash01('a') <= 1)
  })
})

describe('sortTop', () => {
  const rows = [
    { name: 'b', flat: 5, cum: 10 },
    { name: 'a', flat: 5, cum: 20 },
    { name: 'c', flat: 1, cum: 50 },
  ]
  it('sorts by flat, cum or name without mutating', () => {
    assert.deepEqual(sortTop(rows, 'flat').map((r) => r.name), ['a', 'b', 'c'])
    assert.deepEqual(sortTop(rows, 'cum').map((r) => r.name), ['c', 'a', 'b'])
    assert.deepEqual(sortTop(rows, 'name').map((r) => r.name), ['a', 'b', 'c'])
    assert.equal(rows[0].name, 'b')
  })
})
