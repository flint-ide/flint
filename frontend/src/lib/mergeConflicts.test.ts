import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { Text } from '@codemirror/state'
import {
  baseFor,
  conflictAt,
  conflictPrompt,
  conflictText,
  diff3Segments,
  findConflicts,
  hasConflictMarkers,
  linesOf,
  markerKind,
  nextConflict,
  replaceConflict,
  resolvedLines,
  type ConflictRange,
} from './mergeConflicts'

const merged = ['a', '<<<<<<< HEAD', 'ours', '=======', 'theirs', '>>>>>>> feature', 'b', '<<<<<<< HEAD', 'x', '||||||| base', 'o', '=======', '>>>>>>> feature', 'z'].join('\n')

/** Applies replaceConflict's edit to text. */
function apply(text: string, c: ConflictRange, lines: string[]): string {
  const { from, to, insert } = replaceConflict(Text.of(text.split('\n')), c, lines)
  return text.slice(0, from) + insert + text.slice(to)
}

describe('markerKind', () => {
  it('follows git: seven characters, then nothing or a space and a label', () => {
    assert.equal(markerKind('<<<<<<< HEAD'), '<')
    assert.equal(markerKind('<<<<<<<'), '<')
    assert.equal(markerKind('||||||| merged common ancestors'), '|')
    assert.equal(markerKind('======='), '=')
    assert.equal(markerKind('=======\r'), '=')
    assert.equal(markerKind('>>>>>>> abc123 (Fix it)'), '>')
    assert.equal(markerKind('======= x'), null)
    assert.equal(markerKind('<<<<<<<< HEAD'), null)
    assert.equal(markerKind('<<<<<<<HEAD'), null)
    assert.equal(markerKind('===='), null)
  })
})

describe('findConflicts', () => {
  it('finds merge and diff3 style blocks', () => {
    assert.deepEqual(findConflicts(linesOf(merged)), [
      { open: 2, base: null, sep: 4, close: 6 },
      { open: 8, base: 10, sep: 12, close: 13 },
    ])
  })

  it('works on a CodeMirror document', () => {
    assert.equal(findConflicts(Text.of(merged.split('\n'))).length, 2)
  })

  it('ignores heading underlines and unfinished blocks', () => {
    assert.equal(hasConflictMarkers('Title\n=======\n\ntext'), false)
    assert.equal(hasConflictMarkers('<<<<<<< HEAD\nx\n=======\ny'), false)
    assert.equal(hasConflictMarkers(merged), true)
  })

  it('restarts at a second opening marker', () => {
    const text = '<<<<<<< HEAD\nstray\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> b'
    assert.deepEqual(findConflicts(linesOf(text)), [{ open: 3, base: null, sep: 5, close: 7 }])
  })
})

describe('resolving', () => {
  const [first, second] = findConflicts(linesOf(merged))

  it('reads each side', () => {
    assert.deepEqual(conflictText(linesOf(merged), first), { current: ['ours'], base: null, incoming: ['theirs'] })
    assert.deepEqual(conflictText(linesOf(merged), second), { current: ['x'], base: ['o'], incoming: [] })
  })

  it('accepts one side, or both in either order', () => {
    const t = conflictText(linesOf(merged), first)
    assert.deepEqual(resolvedLines(t, 'current'), ['ours'])
    assert.deepEqual(resolvedLines(t, 'incoming'), ['theirs'])
    assert.deepEqual(resolvedLines(t, 'both'), ['ours', 'theirs'])
    assert.deepEqual(resolvedLines(t, 'both-incoming-first'), ['theirs', 'ours'])
  })

  it('replaces the whole block, markers included', () => {
    assert.equal(apply(merged, first, ['ours', 'theirs']), merged.replace('<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\n', 'ours\ntheirs\n'))
  })

  it('removes the lines of an empty resolution', () => {
    assert.equal(apply('a\n<<<<<<< H\n=======\n>>>>>>> f\nb', { open: 2, base: null, sep: 3, close: 4 }, []), 'a\nb')
    // Last line of the file: the line break before it goes too.
    assert.equal(apply('a\n<<<<<<< H\n=======\n>>>>>>> f', { open: 2, base: null, sep: 3, close: 4 }, []), 'a')
    assert.equal(apply('a\n<<<<<<< H\nx\n=======\n>>>>>>> f', { open: 2, base: null, sep: 4, close: 5 }, ['x']), 'a\nx')
  })

  it('uses the document line break', () => {
    const doc = linesOf('<<<<<<< H\nx\n=======\ny\n>>>>>>> f\nz')
    assert.equal(replaceConflict(doc, findConflicts(doc)[0], ['x', 'y'], '\r\n').insert, 'x\r\ny\r\n')
  })
})

describe('navigation', () => {
  const conflicts = findConflicts(linesOf(merged))

  it('finds the conflict at a line', () => {
    assert.equal(conflictAt(conflicts, 1), -1)
    assert.equal(conflictAt(conflicts, 4), 0)
    assert.equal(conflictAt(conflicts, 13), 1)
  })

  it('moves to the next and previous conflict, wrapping around', () => {
    assert.equal(nextConflict(conflicts, 1, 1), 0)
    assert.equal(nextConflict(conflicts, 2, 1), 1)
    assert.equal(nextConflict(conflicts, 9, 1), 0)
    assert.equal(nextConflict(conflicts, 14, -1), 1)
    assert.equal(nextConflict(conflicts, 9, -1), 0)
    assert.equal(nextConflict(conflicts, 3, -1), 1)
    assert.equal(nextConflict([], 3, 1), -1)
  })
})

describe('baseFor', () => {
  it('keeps a base the markers already have', () => {
    assert.deepEqual(baseFor({ current: ['x'], base: ['o'], incoming: [] }, []), ['o'])
  })

  // git merge-file --diff3 keeps two conflicts that git's default style joins into one.
  const diff3 = [
    'func greet() {',
    '<<<<<<< current',
    '  a-main',
    '||||||| base',
    '  a',
    '=======',
    '  a-feature',
    '>>>>>>> incoming',
    '}',
    '',
    '<<<<<<< current',
    '  b-main',
    '||||||| base',
    '  b',
    '=======',
    '  b-feature',
    '>>>>>>> incoming',
    'end',
    '',
  ].join('\r\n')

  it('splits a diff3 merge into common stretches and conflicts', () => {
    assert.deepEqual(diff3Segments(diff3), [
      { common: ['func greet() {'] },
      { current: ['  a-main'], base: ['  a'], incoming: ['  a-feature'] },
      { common: ['}', ''] },
      { current: ['  b-main'], base: ['  b'], incoming: ['  b-feature'] },
      { common: ['end'] },
    ])
  })

  it('finds the base of a single conflict and of joined ones', () => {
    const segments = diff3Segments(diff3)
    assert.deepEqual(baseFor({ current: ['  b-main'], base: null, incoming: ['  b-feature'] }, segments), ['  b'])
    const joined = { current: ['  a-main', '}', '', '  b-main'], base: null, incoming: ['  a-feature', '}', '', '  b-feature'] }
    assert.deepEqual(baseFor(joined, segments), ['  a', '}', '', '  b'])
    assert.equal(baseFor({ current: ['  a-main'], base: null, incoming: ['edited by hand'] }, segments), null)
    assert.equal(baseFor({ current: ['x'], base: null, incoming: [] }, []), null)
  })
})

describe('conflictPrompt', () => {
  const range = { open: 2, base: null, sep: 4, close: 6 }
  const input = {
    rel: 'src/a.go',
    index: 0,
    total: 2,
    range,
    text: { current: ['ours'], base: null, incoming: ['theirs'] },
    base: ['orig'],
    operation: 'rebasing',
    current: { label: 'main', detail: 'the branch you are rebasing onto' },
    incoming: { label: 'feature', detail: 'your commit abc “Fix”' },
  }

  it('names the conflict, both sides and the base', () => {
    const p = conflictPrompt(input)
    assert.match(p, /^Resolve merge conflict 1 of 2 in src\/a\.go \(lines 2–6\), left by a rebase\./)
    assert.match(p, /Current \(main\): the branch you are rebasing onto\n\n```\nours\n```/)
    assert.match(p, /Incoming \(feature\): your commit abc “Fix”\n\n```\ntheirs\n```/)
    assert.match(p, /Base \(common ancestor\):\n\n```\norig\n```/)
  })

  it('says where the base is when it is not known', () => {
    assert.match(conflictPrompt({ ...input, base: null, operation: '' }), /git show :1:src\/a\.go/)
    assert.match(conflictPrompt({ ...input, base: null, operation: '' }), /left by applying changes/)
  })
})
