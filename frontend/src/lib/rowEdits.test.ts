import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  addRow,
  buildBatch,
  changeCount,
  displayValue,
  editability,
  isCellChanged,
  noEdits,
  setCell,
  toggleDelete,
  validateCell,
  type Target,
} from './rowEdits'

const col = (attnum: number, name: string, extra: Partial<Target['columns'][number]> = {}) => ({
  attnum, name, kind: 'text', notNull: false, hasDefault: false, primaryKey: false, editable: true, ...extra,
})

const target: Target = {
  tableOid: 99,
  table: 'items',
  key: ['id', 'line'],
  columns: [
    col(1, 'id', { primaryKey: true, kind: 'number', notNull: true }),
    col(2, 'line', { primaryKey: true, kind: 'number', notNull: true }),
    col(3, 'name'),
    col(4, 'doc', { kind: 'json' }),
  ],
}

// SELECT line, id, name, doc, 1 + 1 FROM items
const result = {
  columns: [{ name: 'line' }, { name: 'id' }, { name: 'name' }, { name: 'doc' }, { name: '?column?' }],
  attnums: [2, 1, 3, 4, 0],
  rows: [
    [1, 10, 'a', { x: 1 }, 2],
    [2, 10, 'b', null, 2],
  ],
}

describe('editability', () => {
  it('maps result columns to table columns', () => {
    const e = editability(result, target, false)
    assert.equal(e.editable, true)
    assert.deepEqual(e.columns.map((c) => c?.name ?? null), ['line', 'id', 'name', 'doc', null])
  })

  it('explains why a result is read-only', () => {
    assert.match(editability(result, target, true).reason, /read-only/)
    assert.match(editability(result, null, false).reason, /single table/)
    assert.match(editability(result, { ...target, key: [] }, false).reason, /no primary key/)
    const noLine = { ...result, attnums: [0, 1, 3, 4, 0] }
    assert.match(editability(noLine, target, false).reason, /line/)
  })
})

describe('staged edits', () => {
  it('stages, displays and undoes cell changes immutably', () => {
    const e1 = setCell(result, noEdits, 0, 2, 'renamed')
    assert.equal(noEdits.updates[0], undefined, 'noEdits untouched')
    assert.equal(displayValue(result, e1, 0, 2), 'renamed')
    assert.equal(isCellChanged(result, e1, 0, 2), true)
    assert.equal(changeCount(e1), 1)
    // Typing the original value back drops the change.
    const e2 = setCell(result, e1, 0, 2, 'a')
    assert.equal(changeCount(e2), 0)
    assert.deepEqual(e2.updates, {})
    // JSON originals compare by their text.
    assert.equal(changeCount(setCell(result, noEdits, 0, 3, '{"x":1}')), 0)
    // NULL is a value of its own.
    assert.equal(displayValue(result, setCell(result, noEdits, 0, 2, null), 0, 2), null)
  })

  it('adds, fills and drops new rows', () => {
    const e1 = addRow(noEdits)
    assert.equal(displayValue(result, e1, 2, 0), undefined, 'unset cells are DEFAULT')
    const e2 = setCell(result, e1, 2, 1, '11')
    assert.equal(displayValue(result, e2, 2, 1), '11')
    assert.equal(changeCount(e2), 1)
    assert.equal(changeCount(toggleDelete(result, e2, 2)), 0)
  })

  it('toggles deletion of existing rows', () => {
    const e1 = toggleDelete(result, setCell(result, noEdits, 1, 2, 'x'), 1)
    assert.equal(changeCount(e1), 1, 'a deleted row counts once even when edited')
    assert.equal(changeCount(toggleDelete(result, e1, 1)), 1, 'restored: the edit remains')
  })
})

describe('buildBatch', () => {
  it('identifies rows by their original key and names columns', () => {
    let e = setCell(result, noEdits, 1, 2, null)
    e = setCell(result, e, 1, 1, '12') // changing a key column: WHERE still uses the old value
    e = toggleDelete(result, setCell(result, e, 0, 2, 'gone anyway'), 0)
    e = setCell(result, addRow(e), 2, 3, '{"y": 2}')
    const batch = buildBatch(result, target, e)
    assert.equal(batch.tableOid, 99)
    assert.deepEqual(batch.edits, [
      { op: 'delete', key: { line: '1', id: '10' }, values: {} },
      { op: 'update', key: { line: '2', id: '10' }, values: { name: null, id: '12' } },
      { op: 'insert', key: {}, values: { doc: '{"y": 2}' } },
    ])
  })

  it('refuses expression columns and non-editable results', () => {
    const e = setCell(result, noEdits, 0, 4, '3')
    assert.throws(() => buildBatch(result, target, e), /\?column\?/)
    assert.throws(() => buildBatch(result, { ...target, key: [] }, noEdits), /no primary key/)
  })
})

describe('validateCell', () => {
  it('checks values by kind', () => {
    assert.equal(validateCell('number', ' -12.5e3 '), '')
    assert.notEqual(validateCell('number', '12abc'), '')
    assert.equal(validateCell('bool', 'TRUE'), '')
    assert.notEqual(validateCell('bool', 'maybe'), '')
    assert.equal(validateCell('json', '{"a": [1]}'), '')
    assert.match(validateCell('json', '{a}'), /Invalid JSON/)
    assert.equal(validateCell('text', 'anything'), '')
    assert.equal(validateCell('text', null), '')
    assert.match(validateCell('text', null, { name: 'id', notNull: true }), /can’t be NULL/)
  })
})
