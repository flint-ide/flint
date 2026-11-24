import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { durationScale, durationShares, durationValueText, formatDuration } from './duration'

describe('durationScale', () => {
  it('is the larger of value and estimate when total is missing', () => {
    assert.equal(durationScale({ value: 3000, estimate: 8000 }), 8000)
    assert.equal(durationScale({ value: 9000, estimate: 8000 }), 9000)
  })
  it('is the total when given', () => {
    assert.equal(durationScale({ value: 3000, total: 2000 }), 2000)
  })
  it('is zero for nothing, and ignores a NaN total', () => {
    assert.equal(durationScale({ value: 0 }), 0)
    assert.equal(durationScale({ value: 5, total: Number.NaN }), 5)
  })
})

describe('durationValueText', () => {
  it('is the value alone without an estimate', () => {
    assert.equal(durationValueText({ value: 850 }), '850ms')
  })
  it('names the estimate and flags an overrun', () => {
    assert.equal(durationValueText({ value: 12400, estimate: 10000 }), '12.4s of 10.0s estimated, over')
  })
  it('names the estimate without the flag when under it', () => {
    assert.equal(durationValueText({ value: 4000, estimate: 10000 }), '4.0s of 10.0s estimated')
  })
})

describe('durationShares', () => {
  it('is empty for zero', () => {
    assert.deepEqual(durationShares({ value: 0 }), { fillPercent: 0, estimatePercent: undefined, over: false })
  })
  it('fills the whole track against the larger of value and estimate by default', () => {
    assert.deepEqual(durationShares({ value: 4000, estimate: 8000 }), { fillPercent: 50, estimatePercent: 100, over: false })
  })
  it('has no tick and is never over when the estimate is missing', () => {
    assert.deepEqual(durationShares({ value: 3000, total: 6000 }), { fillPercent: 50, estimatePercent: undefined, over: false })
  })
  it('treats a zero or negative estimate as missing', () => {
    assert.equal(durationShares({ value: 3000, estimate: 0, total: 6000 }).estimatePercent, undefined)
    assert.equal(durationShares({ value: 3000, estimate: -1, total: 6000 }).over, false)
  })
  it('marks over when the value is above the estimate and places the tick', () => {
    assert.deepEqual(durationShares({ value: 12000, estimate: 10000, total: 20000 }), {
      fillPercent: 60,
      estimatePercent: 50,
      over: true,
    })
  })
  it('is not over when the value equals the estimate', () => {
    assert.equal(durationShares({ value: 10000, estimate: 10000 }).over, false)
  })
  it('clamps a value beyond total to a full track', () => {
    assert.deepEqual(durationShares({ value: 30000, estimate: 10000, total: 20000 }), {
      fillPercent: 100,
      estimatePercent: 50,
      over: true,
    })
  })
  it('clamps an estimate beyond total to the end of the track', () => {
    assert.equal(durationShares({ value: 1000, estimate: 50000, total: 20000 }).estimatePercent, 100)
  })
  it('treats negative input as zero', () => {
    assert.deepEqual(durationShares({ value: -500, estimate: 1000 }), { fillPercent: 0, estimatePercent: 100, over: false })
    assert.deepEqual(durationShares({ value: 500, estimate: -1000, total: 1000 }), {
      fillPercent: 50,
      estimatePercent: undefined,
      over: false,
    })
  })
  it('treats NaN and infinite input as zero or missing', () => {
    assert.deepEqual(durationShares({ value: Number.NaN, estimate: 1000 }), { fillPercent: 0, estimatePercent: 100, over: false })
    assert.deepEqual(durationShares({ value: 500, estimate: Number.NaN, total: 1000 }), {
      fillPercent: 50,
      estimatePercent: undefined,
      over: false,
    })
    assert.equal(durationShares({ value: 500, total: Number.POSITIVE_INFINITY }).fillPercent, 100)
  })
  it('is all zero when total is zero', () => {
    assert.deepEqual(durationShares({ value: 0, total: 0 }), { fillPercent: 0, estimatePercent: undefined, over: false })
  })
})

describe('formatDuration', () => {
  it('shows milliseconds under a second', () => {
    assert.equal(formatDuration(850), '850ms')
    assert.equal(formatDuration(0), '0ms')
  })
  it('shows seconds with one decimal under a minute', () => {
    assert.equal(formatDuration(12400), '12.4s')
    assert.equal(formatDuration(1000), '1.0s')
  })
  it('shows minutes and zero-padded seconds under an hour', () => {
    assert.equal(formatDuration(185000), '3m 05s')
    assert.equal(formatDuration(60000), '1m 00s')
  })
  it('shows hours and zero-padded minutes', () => {
    assert.equal(formatDuration(3720000), '1h 02m')
  })
  it('rolls over to the next unit instead of showing a full one', () => {
    assert.equal(formatDuration(999.6), '1.0s')
    assert.equal(formatDuration(59_949), '59.9s')
    assert.equal(formatDuration(59_999), '1m 00s')
    assert.equal(formatDuration(3_599_400), '59m 59s')
    assert.equal(formatDuration(3_599_999), '1h 00m')
  })
  it('treats negative and NaN input as zero', () => {
    assert.equal(formatDuration(-5), '0ms')
    assert.equal(formatDuration(Number.NaN), '0ms')
  })
})
