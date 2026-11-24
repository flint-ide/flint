import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { contextPercent, formatShare, formatTokens, freeShare, usageBreakdown, hasLimit, partShares, ringGeometry, usageText } from './contextMeterMath'

describe('hasLimit', () => {
  it('is false for null, undefined, zero, negative and NaN', () => {
    for (const limit of [null, undefined, 0, -5, Number.NaN, Number.POSITIVE_INFINITY]) assert.equal(hasLimit(limit), false)
  })
  it('is true for a positive limit', () => {
    assert.equal(hasLimit(200_000), true)
  })
})

describe('contextPercent', () => {
  it('rounds the share of the limit', () => {
    assert.equal(contextPercent(124_000, 200_000), 62)
    assert.equal(contextPercent(1, 3), 33)
    assert.equal(contextPercent(2, 3), 67)
  })
  it('is 0 without a usable limit', () => {
    assert.equal(contextPercent(100, null), 0)
    assert.equal(contextPercent(100, 0), 0)
  })
  it('clamps to 0..100', () => {
    assert.equal(contextPercent(300_000, 200_000), 100)
    assert.equal(contextPercent(-10, 200_000), 0)
  })
  it('treats NaN and infinite usage as 0', () => {
    assert.equal(contextPercent(Number.NaN, 200_000), 0)
    assert.equal(contextPercent(Number.POSITIVE_INFINITY, 200_000), 0)
  })
})

describe('formatTokens', () => {
  it('prints small counts as they are', () => {
    assert.equal(formatTokens(0), '0')
    assert.equal(formatTokens(980), '980')
    assert.equal(formatTokens(999), '999')
  })
  it('uses k from 1000 with one trimmed decimal', () => {
    assert.equal(formatTokens(1000), '1k')
    assert.equal(formatTokens(12_400), '12.4k')
    assert.equal(formatTokens(124_000), '124k')
    assert.equal(formatTokens(200_000), '200k')
  })
  it('rolls over to M instead of printing 1000k', () => {
    assert.equal(formatTokens(999_949), '999.9k')
    assert.equal(formatTokens(999_950), '1M')
    assert.equal(formatTokens(1_000_000), '1M')
    assert.equal(formatTokens(1_200_000), '1.2M')
    assert.equal(formatTokens(12_000_000), '12M')
  })
  it('rounds fractions and guards bad input', () => {
    assert.equal(formatTokens(980.6), '981')
    assert.equal(formatTokens(-4), '0')
    assert.equal(formatTokens(Number.NaN), '0')
  })
})

describe('usageText', () => {
  it('joins percent and tokens', () => {
    assert.equal(usageText(124_000, 200_000), '62%, 124k of 200k tokens')
    assert.equal(usageText(124_000, 200_000, ' · '), '62% · 124k of 200k tokens')
  })
  it('shows the true usage above the limit, with the percent clamped', () => {
    assert.equal(usageText(250_000, 200_000), '100%, 250k of 200k tokens')
  })
})

describe('partShares', () => {
  const parts = [
    { id: 'sys', label: 'System prompt', tokens: 20_000 },
    { id: 'tools', label: 'Tools', tokens: 30_000 },
  ]
  it('gives each part its share of the limit, in order', () => {
    const shares = partShares(parts, 200_000)
    assert.deepEqual(
      shares.map((s) => [s.id, s.share]),
      [
        ['sys', 10],
        ['tools', 15],
      ],
    )
  })
  it('keeps the total at 100 % when the parts exceed the limit', () => {
    const shares = partShares(parts, 25_000)
    const total = shares.reduce((sum, s) => sum + s.share, 0)
    assert.ok(Math.abs(total - 100) < 1e-9)
    assert.ok(shares[1].share > shares[0].share)
  })
  it('returns no shares without a limit', () => {
    assert.deepEqual(partShares(parts, null), [])
    assert.deepEqual(partShares(parts, 0), [])
  })
  it('treats negative and NaN tokens as 0 and keeps the originals untouched', () => {
    const bad = [
      { id: 'a', label: 'A', tokens: -5 },
      { id: 'b', label: 'B', tokens: Number.NaN },
    ]
    const shares = partShares(bad, 100)
    assert.deepEqual(
      shares.map((s) => s.share),
      [0, 0],
    )
    assert.equal(bad[0].tokens, -5)
  })
})

describe('ringGeometry', () => {
  it('draws the arc as a share of the circumference', () => {
    const ring = ringGeometry(16, 50, 2)
    assert.equal(ring.radius, 7)
    assert.equal(ring.center, 8)
    assert.ok(Math.abs(ring.circumference - 2 * Math.PI * 7) < 1e-9)
    assert.ok(Math.abs(ring.filled - ring.circumference / 2) < 1e-9)
    assert.equal(ring.dashArray, `${ring.filled} ${ring.circumference}`)
  })
  it('is empty at 0 and full at 100, and clamps beyond', () => {
    assert.equal(ringGeometry(16, 0, 2).filled, 0)
    const full = ringGeometry(16, 100, 2)
    assert.equal(full.filled, full.circumference)
    assert.equal(ringGeometry(16, 150, 2).filled, full.circumference)
    assert.equal(ringGeometry(16, -3, 2).filled, 0)
    assert.equal(ringGeometry(16, Number.NaN, 2).filled, 0)
  })
  it('never gets a negative radius for a tiny size', () => {
    assert.equal(ringGeometry(2, 50, 4).radius, 0)
  })
})

describe('formatShare', () => {
  it('rounds, and marks slivers', () => {
    assert.equal(formatShare(62.4), '62%')
    assert.equal(formatShare(0.3), '<1%')
    assert.equal(formatShare(0), '0%')
    assert.equal(formatShare(140), '100%')
    assert.equal(formatShare(Number.NaN), '0%')
  })
})

describe('freeShare', () => {
  it('is what the parts leave, never negative', () => {
    assert.equal(freeShare(partShares([{ id: 'a', label: 'A', tokens: 50 }], 200)), 75)
    assert.equal(freeShare(partShares([{ id: 'a', label: 'A', tokens: 500 }], 200)), 0)
    assert.equal(freeShare([]), 100)
  })
})

describe('float drift', () => {
  it('snaps a share that scaling left at ~0 to exactly 0', () => {
    const thirds = ['a', 'b', 'c'].map((id) => ({ id, label: id, tokens: 100_000 }))
    const shares = partShares(thirds, 200_000)
    assert.equal(freeShare(shares), 0)
    assert.equal(formatShare(freeShare(shares)), '0%')
  })
})

describe('usageBreakdown', () => {
  const parts = [
    { id: 'a', label: 'A', tokens: 20_000 },
    { id: 'b', label: 'B', tokens: 40_000 },
  ]
  it('puts used tokens the parts do not explain into other, so bar and ring agree', () => {
    const b = usageBreakdown(parts, 150_000, 200_000)
    assert.equal(b.other, 45)
    assert.equal(b.otherTokens, 90_000)
    assert.equal(b.free, 25)
    assert.equal(100 - b.free, contextPercent(150_000, 200_000))
  })
  it('has no other when the parts account for the usage', () => {
    const b = usageBreakdown(parts, 60_000, 200_000)
    assert.equal(b.other, 0)
    assert.equal(b.free, 70)
  })
  it('leaves free alone when the parts exceed the usage', () => {
    const b = usageBreakdown(parts, 10_000, 200_000)
    assert.equal(b.other, 0)
    assert.equal(b.free, 70)
  })
  it('has no free and no other when full', () => {
    const b = usageBreakdown(parts, 300_000, 200_000)
    assert.equal(b.free, 0)
    assert.equal(b.other, 70)
    assert.equal(b.shares.length, 2)
  })
  it('is empty without a limit', () => {
    const b = usageBreakdown(parts, 100, null)
    assert.deepEqual(b.shares, [])
    assert.equal(b.free, 100)
    assert.equal(b.other, 0)
  })
})
