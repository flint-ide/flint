import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  ariaSort,
  badgeSeverity,
  cellText,
  columnWidths,
  dateValue,
  formatDate,
  formatNumber,
  sortRows,
  tableToTSV,
  toggleSort,
  type TableColumn,
  type TableRow,
} from './viewTable'

const columns: TableColumn[] = [
  { id: 'key', title: 'Key', type: 'text', align: 'start' },
  { id: 'points', title: 'Points', type: 'number', align: 'end' },
  { id: 'updated', title: 'Updated', type: 'date', align: 'start' },
  { id: 'status', title: 'Status', type: 'badge', align: 'start', badges: { Done: 'ok', Blocked: 'error', Odd: 'purple' } },
]

const rows: TableRow[] = [
  { id: 'a', cells: ['ABC-10', 3, '2026-11-01T09:30:00Z', 'Done'] },
  { id: 'b', cells: ['abc-2', null, '2026-10-01', 'Blocked'] },
  { id: 'c', cells: ['ABC-1', 13, null, null] },
  { id: 'd', cells: [null, 3, '2026-11-01', 'Odd'] },
]

const ids = (order: number[]) => order.map((i) => rows[i].id).join('')

describe('table cells', () => {
  it('formats numbers with separators and at most four decimals', () => {
    assert.equal(formatNumber(1234567), '1,234,567')
    assert.equal(formatNumber(-1234.5), '-1,234.5')
    assert.equal(formatNumber(0.123456), '0.1235')
    assert.equal(formatNumber(12), '12')
    assert.equal(formatNumber(1e21), '1.000e+21')
    assert.equal(formatNumber(NaN), '')
  })

  it('reads ISO dates, with and without a time or a zone', () => {
    assert.equal(dateValue('2026-11-01'), Date.UTC(2026, 10, 1))
    assert.equal(dateValue('2026-11-01T09:30:00Z'), Date.UTC(2026, 10, 1, 9, 30))
    assert.equal(dateValue('2026-11-01T09:30:00'), Date.UTC(2026, 10, 1, 9, 30))
    assert.equal(dateValue('2026-11-01T09:30:00+02:00'), Date.UTC(2026, 10, 1, 7, 30))
    assert.ok(Number.isNaN(dateValue('yesterday')))
    assert.ok(Number.isNaN(dateValue(7)))
  })

  it('shows a date as written and a time to the minute', () => {
    assert.equal(formatDate('2026-11-01'), '2026-11-01')
    assert.equal(formatDate('2026-11-01T09:30:45Z', true), '2026-11-01 09:30')
    assert.equal(formatDate('nonsense'), '')
  })

  it('shows each cell by its column type, and nothing for a missing or mistyped value', () => {
    assert.equal(cellText(columns[0], '<b>x</b>'), '<b>x</b>')
    assert.equal(cellText(columns[1], 1500), '1,500')
    assert.equal(cellText(columns[1], '1500'), '')
    assert.equal(cellText(columns[2], '2026-11-01T09:30:00Z', true), '2026-11-01 09:30')
    assert.equal(cellText(columns[2], 5), '')
    assert.equal(cellText(columns[3], 'Done'), 'Done')
    assert.equal(cellText(columns[0], null), '')
    assert.equal(cellText(columns[0], { a: 1 }), '')
  })

  it('maps a badge to a severity only when the column says so', () => {
    assert.equal(badgeSeverity(columns[3], 'Done'), 'ok')
    assert.equal(badgeSeverity(columns[3], 'Blocked'), 'error')
    assert.equal(badgeSeverity(columns[3], 'Odd'), '')
    assert.equal(badgeSeverity(columns[3], 'Other'), '')
    assert.equal(badgeSeverity(columns[3], 'constructor'), '')
    assert.equal(badgeSeverity(columns[0], 'Done'), '')
  })
})

describe('table sorting', () => {
  it('keeps the extension’s order without a sort or for an unknown column', () => {
    assert.equal(ids(sortRows(columns, rows, null)), 'abcd')
    assert.equal(ids(sortRows(columns, rows, { column: 'nope' })), 'abcd')
  })

  it('sorts text without case and with numbers by value, missing last', () => {
    assert.equal(ids(sortRows(columns, rows, { column: 'key' })), 'cbad')
    assert.equal(ids(sortRows(columns, rows, { column: 'key', descending: true })), 'abcd')
  })

  it('sorts numbers by value, ties in the given order, missing last both ways', () => {
    assert.equal(ids(sortRows(columns, rows, { column: 'points' })), 'adcb')
    assert.equal(ids(sortRows(columns, rows, { column: 'points', descending: true })), 'cadb')
  })

  it('sorts dates by time, whatever their form', () => {
    assert.equal(ids(sortRows(columns, rows, { column: 'updated' })), 'bdac')
    assert.equal(ids(sortRows(columns, rows, { column: 'updated', descending: true })), 'adbc')
  })

  it('does not change the rows', () => {
    const before = JSON.stringify(rows)
    sortRows(columns, rows, { column: 'points', descending: true })
    assert.equal(JSON.stringify(rows), before)
  })

  it('cycles a header click through ascending, descending and the extension’s order', () => {
    const asc = toggleSort(null, 'key')
    assert.deepEqual(asc, { column: 'key', descending: false })
    const desc = toggleSort(asc, 'key')
    assert.deepEqual(desc, { column: 'key', descending: true })
    assert.equal(toggleSort(desc, 'key'), null)
    assert.deepEqual(toggleSort(desc, 'points'), { column: 'points', descending: false })
    assert.equal(ariaSort(asc, 'key'), 'ascending')
    assert.equal(ariaSort(desc, 'key'), 'descending')
    assert.equal(ariaSort(desc, 'points'), 'none')
    assert.equal(ariaSort(null, 'key'), 'none')
  })

  it('sorts 5,000 rows quickly', () => {
    const many: TableRow[] = Array.from({ length: 5000 }, (_, i) => ({ id: String(i), cells: [`row ${(i * 7919) % 5000}`, (i * 31) % 997, null, null] }))
    const start = performance.now()
    const order = sortRows(columns, many, { column: 'key' })
    assert.equal(order.length, 5000)
    assert.equal(many[order[0]].cells[0], 'row 0')
    assert.equal(many[order[4999]].cells[0], 'row 4999')
    assert.ok(performance.now() - start < 2000)
  })
})

describe('table layout and copying', () => {
  it('sizes columns from titles and cells, within bounds', () => {
    const w = columnWidths(columns, [...rows, { id: 'long', cells: ['x'.repeat(500), 1, null, null] }])
    assert.equal(w.length, 4)
    assert.equal(w[0], 360)
    assert.ok(w[1] >= 60 && w[1] < 120)
    assert.deepEqual(columnWidths(columns, []), columnWidths(columns, [{ id: 'e', cells: [] }]))
  })

  it('copies the shown rows as TSV, in the shown order', () => {
    const tsv = tableToTSV(columns, [{ id: 'x', cells: ['a\tb\nc', 1000, '2026-10-01', 'Done'] }, rows[2]], [1, 0])
    assert.equal(tsv, 'Key\tPoints\tUpdated\tStatus\nABC-1\t13\t\t\na b c\t1,000\t2026-10-01\tDone')
  })
})
