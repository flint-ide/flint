import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { DropdownItem } from '../../../components/ui/Dropdown'
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
    assert.deepEqual(labels(menu), [
      'New SQL Console',
      'Refresh Databases',
      'Edit Connection…',
      'Copy Name',
      'Copy Connection URL',
      'Copy Host and Port',
      'Delete Connection…',
    ])
    assert.equal(items(menu).at(-1)?.danger, true)
  })

  it('wires each item to its action', () => {
    const { calls, actions } = recorder()
    const menu = connectionMenu(conn, actions)
    for (const label of labels(menu)) pick(menu, label)
    assert.deepEqual(calls, ['newConsole', 'refresh', 'edit', 'copy:Prod', 'copy:postgresql://app@db:5432/shop', 'copy:db:5432', 'remove'])
  })
})

describe('databaseMenu', () => {
  it('opens a console, refreshes, sizes and copies a reachable database', () => {
    const { calls, actions } = recorder()
    const menu = databaseMenu('postgres', { name: 'billing', canConnect: true, default: false }, actions)
    assert.deepEqual(labels(menu), ['New SQL Console', 'Refresh Schema', 'Database Size', 'Copy Name'])
    for (const label of labels(menu)) pick(menu, label)
    assert.deepEqual(calls, ['newConsole', 'refresh', 'query:SELECT pg_size_pretty(pg_database_size(current_database())) AS size;\n', 'copy:billing'])
  })

  it('lists table sizes of a ClickHouse database', () => {
    const { actions } = recorder()
    assert.ok(labels(databaseMenu('clickhouse', { name: 'logs', canConnect: true, default: false }, actions)).includes('Table Sizes'))
  })

  it('only copies the name of a database without CONNECT', () => {
    const { actions } = recorder()
    assert.deepEqual(labels(databaseMenu('postgres', { name: 'rdsadmin', canConnect: false, default: false }, actions)), ['Copy Name'])
  })
})

describe('schemaMenu', () => {
  it('lists table sizes and copies the schema name', () => {
    const { calls, actions } = recorder()
    const menu = schemaMenu('postgres', 'public', actions)
    assert.deepEqual(labels(menu), ['Table Sizes', 'Copy Name'])
    pick(menu, 'Copy Name')
    assert.deepEqual(calls, ['copy:public'])
  })
})

describe('tableMenu', () => {
  const table = { schema: 'public', name: 'Order', kind: 'table', columns: [{ name: 'id', type: 'int' }, { name: 'Total', type: 'numeric' }] }

  it('offers data, scripts, information, copying and (last, dangerous) truncate and drop', () => {
    const { actions } = recorder()
    const menu = tableMenu('postgres', table, actions)
    assert.deepEqual(labels(menu), [
      'Preview Data',
      'Count Rows',
      'Refresh Schema',
      'SELECT Statement',
      'INSERT Statement',
      'UPDATE Statement',
      'DELETE Statement',
      'Generate DDL',
      'Indexes',
      'Table Size',
      'Copy Name',
      'Copy Qualified Name',
      'Copy Column Names',
      'Copy SELECT Statement',
      'Truncate…',
      'Drop…',
    ])
    assert.deepEqual(items(menu).filter((i) => i.danger).map((i) => i.label), ['Truncate…', 'Drop…'])
  })

  it('opens statements in a console and copies names', () => {
    const { calls, actions } = recorder()
    const menu = tableMenu('postgres', table, actions)
    for (const label of ['Preview Data', 'Count Rows', 'Copy Name', 'Copy Qualified Name', 'Copy Column Names', 'Drop…']) pick(menu, label)
    assert.deepEqual(calls, [
      'query:SELECT *\nFROM public."Order"\nLIMIT 100;\n',
      'query:SELECT count(*)\nFROM public."Order";\n',
      'copy:Order',
      'copy:public."Order"',
      'copy:id, "Total"',
      'query:DROP TABLE public."Order";\n',
    ])
  })

  it('offers no data-changing templates for a view and shows real DDL on ClickHouse', () => {
    const { actions } = recorder()
    const view = labels(tableMenu('postgres', { ...table, kind: 'view' }, actions))
    for (const l of ['INSERT Statement', 'UPDATE Statement', 'DELETE Statement', 'Truncate…', 'Table Size']) assert.ok(!view.includes(l), l)
    assert.ok(labels(tableMenu('clickhouse', table, actions)).includes('Show DDL'))
  })

  it('leaves out the column list when the table has no columns', () => {
    const { actions } = recorder()
    assert.ok(!labels(tableMenu('clickhouse', { ...table, columns: [] }, actions)).includes('Copy Column Names'))
  })
})

describe('columnMenu', () => {
  it('queries the column and copies its names', () => {
    const { calls, actions } = recorder()
    const menu = columnMenu('postgres', { schema: 'public', name: 'Order' }, { name: 'created_at', type: 'timestamptz' }, actions)
    for (const label of labels(menu)) pick(menu, label)
    assert.deepEqual(calls, [
      'query:SELECT created_at, count(*) AS count\nFROM public."Order"\nGROUP BY created_at\nORDER BY count DESC\nLIMIT 100;\n',
      'query:SELECT *\nFROM public."Order"\nWHERE created_at IS NULL\nLIMIT 100;\n',
      'copy:created_at',
      'copy:public."Order".created_at',
      'copy:created_at timestamptz',
    ])
  })
})
