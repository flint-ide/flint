import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  applyView,
  compareValues,
  distinctValues,
  emptyView,
  excludeValue,
  isFiltered,
  isIdentity,
  isKeyShown,
  onlyValue,
  setFilter,
  setSort,
  toggleSort,
  valueKey,
  type GridView,
} from './gridView'

const rows = [
  ['b', 10, null],
  ['a', 2, 'x'],
  ['c', 2, 'y'],
  ['a', 33, null],
]

const view = (v: Partial<GridView>): GridView => ({ ...emptyView, ...v })

describe('applyView', () => {
  it('keeps every row in order without sort or filters', () => {
    assert.deepEqual(applyView(rows, emptyView), [0, 1, 2, 3])
    assert.ok(isIdentity(emptyView))
  })

  it('sorts numbers by value and keeps ties stable', () => {
    assert.deepEqual(applyView(rows, view({ sort: [{ col: 1, dir: 'asc' }] })), [1, 2, 0, 3])
    assert.deepEqual(applyView(rows, view({ sort: [{ col: 1, dir: 'desc' }] })), [3, 0, 1, 2])
  })

  it('puts NULLs last ascending and first descending', () => {
    assert.deepEqual(applyView(rows, view({ sort: [{ col: 2, dir: 'asc' }] })), [1, 2, 0, 3])
    assert.deepEqual(applyView(rows, view({ sort: [{ col: 2, dir: 'desc' }] })), [0, 3, 2, 1])
  })

  it('sorts by several keys', () => {
    const v = view({ sort: [{ col: 0, dir: 'asc' }, { col: 1, dir: 'desc' }] })
    assert.deepEqual(applyView(rows, v), [3, 1, 0, 2])
  })

  it('filters by value, keeping NULL apart from the text NULL', () => {
    assert.deepEqual(applyView(rows, onlyValue(emptyView, 0, 'a')), [1, 3])
    assert.deepEqual(applyView(rows, onlyValue(emptyView, 2, null)), [0, 3])
    assert.deepEqual(applyView([['NULL'], [null]], onlyValue(emptyView, 0, null)), [1])
  })

  it('excludes values and searches every cell', () => {
    assert.deepEqual(applyView(rows, excludeValue(excludeValue(emptyView, 0, 'a'), 0, 'c')), [0])
    assert.deepEqual(applyView(rows, view({ search: ' X ' })), [1])
    assert.deepEqual(applyView(rows, view({ search: 'null' })), [0, 3])
  })
})

describe('compareValues', () => {
  it('compares numeric text (big ints, decimals) by value', () => {
    assert.ok(compareValues('9007199254740993', '10') > 0)
    assert.ok(compareValues('1.5', '-2') > 0)
    assert.ok(compareValues('apple', 'banana') < 0)
  })
})

describe('distinctValues', () => {
  it('counts values most frequent first, among rows other filters keep', () => {
    assert.deepEqual(
      distinctValues(rows, 0, emptyView).map((d) => [d.label, d.count]),
      [['a', 2], ['b', 1], ['c', 1]],
    )
    const narrowed = onlyValue(emptyView, 1, 2)
    assert.deepEqual(distinctValues(rows, 0, narrowed).map((d) => d.label), ['a', 'c'])
    // The column's own filter doesn't hide its choices.
    assert.equal(distinctValues(rows, 0, onlyValue(emptyView, 0, 'b')).length, 3)
  })

  it('labels NULL and empty text', () => {
    const labels = distinctValues([[null], [''], ['']], 0, emptyView).map((d) => d.label)
    assert.deepEqual(labels, ['(empty)', 'NULL'])
  })
})

describe('sorting state', () => {
  it('cycles a header asc → desc → off', () => {
    const a = toggleSort(emptyView, 1, false)
    assert.deepEqual(a.sort, [{ col: 1, dir: 'asc' }])
    const d = toggleSort(a, 1, false)
    assert.deepEqual(d.sort, [{ col: 1, dir: 'desc' }])
    assert.deepEqual(toggleSort(d, 1, false).sort, [])
  })

  it('adds secondary keys with Shift and replaces them without', () => {
    const two = toggleSort(toggleSort(emptyView, 0, false), 2, true)
    assert.deepEqual(two.sort, [{ col: 0, dir: 'asc' }, { col: 2, dir: 'asc' }])
    assert.deepEqual(toggleSort(two, 0, true).sort, [{ col: 0, dir: 'desc' }, { col: 2, dir: 'asc' }])
    assert.deepEqual(toggleSort(two, 1, false).sort, [{ col: 1, dir: 'asc' }])
    assert.deepEqual(setSort(two, 2, null).sort, [])
  })
})

describe('filter state', () => {
  it('removes an emptied exclusion and narrows an inclusion', () => {
    assert.deepEqual(setFilter(view({ filters: { 0: { mode: 'in', keys: ['a'] } } }), 0, null).filters, {})
    assert.deepEqual(setFilter(emptyView, 0, { mode: 'notIn', keys: [] }).filters, {})
    const f = excludeValue(view({ filters: { 0: { mode: 'in', keys: ['a', 'b'] } } }), 0, 'a').filters[0]
    assert.deepEqual(f, { mode: 'in', keys: ['b'] })
    assert.ok(isFiltered(view({ filters: { 0: f } })))
  })

  it('tells which keys a filter shows', () => {
    assert.ok(isKeyShown(undefined, 'a'))
    assert.ok(isKeyShown({ mode: 'in', keys: ['a'] }, 'a'))
    assert.ok(!isKeyShown({ mode: 'notIn', keys: [valueKey(null)] }, valueKey(null)))
  })
})
