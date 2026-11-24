import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { formatCompact, lastPoint, sparklineAreaPath, sparklineDots, sparklinePath, sparklinePoints, sparklineSummary } from './sparklineMath'

const box = { width: 100, height: 20, padding: 0 }

describe('sparklinePoints', () => {
  it('returns no segments for an empty array', () => {
    assert.deepEqual(sparklinePoints([], box), [])
  })
  it('returns no segments when every value is null', () => {
    assert.deepEqual(sparklinePoints([null, null], box), [])
  })
  it('puts a single value in the middle', () => {
    assert.deepEqual(sparklinePoints([5], box), [[{ x: 50, y: 10 }]])
  })
  it('spreads points evenly, the largest at the top', () => {
    assert.deepEqual(sparklinePoints([0, 10, 5], box), [
      [
        { x: 0, y: 20 },
        { x: 50, y: 0 },
        { x: 100, y: 10 },
      ],
    ])
  })
  it('draws a constant series as a centred flat line', () => {
    const [seg] = sparklinePoints([3, 3, 3], box)
    assert.deepEqual(
      seg.map((p) => p.y),
      [10, 10, 10],
    )
    assert.deepEqual(
      seg.map((p) => p.x),
      [0, 50, 100],
    )
  })
  it('keeps the padding on every side', () => {
    const [seg] = sparklinePoints([0, 1], { width: 20, height: 10, padding: 2 })
    assert.deepEqual(seg, [
      { x: 2, y: 8 },
      { x: 18, y: 2 },
    ])
  })
  it('breaks the line at a null in the middle', () => {
    const segs = sparklinePoints([0, 10, null, 10, 0], box)
    assert.equal(segs.length, 2)
    assert.deepEqual(
      segs[0].map((p) => p.x),
      [0, 25],
    )
    assert.deepEqual(
      segs[1].map((p) => p.x),
      [75, 100],
    )
  })
  it('skips nulls at both ends but keeps the x positions', () => {
    const segs = sparklinePoints([null, 0, 10, null], box)
    assert.equal(segs.length, 1)
    assert.deepEqual(
      segs[0].map((p) => p.x),
      [100 / 3, 200 / 3],
    )
  })
  it('treats NaN and Infinity as gaps', () => {
    const segs = sparklinePoints([1, Number.NaN, 2, Number.POSITIVE_INFINITY, 3], box)
    assert.equal(segs.length, 3)
  })
  it('clips values above a pinned max and below a pinned min', () => {
    const [seg] = sparklinePoints([-5, 5, 50], { ...box, min: 0, max: 10 })
    assert.deepEqual(
      seg.map((p) => p.y),
      [20, 10, 0],
    )
  })
  it('uses a pinned scale instead of the data range', () => {
    const [seg] = sparklinePoints([1, 2], { ...box, min: 0, max: 10 })
    assert.deepEqual(
      seg.map((p) => p.y),
      [18, 16],
    )
  })
  it('pins only one end and takes the other from the data', () => {
    const [seg] = sparklinePoints([5, 10], { ...box, min: 0 })
    assert.deepEqual(
      seg.map((p) => p.y),
      [10, 0],
    )
  })
  it('falls back to a flat line when min is not below max', () => {
    const [seg] = sparklinePoints([1, 2], { ...box, min: 5, max: 5 })
    assert.deepEqual(
      seg.map((p) => p.y),
      [10, 10],
    )
  })
  it('does not mutate its input', () => {
    const values = Object.freeze([1, null, 2])
    assert.equal(sparklinePoints(values, box)[0].length, 1)
  })
})

describe('lastPoint', () => {
  it('is undefined without points', () => {
    assert.equal(lastPoint([]), undefined)
  })
  it('is the last point of the last segment', () => {
    assert.deepEqual(lastPoint(sparklinePoints([0, null, 10], box)), { x: 100, y: 0 })
  })
})

describe('paths', () => {
  it('writes a polyline path per segment, leaving isolated points to the dots', () => {
    const segs = sparklinePoints([0, 10, null, 10], box)
    assert.equal(sparklinePath(segs), 'M0,20 L33.33,0')
  })
  it('is empty for no segments', () => {
    assert.equal(sparklinePath([]), '')
  })
  it('closes an area down to the baseline', () => {
    const segs = sparklinePoints([0, 10], box)
    assert.equal(sparklineAreaPath(segs, 20), 'M0,20 L100,0 L100,20 L0,20 Z')
  })
  it('gives a lone point no area', () => {
    assert.equal(sparklineAreaPath(sparklinePoints([4], box), 20), '')
  })
})

describe('sparklineDots', () => {
  it('is empty for no segments', () => {
    assert.deepEqual(sparklineDots([]), [])
  })
  it('marks isolated points and the last point', () => {
    const segs = sparklinePoints([0, 10, null, 5, null, 8], box)
    assert.deepEqual(
      sparklineDots(segs).map((p) => p.x),
      [60, 100],
    )
  })
  it('does not repeat a last point that is also isolated', () => {
    assert.equal(sparklineDots(sparklinePoints([4], box)).length, 1)
  })
  it('marks only the last point of a line that has no isolated points', () => {
    assert.equal(sparklineDots(sparklinePoints([1, 2, 3], box)).length, 1)
  })
})

describe('large and pinned series', () => {
  it('handles 200k values without throwing', () => {
    const values = Array.from({ length: 200_000 }, (_, i) => i % 100)
    const [seg] = sparklinePoints(values, box)
    assert.equal(seg.length, 200_000)
    assert.equal(sparklineSummary('Big', values), 'Big: 200000 values, last 99, min 0, max 99')
  })
  it('clips to the bottom when min is pinned above all the data', () => {
    const [seg] = sparklinePoints([1, 2, 3], { ...box, min: 10 })
    assert.deepEqual(
      seg.map((p) => p.y),
      [20, 20, 20],
    )
  })
  it('clips to the top when max is pinned below all the data', () => {
    const [seg] = sparklinePoints([5, 6], { ...box, max: 1 })
    assert.deepEqual(
      seg.map((p) => p.y),
      [0, 0],
    )
  })
})

describe('formatCompact', () => {
  it('keeps small numbers', () => {
    assert.equal(formatCompact(300), '300')
    assert.equal(formatCompact(0), '0')
    assert.equal(formatCompact(2.456), '2.46')
  })
  it('shortens thousands, millions and billions', () => {
    assert.equal(formatCompact(1200), '1.2k')
    assert.equal(formatCompact(1900), '1.9k')
    assert.equal(formatCompact(41_000_000), '41M')
    assert.equal(formatCompact(2_500_000_000), '2.5B')
  })
  it('rolls over at the k and M boundaries', () => {
    assert.equal(formatCompact(999_949), '999.9k')
    assert.equal(formatCompact(999_950), '1M')
    assert.equal(formatCompact(999_950_000), '1B')
    assert.equal(formatCompact(999.999), '1k')
    assert.equal(formatCompact(-999_950), '-1M')
  })
  it('keeps the sign', () => {
    assert.equal(formatCompact(-1500), '-1.5k')
  })
})

describe('sparklineSummary', () => {
  it('describes the series', () => {
    const text = sparklineSummary('Tokens per day', [300, 800, 1900, 700, 1000, 900, 1200])
    assert.equal(text, 'Tokens per day: 7 values, last 1.2k, min 300, max 1.9k')
  })
  it('says there is no data for an empty or all-null series', () => {
    assert.equal(sparklineSummary('Tokens', []), 'Tokens: no data')
    assert.equal(sparklineSummary('Tokens', [null]), 'Tokens: no data')
  })
  it('uses the singular for one value', () => {
    assert.equal(sparklineSummary('Duration', [42]), 'Duration: 1 value, last 42, min 42, max 42')
  })
  it('counts only real values and takes the last real one', () => {
    assert.equal(sparklineSummary('X', [1, null, 5, null]), 'X: 2 values, last 5, min 1, max 5')
  })
  it('uses the caller format', () => {
    assert.equal(
      sparklineSummary('Cost', [1, 2], (n) => `$${n}`),
      'Cost: 2 values, last $2, min $1, max $2',
    )
  })
})
