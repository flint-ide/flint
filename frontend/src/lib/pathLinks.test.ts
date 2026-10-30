import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { assertSubset } from '../testing/assertSubset'
import { findPathLinks, resolvePath } from './pathLinks'

describe('findPathLinks', () => {
  it('finds paths with line and column', () => {
    const [link] = findPathLinks('  Edited internal/db/store.go:42:7 (3 lines)')
    assertSubset(link, { path: 'internal/db/store.go', line: 42, column: 7 })
  })

  it('finds relative and absolute paths without a line', () => {
    const paths = findPathLinks('see ./main.go and /Users/me/app/config.yaml').map((l) => l.path)
    assert.deepEqual(paths, ['./main.go', '/Users/me/app/config.yaml'])
  })

  it('ignores plain words, versions and URLs', () => {
    assert.deepEqual(findPathLinks('hello world v1.2.3 1.26.0'), [])
    assert.deepEqual(findPathLinks('open https://example.com/docs'), [])
  })

  it('reports the span covering the line suffix', () => {
    const text = 'x src/a.ts:10 y'
    const [link] = findPathLinks(text)
    assert.equal(text.slice(link.start, link.end), 'src/a.ts:10')
  })

  it('trims trailing punctuation from the path', () => {
    assert.equal(findPathLinks('(see README.md)')[0].path, 'README.md')
  })
})

describe('resolvePath', () => {
  it('joins relative paths with the root', () => {
    assert.equal(resolvePath('/p', './src/../a.go'), '/p/a.go')
    assert.equal(resolvePath('/p', 'b/c.go'), '/p/b/c.go')
  })

  it('keeps absolute paths', () => {
    assert.equal(resolvePath('/p', '/etc/hosts'), '/etc/hosts')
  })
})
