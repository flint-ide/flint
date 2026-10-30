import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { databaseChoices, defaultDatabase, defaultDatabaseName, isSchemaKeyOf, resolveDatabase, schemaKey } from './databases'

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
