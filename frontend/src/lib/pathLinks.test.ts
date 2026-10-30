import { describe, expect, it } from 'vitest'
import { findPathLinks, resolvePath } from './pathLinks'

describe('findPathLinks', () => {
  it('finds paths with line and column', () => {
    const [link] = findPathLinks('  Edited internal/db/store.go:42:7 (3 lines)')
    expect(link).toMatchObject({ path: 'internal/db/store.go', line: 42, column: 7 })
  })

  it('finds relative and absolute paths without a line', () => {
    const paths = findPathLinks('see ./main.go and /Users/me/app/config.yaml').map((l) => l.path)
    expect(paths).toEqual(['./main.go', '/Users/me/app/config.yaml'])
  })

  it('ignores plain words, versions and URLs', () => {
    expect(findPathLinks('hello world v1.2.3 1.26.0')).toEqual([])
    expect(findPathLinks('open https://example.com/docs')).toEqual([])
  })

  it('reports the span covering the line suffix', () => {
    const text = 'x src/a.ts:10 y'
    const [link] = findPathLinks(text)
    expect(text.slice(link.start, link.end)).toBe('src/a.ts:10')
  })

  it('trims trailing punctuation from the path', () => {
    expect(findPathLinks('(see README.md)')[0].path).toBe('README.md')
  })
})

describe('resolvePath', () => {
  it('joins relative paths with the root', () => {
    expect(resolvePath('/p', './src/../a.go')).toBe('/p/a.go')
    expect(resolvePath('/p', 'b/c.go')).toBe('/p/b/c.go')
  })

  it('keeps absolute paths', () => {
    expect(resolvePath('/p', '/etc/hosts')).toBe('/etc/hosts')
  })
})
