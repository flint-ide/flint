import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { DropdownItem } from '../../../components/ui/Dropdown'
import { emptyView, onlyValue, type GridView } from '../lib/gridView'
import { cellMenu, headerMenu, shortValue, type GridMenuActions } from './gridMenu'

type Item = Extract<DropdownItem, { onSelect: unknown }>
const items = (menu: DropdownItem[]): Item[] => menu.filter((i): i is Item => 'onSelect' in i)
const labels = (menu: DropdownItem[]) => items(menu).map((i) => i.label)
const pick = (menu: DropdownItem[], label: string) => {
  const item = items(menu).find((i) => i.label === label)
  assert.ok(item, `no "${label}" in ${labels(menu).join(', ')}`)
  item.onSelect({ altKey: false, metaKey: false })
}

function recorder() {
  const views: GridView[] = []
  const calls: string[] = []
  const actions: GridMenuActions = {
    setView: (v) => views.push(v),
    openFilter: (col) => calls.push(`filter:${col}`),
    copy: (text, what) => calls.push(`${what}:${text}`),
  }
  return { views, calls, actions }
}

const copies = { row: () => 'r', rowJSON: () => 'j', rowInsert: () => 'i', column: () => 'c', columnIn: () => 'in' }

describe('cellMenu', () => {
  it('filters by and excludes the clicked value', () => {
    const { views, actions } = recorder()
    const menu = cellMenu({ view: emptyView, col: 1, columnName: 'status', value: 'paid', copies }, actions)
    pick(menu, 'Show Only = paid')
    pick(menu, 'Exclude ≠ paid')
    assert.deepEqual(views.map((v) => v.filters[1]), [{ mode: 'in', keys: ['paid'] }, { mode: 'notIn', keys: ['paid'] }])
  })

  it('offers clearing only when something is filtered or sorted', () => {
    const { actions } = recorder()
    const plain = labels(cellMenu({ view: emptyView, col: 0, columnName: 'a', value: 1, copies }, actions))
    assert.ok(!plain.includes('Clear All Filters') && !plain.includes('Clear Sort'))
    const busy = { ...onlyValue(emptyView, 0, 1), sort: [{ col: 0, dir: 'asc' as const }] }
    const filtered = labels(cellMenu({ view: busy, col: 0, columnName: 'a', value: 1, copies }, actions))
    for (const l of ['Clear Column Filter', 'Clear All Filters', 'Clear Sort']) assert.ok(filtered.includes(l), l)
  })

  it('copies through the grid and adds edit items for editable grids', () => {
    const { calls, actions } = recorder()
    const edit = { canEdit: true, deleted: false, edit: () => calls.push('edit'), setNull: () => calls.push('null'), toggleDelete: () => calls.push('del') }
    const menu = cellMenu({ view: emptyView, col: 0, columnName: 'a', value: null, copies, edit }, actions)
    for (const l of ['Copy Value', 'Copy Row', 'Copy Row as INSERT', 'Copy Column as IN (…)', 'Edit Value', 'Set NULL', 'Delete Row']) pick(menu, l)
    assert.deepEqual(calls, ['value:', 'row:r', 'row as INSERT:i', 'IN list:in', 'edit', 'null', 'del'])
    const readOnlyCol = labels(cellMenu({ view: emptyView, col: 0, columnName: 'a', value: 1, copies, edit: { ...edit, canEdit: false } }, actions))
    assert.ok(!readOnlyCol.includes('Edit Value') && readOnlyCol.includes('Delete Row'))
  })
})

describe('headerMenu', () => {
  it('sorts and opens the filter list', () => {
    const { views, calls, actions } = recorder()
    const menu = headerMenu(emptyView, 2, 'name', () => 'x\ny', actions)
    pick(menu, 'Sort Descending')
    pick(menu, 'Filter Column Values…')
    pick(menu, 'Copy Column Name')
    assert.deepEqual(views[0].sort, [{ col: 2, dir: 'desc' }])
    assert.deepEqual(calls, ['filter:2', 'column name:name'])
  })
})

describe('shortValue', () => {
  it('shortens long values and names NULL and empty text', () => {
    assert.equal(shortValue(null), 'NULL')
    assert.equal(shortValue(''), "''")
    assert.equal(shortValue('x'.repeat(40)).length, 32)
  })
})
