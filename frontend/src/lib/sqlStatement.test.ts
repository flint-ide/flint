import { describe, expect, it } from 'vitest'
import { splitStatements, statementAt } from './sqlStatement'

describe('splitStatements', () => {
  it('splits on semicolons and trims', () => {
    const sqls = splitStatements('select 1;\n  select 2 ;\n').map((s) => s.sql)
    expect(sqls).toEqual(['select 1', 'select 2'])
  })

  it('ignores semicolons in strings, identifiers and comments', () => {
    const text = `select ';' as a, "x;y", \`b;c\` -- c;omment
from t /* ; */ where s = 'it''s;';
select 2`
    const sqls = splitStatements(text).map((s) => s.sql)
    expect(sqls).toHaveLength(2)
    expect(sqls[1]).toBe('select 2')
  })

  it('handles postgres dollar quoting', () => {
    const text = 'create function f() returns int as $body$ select 1; $body$ language sql; select 3'
    const sqls = splitStatements(text).map((s) => s.sql)
    expect(sqls).toHaveLength(2)
    expect(sqls[0]).toContain('$body$ select 1; $body$')
  })

  it('handles backslash escapes in single quotes', () => {
    const sqls = splitStatements("select 'a\\';b'; select 2").map((s) => s.sql)
    expect(sqls).toEqual(["select 'a\\';b'", 'select 2'])
  })

  it('drops comment-only chunks', () => {
    expect(splitStatements('-- just a note\n;\n/* x */')).toEqual([])
  })

  it('reports offsets into the original text', () => {
    const text = 'select 1;\n\n  select 2'
    const [, second] = splitStatements(text)
    expect(text.slice(second.from, second.to)).toBe('select 2')
  })
})

describe('statementAt', () => {
  const text = 'select 1;\n\nselect 2;\nselect 3'

  it('finds the statement under the cursor', () => {
    expect(statementAt(text, text.indexOf('2'))?.sql).toBe('select 2')
  })

  it('treats the position just after a semicolon as the previous statement', () => {
    expect(statementAt(text, text.indexOf(';') + 1)?.sql).toBe('select 1')
  })

  it('resolves blank lines to the preceding statement', () => {
    expect(statementAt(text, text.indexOf('\n\n') + 1)?.sql).toBe('select 1')
  })

  it('returns null for empty input', () => {
    expect(statementAt('  ', 0)).toBeNull()
  })
})
