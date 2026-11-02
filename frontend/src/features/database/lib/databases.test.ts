import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  connectionUrl,
  countQuery,
  databaseChoices,
  defaultDatabase,
  defaultDatabaseName,
  isSchemaKeyOf,
  previewQuery,
  qualifiedTable,
  quoteIdent,
  resolveDatabase,
  schemaKey,
} from './databases'

describe('defaultDatabase', () => {
  it('prefers the profile database', () => {
    assert.equal(defaultDatabase({ driver: 'postgres', database: 'app' }), 'app')
    assert.equal(defaultDatabase({ driver: 'clickhouse', database: 'logs' }), 'logs')
  })

  it('falls back to the server default when the profile has none', () => {
    assert.equal(defaultDatabase({ driver: 'postgres', database: '' }), 'postgres')
    assert.equal(defaultDatabase({ driver: 'clickhouse', database: '' }), 'default')
  })
})

describe('resolveDatabase', () => {
  it('keeps an explicit choice and resolves an empty one to the profile database', () => {
    const conn = { database: 'app' }
    assert.equal(resolveDatabase(conn, 'billing'), 'billing')
    assert.equal(resolveDatabase(conn, ''), 'app')
    assert.equal(resolveDatabase(conn, undefined), 'app')
  })

  it('leaves the server default to Go when the profile has no database', () => {
    assert.equal(resolveDatabase({ database: '' }, ''), '')
  })
})

describe('defaultDatabaseName', () => {
  const conn = { driver: 'clickhouse', database: '' }
  it('trusts the server flag over the fallback', () => {
    const list = [
      { name: 'default', canConnect: true, default: false },
      { name: 'mail', canConnect: true, default: true },
    ]
    assert.equal(defaultDatabaseName(list, conn), 'mail')
  })

  it('falls back before the list loads', () => {
    assert.equal(defaultDatabaseName(undefined, conn), 'default')
  })
})

describe('schemaKey', () => {
  it('separates databases of one connection and tells connections apart', () => {
    assert.notEqual(schemaKey('c1', 'a'), schemaKey('c1', 'b'))
    assert.ok(isSchemaKeyOf(schemaKey('c1', 'a'), 'c1'))
    assert.ok(!isSchemaKeyOf(schemaKey('c10', 'a'), 'c1'))
  })
})

describe('databaseChoices', () => {
  const list = [
    { name: 'admindb', canConnect: true, default: true },
    { name: 'rdsadmin', canConnect: false, default: false },
  ]

  it('returns the server list when it has the current database', () => {
    assert.deepEqual(databaseChoices(list, 'admindb'), list)
  })

  it('keeps the current database visible before the list loads', () => {
    assert.deepEqual(databaseChoices(undefined, 'app'), [{ name: 'app', canConnect: true, default: false }])
    assert.deepEqual(databaseChoices(list, 'gone')[0], { name: 'gone', canConnect: true, default: false })
  })

  it('adds nothing for an empty choice', () => {
    assert.deepEqual(databaseChoices(undefined, ''), [])
  })
})

describe('quoteIdent', () => {
  it('leaves plain lowercase names bare', () => {
    assert.equal(quoteIdent('postgres', 'users'), 'users')
    assert.equal(quoteIdent('clickhouse', 'events_2024'), 'events_2024')
  })

  it('quotes with the dialect quote and doubles it inside', () => {
    assert.equal(quoteIdent('postgres', 'Users'), '"Users"')
    assert.equal(quoteIdent('postgres', 'a"b'), '"a""b"')
    assert.equal(quoteIdent('clickhouse', 'my-table'), '`my-table`')
    assert.equal(quoteIdent('clickhouse', 'a`b'), '`a``b`')
  })
})

describe('table queries', () => {
  const t = { schema: 'public', name: 'Order' }

  it('qualifies the table with its schema', () => {
    assert.equal(qualifiedTable('postgres', t), 'public."Order"')
  })

  it('builds the preview and count statements', () => {
    assert.equal(previewQuery('postgres', t), 'SELECT *\nFROM public."Order"\nLIMIT 100;\n')
    assert.equal(countQuery('clickhouse', { schema: 'logs', name: 'hits' }), 'SELECT count(*)\nFROM logs.hits;\n')
  })
})

describe('connectionUrl', () => {
  it('builds a password-less URL with the default database', () => {
    const conn = { driver: 'postgres', host: 'db.local', port: 5432, user: 'app', database: '' }
    assert.equal(connectionUrl(conn), 'postgresql://app@db.local:5432/postgres')
  })

  it('escapes the user and database and omits an empty user', () => {
    assert.equal(
      connectionUrl({ driver: 'clickhouse', host: 'ch', port: 9000, user: 'a@b', database: 'my db' }),
      'clickhouse://a%40b@ch:9000/my%20db',
    )
    assert.equal(connectionUrl({ driver: 'clickhouse', host: 'ch', port: 9000, user: '', database: 'x' }), 'clickhouse://ch:9000/x')
  })
})
