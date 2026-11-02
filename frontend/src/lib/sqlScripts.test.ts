import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  ddlStatement,
  deleteStatement,
  dropStatement,
  inList,
  insertRowStatement,
  insertStatement,
  selectStatement,
  sqlLiteral,
  tableSizeQuery,
  updateStatement,
} from './sqlScripts'

const t = { schema: 'public', name: 'Order', kind: 'table', columns: [{ name: 'id', type: 'int' }, { name: 'note', type: 'text' }] }

describe('sqlLiteral', () => {
  it('quotes text, keeps numbers bare and escapes per dialect', () => {
    assert.equal(sqlLiteral('postgres', null), 'NULL')
    assert.equal(sqlLiteral('postgres', 42), '42')
    assert.equal(sqlLiteral('postgres', true), 'true')
    assert.equal(sqlLiteral('postgres', "it's \\"), "'it''s \\'")
    assert.equal(sqlLiteral('clickhouse', "it's \\"), "'it''s \\\\'")
    assert.equal(sqlLiteral('postgres', { a: 1 }), `'{"a":1}'`)
  })
})

describe('table statements', () => {
  it('lists the columns, quoting where needed', () => {
    assert.equal(selectStatement('postgres', t), 'SELECT id,\n       note\nFROM public."Order"\nLIMIT 100;\n')
    assert.match(insertStatement('postgres', t), /^INSERT INTO public\."Order" \(id, note\)\nVALUES \(\n {4}NULL, -- id int\n {4}NULL -- note text\n\);/)
    assert.match(updateStatement('postgres', t), /^UPDATE public\."Order"\nSET\n {4}id = NULL,\n {4}note = NULL\nWHERE false;/)
    assert.match(updateStatement('clickhouse', t), /^ALTER TABLE public\.`Order`\nUPDATE/)
    assert.equal(deleteStatement('clickhouse', t), 'DELETE FROM public.`Order`\nWHERE false;\n')
  })

  it('shows real DDL on ClickHouse and a labelled rebuild on Postgres', () => {
    assert.equal(ddlStatement('clickhouse', t), 'SHOW CREATE TABLE public.`Order`;\n')
    const pg = ddlStatement('postgres', t)
    assert.match(pg, /^-- Rebuilt from the column list/)
    assert.match(pg, /CREATE TABLE public\."Order" \(\n {4}id int,\n {4}note text\n\);/)
  })

  it('drops the right kind of object', () => {
    assert.equal(dropStatement('postgres', t), 'DROP TABLE public."Order";\n')
    assert.equal(dropStatement('postgres', { ...t, kind: 'materialized view' }), 'DROP MATERIALIZED VIEW public."Order";\n')
    assert.equal(dropStatement('clickhouse', { ...t, kind: 'materialized view' }), 'DROP VIEW public.`Order`;\n')
    assert.equal(dropStatement('clickhouse', { ...t, kind: 'dictionary' }), 'DROP DICTIONARY public.`Order`;\n')
  })

  it('passes the quoted relation name to the Postgres size functions', () => {
    assert.match(tableSizeQuery('postgres', t), /pg_total_relation_size\('public\."Order"'\)/)
  })
})

describe('row statements', () => {
  it('builds an INSERT of a result row and an IN list without duplicates', () => {
    assert.equal(insertRowStatement('postgres', 'users', ['id', 'Name'], [1, null]), 'INSERT INTO users (id, "Name") VALUES (1, NULL);')
    assert.equal(inList('postgres', [1, 'a', 1, null]), "(1, 'a', NULL)")
  })
})
