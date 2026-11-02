import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { DropdownItem } from '../ui/Dropdown'
import { columnMenu, connectionMenu, databaseMenu, schemaMenu, tableMenu } from './databaseMenu'

type Item = Extract<DropdownItem, { onSelect: unknown }>

const items = (menu: DropdownItem[]): Item[] => menu.filter((i): i is Item => 'onSelect' in i)
const labels = (menu: DropdownItem[]) => items(menu).map((i) => i.label)
const pick = (menu: DropdownItem[], label: string) => {
  const item = items(menu).find((i) => i.label === label)
  assert.ok(item, `no "${label}" in ${labels(menu).join(', ')}`)
  item.onSelect({ altKey: false, metaKey: false })
}

/** Records every action a menu triggers, in order. */
function recorder() {
  const calls: string[] = []
  const log = (name: string) => (arg?: string) => calls.push(arg === undefined ? name : `${name}:${arg}`)
  return {
    calls,
    actions: {
      newConsole: log('newConsole'),
      refresh: log('refresh'),
      edit: log('edit'),
      remove: log('remove'),
      query: log('query'),
      copy: log('copy'),
    },
  }
}

const conn = { driver: 'postgres', host: 'db', port: 5432, user: 'app', database: 'shop', name: 'Prod' }

describe('connectionMenu', () => {
  it('offers the row actions plus copying, with delete last and marked dangerous', () => {
    const { actions } = recorder()
    const menu = connectionMenu(conn, actions)
    assert.deepEqual(labels(menu), ['New SQL Console', 'Refresh Databases', 'Edit Connection…', 'Copy Name', 'Copy Connection URL', 'Delete Connection…'])
    assert.equal(items(menu).at(-1)?.danger, true)
  })

  it('wires each item to its action', () => {
    const { calls, actions } = recorder()
    const menu = connectionMenu(conn, actions)
    for (const label of labels(menu)) pick(menu, label)
    assert.deepEqual(calls, ['newConsole', 'refresh', 'edit', 'copy:Prod', 'copy:postgresql://app@db:5432/shop', 'remove'])
  })
})

describe('databaseMenu', () => {
  it('opens a console, refreshes and copies a reachable database', () => {
    const { calls, actions } = recorder()
    const menu = databaseMenu({ name: 'billing', canConnect: true, default: false }, actions)
    assert.deepEqual(labels(menu), ['New SQL Console', 'Refresh Schema', 'Copy Name'])
    for (const label of labels(menu)) pick(menu, label)
    assert.deepEqual(calls, ['newConsole', 'refresh', 'copy:billing'])
  })

  it('only copies the name of a database without CONNECT', () => {
    const { actions } = recorder()
    assert.deepEqual(labels(databaseMenu({ name: 'rdsadmin', canConnect: false, default: false }, actions)), ['Copy Name'])
  })
})

describe('schemaMenu', () => {
  it('copies the schema name', () => {
    const { calls, actions } = recorder()
    pick(schemaMenu('public', actions), 'Copy Name')
    assert.deepEqual(calls, ['copy:public'])
  })
})

describe('tableMenu', () => {
  const table = { schema: 'public', name: 'Order', kind: 'table', columns: [{ name: 'id', type: 'int' }, { name: 'Total', type: 'numeric' }] }

  it('queries the table and copies its names', () => {
    const { calls, actions } = recorder()
    const menu = tableMenu('postgres', table, actions)
    assert.deepEqual(labels(menu), ['Preview Data', 'Count Rows', 'Copy Name', 'Copy Qualified Name', 'Copy Column Names'])
    for (const label of labels(menu)) pick(menu, label)
    assert.deepEqual(calls, [
      'query:SELECT *\nFROM public."Order"\nLIMIT 100;\n',
      'query:SELECT count(*)\nFROM public."Order";\n',
      'copy:Order',
      'copy:public."Order"',
      'copy:id, "Total"',
    ])
  })

  it('leaves out the column list when the table has no columns', () => {
    const { actions } = recorder()
    assert.ok(!labels(tableMenu('clickhouse', { ...table, columns: [] }, actions)).includes('Copy Column Names'))
  })
})

describe('columnMenu', () => {
  it('copies the name and the name with its type', () => {
    const { calls, actions } = recorder()
    const menu = columnMenu({ name: 'created_at', type: 'timestamptz' }, actions)
    for (const label of labels(menu)) pick(menu, label)
    assert.deepEqual(calls, ['copy:created_at', 'copy:created_at timestamptz'])
  })
})
