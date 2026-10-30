import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { historyKindLabel, relativeTime, sqlPreview } from './queryHistory'

describe('relativeTime', () => {
  const now = new Date(2026, 9, 30, 15, 30).getTime()

  it('uses relative words for recent runs and dates for older ones', () => {
    assert.equal(relativeTime(now - 10_000, now), 'just now')
    assert.equal(relativeTime(now - 5 * 60_000, now), '5 min ago')
    assert.equal(relativeTime(now - 3 * 3_600_000, now), '3 h ago')
    assert.equal(relativeTime(new Date(2026, 9, 29, 14, 5).getTime(), now), 'yesterday 14:05')
    assert.equal(relativeTime(new Date(2024, 2, 1, 9, 12).getTime(), now), '2024-03-01 09:12')
    assert.equal(relativeTime(now + 5000, now), 'just now', 'clock skew is not negative')
  })
})

describe('historyKindLabel', () => {
  it('labels non-console entries per driver', () => {
    assert.equal(historyKindLabel('', 'postgres'), '')
    assert.equal(historyKindLabel('explain', 'postgres'), 'EXPLAIN')
    assert.equal(historyKindLabel('explain-analyze', 'postgres'), 'ANALYZE')
    assert.equal(historyKindLabel('explain-analyze', 'clickhouse'), 'PIPELINE')
    assert.equal(historyKindLabel('edit', 'postgres'), 'EDIT')
  })
})

describe('sqlPreview', () => {
  it('flattens whitespace and cuts long statements', () => {
    assert.equal(sqlPreview('SELECT *\n  FROM t\n'), 'SELECT * FROM t')
    assert.equal(sqlPreview('x'.repeat(10), 5), 'xxxx…')
  })
})
