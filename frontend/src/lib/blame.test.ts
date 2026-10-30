import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { EditorState, Text } from '@codemirror/state'
import { NOT_COMMITTED, blameLabel, blameTooltip, joinRoot, lineCommits, relativeTo, shortAuthor, type BlameCommit } from './blame'
import { blameAt, blameGutter, blameMarkers, blameShown, setBlame } from './blameGutter'

const commit = (hash: string, author = 'Ada Lovelace', time = 1_700_000_000): BlameCommit => ({
  hash,
  author,
  email: 'ada@example.com',
  time,
  summary: `summary of ${hash}`,
  path: 'a.go',
  previous: '',
  prevPath: '',
  uncommitted: false,
})

describe('lineCommits', () => {
  const a = commit('a'.repeat(40))
  const b = commit('b'.repeat(40))

  it('expands ranges to one commit per line', () => {
    const lines = lineCommits([a, b], [{ line: 1, count: 2, commit: 0 }, { line: 3, count: 1, commit: 1 }], 3)
    assert.deepEqual(lines.slice(1), [a, a, b])
  })

  it('marks lines without a range as not committed and ignores ranges past the end', () => {
    const lines = lineCommits([a], [{ line: 2, count: 5, commit: 0 }], 3)
    assert.deepEqual(lines.slice(1), [NOT_COMMITTED, a, a])
  })

  it('marks every line of an untracked file as not committed', () => {
    assert.deepEqual(lineCommits([a], [{ line: 1, count: 2, commit: 0 }], 2, true).slice(1), [NOT_COMMITTED, NOT_COMMITTED])
  })
})

describe('blame labels', () => {
  it('shows date and first name, IntelliJ style', () => {
    assert.ok(/^\d{4}-\d{2}-\d{2} Ada$/.test(blameLabel(commit('c'.repeat(40)))))
    assert.equal(blameLabel(NOT_COMMITTED), 'Not committed')
  })

  it('caps long names', () => {
    assert.equal(shortAuthor('Bartholomew-Maximilian Smith', 8), 'Barthol…')
    assert.equal(shortAuthor('  '), '')
  })

  it('puts the full hash, author and summary in the tooltip', () => {
    const tip = blameTooltip(commit('d'.repeat(40)))
    assert.ok(tip.startsWith('d'.repeat(40)))
    assert.ok(tip.includes('Ada Lovelace <ada@example.com>'))
    assert.ok(tip.endsWith('summary of ' + 'd'.repeat(40)))
  })
})

describe('paths', () => {
  it('converts between absolute and repo-relative paths', () => {
    assert.equal(relativeTo('/repo', '/repo/src/a.go'), 'src/a.go')
    assert.equal(relativeTo('/repo/', '/repo/a.go'), 'a.go')
    assert.equal(relativeTo('/repo', '/repository/a.go'), '')
    assert.equal(joinRoot('/repo/', 'a.go'), '/repo/a.go')
  })
})

describe('blameGutter', () => {
  const a = commit('a'.repeat(40))
  const b = commit('b'.repeat(40))
  const ranges = [{ line: 1, count: 1, commit: 0 }, { line: 2, count: 2, commit: 1 }]

  it('shares one marker per commit', () => {
    const set = blameMarkers(Text.of(['x', 'y', 'z']), { commits: [a, b], ranges, untracked: false })
    const seen: unknown[] = []
    set.between(0, 10, (_f, _t, m) => void seen.push(m))
    assert.equal(seen.length, 3)
    assert.equal(seen[1], seen[2])
    assert.notEqual(seen[0], seen[1])
  })

  it('maps annotations through edits and clears them on null', () => {
    let state = EditorState.create({ doc: 'x\ny\nz', extensions: blameGutter() })
    assert.equal(blameShown(state), false)
    state = state.update({ effects: setBlame.of({ commits: [a, b], ranges, untracked: false, open: () => undefined }) }).state
    assert.equal(blameShown(state), true)
    assert.equal(blameAt(state, state.doc.line(1).from), a)
    // A line typed after line 1 has no annotation; the old lines keep theirs.
    state = state.update({ changes: { from: 1, insert: '\nnew' } }).state
    assert.equal(blameAt(state, state.doc.line(1).from), a)
    assert.equal(blameAt(state, state.doc.line(2).from), null)
    assert.equal(blameAt(state, state.doc.line(3).from), b)
    assert.equal(blameAt(state, state.doc.line(4).from), b)
    state = state.update({ effects: setBlame.of(null) }).state
    assert.equal(blameShown(state), false)
    assert.equal(blameAt(state, 0), null)
  })
})
