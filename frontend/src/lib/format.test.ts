import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { cellText, columnWidths, formatDuration, toTSV } from './format'

const result = {
  columns: [
    { name: 'id', type: 'int8' },
    { name: 'payload', type: 'jsonb' },
  ],
  rows: [
    [1, { a: 1 }],
    [2, null],
    [3, 'line\nbreak\there'],
  ],
}

describe('cellText', () => {
  it('stringifies objects and handles null', () => {
    assert.equal(cellText({ a: 1 }), '{"a":1}')
    assert.equal(cellText(null), '')
    assert.equal(cellText(42), '42')
  })
})

describe('toTSV', () => {
  it('writes header, NULL and escapes control characters', () => {
    assert.deepEqual(toTSV(result).split('\n'), ['id\tpayload', '1\t{"a":1}', '2\tNULL', '3\tline\\nbreak\\there'])
  })
})

describe('columnWidths', () => {
  it('stays within bounds', () => {
    const widths = columnWidths({ columns: result.columns, rows: [[1, 'x'.repeat(1000)]] })
    assert.ok((widths[0]) >= 60)
    assert.equal(widths[1], 420)
  })
})

describe('formatDuration', () => {
  it('picks a sensible unit', () => {
    assert.equal(formatDuration(12), '12 ms')
    assert.equal(formatDuration(1500), '1.50 s')
    assert.equal(formatDuration(125_000), '2m 5s')
  })
})
