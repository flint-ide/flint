import { describe, expect, it } from 'vitest'
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
    expect(cellText({ a: 1 })).toBe('{"a":1}')
    expect(cellText(null)).toBe('')
    expect(cellText(42)).toBe('42')
  })
})

describe('toTSV', () => {
  it('writes header, NULL and escapes control characters', () => {
    expect(toTSV(result).split('\n')).toEqual(['id\tpayload', '1\t{"a":1}', '2\tNULL', '3\tline\\nbreak\\there'])
  })
})

describe('columnWidths', () => {
  it('stays within bounds', () => {
    const widths = columnWidths({ columns: result.columns, rows: [[1, 'x'.repeat(1000)]] })
    expect(widths[0]).toBeGreaterThanOrEqual(60)
    expect(widths[1]).toBe(420)
  })
})

describe('formatDuration', () => {
  it('picks a sensible unit', () => {
    expect(formatDuration(12)).toBe('12 ms')
    expect(formatDuration(1500)).toBe('1.50 s')
    expect(formatDuration(125_000)).toBe('2m 5s')
  })
})
