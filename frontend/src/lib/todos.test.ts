import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { affectsTodos, countTags, groupTodos, type TodoItem } from './todos'

const item = (path: string, line: number, tag: string, text = ''): TodoItem => ({ root: '/p', path, line, column: 1, tag, text }) as TodoItem

describe('todos', () => {
  const items = [item('a.go', 3, 'TODO', 'cache'), item('a.go', 9, 'FIXME', 'leak'), item('b.go', 1, 'TODO', 'docs')]

  it('groups by file in scan order', () => {
    assert.deepEqual(groupTodos(items, new Set(), '').map((f) => [f.path, f.items.length]), [['a.go', 2], ['b.go', 1]])
  })

  it('filters by tag and text', () => {
    assert.deepEqual(groupTodos(items, new Set(['FIXME']), '').map((f) => f.items.map((i) => i.line)), [[9]])
    assert.deepEqual(groupTodos(items, new Set(), 'DOC').map((f) => f.path), ['b.go'])
  })

  it('counts tags', () => {
    assert.deepEqual(countTags(items), { TODO: 2, FIXME: 1 })
  })
})

describe('rescan on file changes', () => {
  it('ignores .git, dependency folders and the folders the scan skipped', () => {
    const skipped = ['/p/.env', '/p/rust/target']
    assert.equal(affectsTodos('/p/src/a.ts', skipped), true)
    assert.equal(affectsTodos('/p/.git/index', skipped), false)
    assert.equal(affectsTodos('/p/web/node_modules/x/i.js', skipped), false)
    assert.equal(affectsTodos('/p/.env/lib/site.py', skipped), false)
    assert.equal(affectsTodos('/p/.envrc', skipped), true)
  })
})
