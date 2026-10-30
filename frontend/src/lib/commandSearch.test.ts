import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { fuzzyMatch, pushRecent, rankCommands } from './commandSearch'

const items = [
  { id: 'terminal.toggle', title: 'Toggle Terminal', category: 'Terminal' },
  { id: 'terminal.new', title: 'New Terminal', category: 'Terminal' },
  { id: 'git.push', title: 'Push…', category: 'Git' },
  { id: 'git.pull', title: 'Update Project (Pull)', category: 'Git' },
  { id: 'view.sidebar', title: 'Toggle Sidebar', category: 'View', keywords: 'panel hide show' },
  { id: 'file.open', title: 'Go to File…', category: 'Navigate' },
]
const ids = (r: { item: { id: string } }[]) => r.map((x) => x.item.id)

describe('fuzzyMatch', () => {
  it('matches subsequences case-insensitively and ignores spaces', () => {
    assert.deepEqual(fuzzyMatch('NT', 'New Terminal')?.positions, [0, 4])
    assert.deepEqual(fuzzyMatch('new term', 'New Terminal')?.positions, [0, 1, 2, 4, 5, 6, 7])
    assert.equal(fuzzyMatch('xyz', 'New Terminal'), null)
    assert.deepEqual(fuzzyMatch('', 'anything'), { score: 0, positions: [] })
  })

  it('prefers word starts and consecutive runs', () => {
    const initials = fuzzyMatch('tt', 'Toggle Terminal')
    assert.deepEqual(initials?.positions, [0, 7])
    const run = fuzzyMatch('term', 'Toggle Terminal')!
    const scattered = fuzzyMatch('term', 'The Remote Merge')!
    assert.ok(run.score > scattered.score)
  })

  it('still matches when jumping to word starts would strand later letters', () => {
    assert.ok(fuzzyMatch('abc', 'xab c-b'))
    assert.deepEqual(fuzzyMatch('ab', 'ab b')?.positions, [0, 1])
  })
})

describe('rankCommands', () => {
  it('puts recently used commands first for an empty query', () => {
    assert.deepEqual(ids(rankCommands(items, '', ['git.push', 'gone', 'file.open'])), [
      'git.push', 'file.open', 'terminal.toggle', 'terminal.new', 'git.pull', 'view.sidebar',
    ])
  })

  it('keeps only matches, best first', () => {
    const r = rankCommands(items, 'push', [])
    assert.deepEqual(ids(r), ['git.push'])
    assert.deepEqual(r[0].positions, [0, 1, 2, 3])
    assert.equal(ids(rankCommands(items, 'tog term', []))[0], 'terminal.toggle')
  })

  it('matches the category and keywords too', () => {
    assert.ok(ids(rankCommands(items, 'git pull', [])).includes('git.pull'))
    assert.deepEqual(ids(rankCommands(items, 'panel', [])), ['view.sidebar'])
  })

  it('prefers the shorter title when matches tie', () => {
    const pair = [
      { id: 'nav.findInFiles', title: 'Find in Files…' },
      { id: 'editor.find', title: 'Find in File…' },
    ]
    assert.equal(ids(rankCommands(pair, 'find in file', []))[0], 'editor.find')
  })

  it('breaks near-ties in favour of recent use', () => {
    const plain = ids(rankCommands(items, 'terminal', []))
    assert.equal(plain[0], 'terminal.new')
    assert.equal(ids(rankCommands(items, 'terminal', ['terminal.toggle']))[0], 'terminal.toggle')
  })
})

describe('pushRecent', () => {
  it('moves the id to the front, dedupes and caps the list', () => {
    assert.deepEqual(pushRecent(['a', 'b', 'c'], 'c'), ['c', 'a', 'b'])
    assert.deepEqual(pushRecent(['a', 'b'], 'x', 2), ['x', 'a'])
    const input = ['a']
    pushRecent(input, 'b')
    assert.deepEqual(input, ['a'])
  })
})
