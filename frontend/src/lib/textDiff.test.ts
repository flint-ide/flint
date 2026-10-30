import { describe, expect, it } from 'vitest'
import { minimalChange } from './textDiff'

const apply = (a: string, c: ReturnType<typeof minimalChange>) =>
  c ? a.slice(0, c.from) + c.insert + a.slice(c.to) : a

describe('minimalChange', () => {
  it('returns null for identical text', () => {
    expect(minimalChange('abc', 'abc')).toBeNull()
  })

  it.each([
    ['hello world', 'hello brave world'],
    ['line1\nline2\nline3', 'line1\nline3'],
    ['', 'new file'],
    ['old', ''],
    ['aaaa', 'aa'],
    ['func a() {}\n', 'func a() {\n\treturn\n}\n'],
  ])('turns %j into %j with a minimal edit', (a, b) => {
    const change = minimalChange(a, b)
    expect(apply(a, change)).toBe(b)
  })

  it('keeps the common prefix and suffix out of the change', () => {
    expect(minimalChange('abcXdef', 'abcYYdef')).toEqual({ from: 3, to: 4, insert: 'YY' })
  })
})
