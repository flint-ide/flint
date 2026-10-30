import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { minimalChange } from './textDiff'

const apply = (a: string, c: ReturnType<typeof minimalChange>) =>
  c ? a.slice(0, c.from) + c.insert + a.slice(c.to) : a

describe('minimalChange', () => {
  it('returns null for identical text', () => {
    assert.equal(minimalChange('abc', 'abc'), null)
  })

  for (const [a, b] of [
    ['hello world', 'hello brave world'],
    ['line1\nline2\nline3', 'line1\nline3'],
    ['', 'new file'],
    ['old', ''],
    ['aaaa', 'aa'],
    ['func a() {}\n', 'func a() {\n\treturn\n}\n'],
  ]) {
    it(`turns ${JSON.stringify(a)} into ${JSON.stringify(b)} with a minimal edit`, () => {
      const change = minimalChange(a, b)
      assert.equal(apply(a, change), b)
    })
  }

  it('keeps the common prefix and suffix out of the change', () => {
    assert.deepEqual(minimalChange('abcXdef', 'abcYYdef'), { from: 3, to: 4, insert: 'YY' })
  })
})
