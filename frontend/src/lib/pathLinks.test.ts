import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { assertSubset } from '../testing/assertSubset'
import { findPathLinks, firstExisting, linkBases, linkCandidates, resolvePath } from './pathLinks'

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

describe('linkBases', () => {
  it('keeps the given order and drops empty entries', () =>
    assert.deepEqual(linkBases(['/b', null, '', undefined, '/a']), ['/b', '/a']))
  it('drops repeats, also with a trailing slash', () => assert.deepEqual(linkBases(['/a', '/b/', '/a/', '/b']), ['/a', '/b/']))
  it('keeps the filesystem root', () => assert.deepEqual(linkBases(['/', '/']), ['/']))
})

describe('linkCandidates', () => {
  it('resolves a relative path against every base, in order', () =>
    assert.deepEqual(linkCandidates(['/b', '/a'], 'src/x.ts'), ['/b/src/x.ts', '/a/src/x.ts']))
  it('drops candidates that resolve to the same file', () =>
    assert.deepEqual(linkCandidates(['/a', '/a/b/..'], 'x.ts'), ['/a/x.ts']))
  it('leaves an absolute path alone', () => assert.deepEqual(linkCandidates(['/a', '/b'], '/etc/x'), ['/etc/x']))
  it('has none without a base', () => assert.deepEqual(linkCandidates([], 'x.ts'), []))
})

describe('firstExisting', () => {
  it('returns the first candidate that exists, and stops there', async () => {
    const asked: string[] = []
    const got = await firstExisting(['/a/x', '/b/x', '/c/x'], async (p) => (asked.push(p), p !== '/a/x'))
    assert.equal(got, '/b/x')
    assert.deepEqual(asked, ['/a/x', '/b/x'])
  })
  it('is null when none exists', async () => assert.equal(await firstExisting(['/a'], async () => false), null))
  it('treats a failing check as not found', async () =>
    assert.equal(
      await firstExisting(['/a', '/b'], async (p) => {
        if (p === '/a') throw new Error('boom')
        return true
      }),
      '/b',
    ))
})
