import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  folderGlob, isWithin, rescanPaths, withDraft, workspaceRows, nameError, rangeBetween, relativePath, remapKeys, remapPath, stemRange, toggleIn, topLevel, visibleRows,
  type TreeEntry,
} from './fileTree'

const dir = (path: string): TreeEntry => ({ name: path.split('/').pop()!, path, isDir: true })
const file = (path: string): TreeEntry => ({ name: path.split('/').pop()!, path, isDir: false })

const children = {
  '/p': [dir('/p/src'), file('/p/go.mod')],
  '/p/src': [file('/p/src/a.go'), file('/p/src/b.go')],
}

describe('visibleRows', () => {
  it('walks only expanded folders', () => {
    const closed = visibleRows('/p', children, { '/p': true })
    assert.deepEqual(closed.map((r) => r.entry.path), ['/p/src', '/p/go.mod'])
    const open = visibleRows('/p', children, { '/p': true, '/p/src': true })
    assert.deepEqual(open.map((r) => [r.entry.path, r.depth]), [['/p/src', 0], ['/p/src/a.go', 1], ['/p/src/b.go', 1], ['/p/go.mod', 0]])
  })
})

describe('selection', () => {
  const rows = visibleRows('/p', children, { '/p': true, '/p/src': true })

  it('selects a range in tree order in either direction', () => {
    assert.deepEqual(rangeBetween(rows, '/p/go.mod', '/p/src/a.go'), ['/p/src/a.go', '/p/src/b.go', '/p/go.mod'])
    assert.deepEqual(rangeBetween(rows, '/p/src', '/p/src/a.go'), ['/p/src', '/p/src/a.go'])
    assert.deepEqual(rangeBetween(rows, '/gone', '/p/go.mod'), ['/p/go.mod'])
  })

  it('toggles paths', () => {
    assert.deepEqual(toggleIn(['/a'], '/b'), ['/a', '/b'])
    assert.deepEqual(toggleIn(['/a', '/b'], '/a'), ['/b'])
  })

  it('collapses nested selections into their folder', () => {
    assert.deepEqual(topLevel(['/p/src/a.go', '/p/src', '/p/go.mod', '/p/go.mod', '/p/srcx']), ['/p/src', '/p/go.mod', '/p/srcx'])
  })
})

describe('paths', () => {
  it('checks containment by path segment', () => {
    assert.ok(isWithin('/p/src', '/p/src'))
    assert.ok(isWithin('/p/src', '/p/src/a.go'))
    assert.ok(!isWithin('/p/src', '/p/srcx/a.go'))
  })

  it('remaps paths and keys through a folder rename', () => {
    assert.equal(remapPath('/p/src/a.go', '/p/src', '/p/lib'), '/p/lib/a.go')
    assert.equal(remapPath('/p/src', '/p/src', '/p/lib'), '/p/lib')
    assert.equal(remapPath('/p/srcx/a.go', '/p/src', '/p/lib'), '/p/srcx/a.go')
    assert.deepEqual(remapKeys({ '/p': true, '/p/src': true, '/p/src/x': false }, '/p/src', '/p/lib'), {
      '/p': true,
      '/p/lib': true,
      '/p/lib/x': false,
    })
  })

  it('builds relative paths and folder globs', () => {
    assert.equal(relativePath('/p', '/p/src/a.go'), 'src/a.go')
    assert.equal(relativePath('/p', '/p'), '')
    assert.equal(relativePath('/p', '/q/a'), '/q/a')
    assert.equal(folderGlob('/p', '/p/src/lib'), 'src/lib/**')
    assert.equal(folderGlob('/p', '/p'), '')
  })
})

describe('rename helpers', () => {
  it('selects the stem like Finder', () => {
    assert.deepEqual(stemRange('main.test.ts', false), [0, 9])
    assert.deepEqual(stemRange('.env', false), [0, 4])
    assert.deepEqual(stemRange('Makefile', false), [0, 8])
    assert.deepEqual(stemRange('v1.2', true), [0, 4])
  })

  it('validates names against siblings', () => {
    assert.equal(nameError('  ', []), 'A name is required')
    assert.ok(/cannot contain/.test(nameError('a/b', []) ?? ''))
    assert.ok(/not a valid/.test(nameError('..', []) ?? ''))
    assert.ok(/already exists/.test(nameError('Go.mod', ['go.mod']) ?? ''))
    assert.equal(nameError('README.md', ['Readme.md'], 'Readme.md'), null) // case-only rename
    assert.equal(nameError('new.go', ['go.mod']), null)
  })
})

describe('withDraft', () => {
  const rows = visibleRows('/p', children, { '/p': true, '/p/src': true })
  const shape = (d: ReturnType<typeof withDraft>) => d.map((r) => (r.kind === 'draft' ? `+${r.depth}` : r.row.entry.path))

  it('puts the input first in its folder, one level deeper', () => {
    assert.deepEqual(shape(withDraft(rows, '/p', { dir: '/p/src', isDir: false })), ['/p/src', '+1', '/p/src/a.go', '/p/src/b.go', '/p/go.mod'])
    assert.deepEqual(shape(withDraft(rows, '/p', { dir: '/p', isDir: true })), ['+0', '/p/src', '/p/src/a.go', '/p/src/b.go', '/p/go.mod'])
  })

  it('leaves rows alone without a draft or for a folder that is not shown', () => {
    assert.equal(withDraft(rows, '/p', null).length, rows.length)
    assert.equal(withDraft(rows, '/p', { dir: '/elsewhere', isDir: false }).length, rows.length)
  })
})

describe('workspaceRows', () => {
  const tree = { ...children, '/q': [file('/q/x.ts')] }

  it('shows a single folder without a header', () => {
    assert.deepEqual(workspaceRows(['/p'], ['p'], tree, { '/p': true }).map((r) => r.entry.path), ['/p/src', '/p/go.mod'])
  })

  it('gives every folder a collapsible header when there are several', () => {
    const rows = workspaceRows(['/p', '/q'], ['p', 'q'], tree, { '/p': true, '/p/src': true, '/q': false })
    assert.deepEqual(
      rows.map((r) => [r.entry.path, r.depth, !!r.isRoot]),
      [['/p', 0, true], ['/p/src', 1, false], ['/p/src/a.go', 2, false], ['/p/src/b.go', 2, false], ['/p/go.mod', 1, false], ['/q', 0, true]],
    )
    assert.equal(rows[0].entry.name, 'p')
  })

  it('puts a new-file input under a folder header', () => {
    const rows = workspaceRows(['/p', '/q'], ['p', 'q'], tree, { '/p': false, '/q': true })
    const shape = withDraft(rows, '', { dir: '/q', isDir: false }).map((r) => (r.kind === 'draft' ? `+${r.depth}` : r.row.entry.path))
    assert.deepEqual(shape, ['/p', '/q', '+1', '/q/x.ts'])
  })
})

describe('rescanPaths', () => {
  it('lists the open files and loaded folders under the root', () => {
    assert.deepEqual(rescanPaths('/p', ['/p/a.go', '/q/b.go', '/p/a.go'], ['/p', '/p/src', '/pp']), ['/p', '/p/src', '/p/a.go'])
  })
})
