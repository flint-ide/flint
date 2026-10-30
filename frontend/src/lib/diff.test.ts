import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { EditorState, Text } from '@codemirror/state'
import type { Hunk } from '../api/backend'
import { diffSide, foldsFor } from './diffDecorations'
import { countLines, previewLines } from './editDiff'
import { markerSet } from './gitGutter'

const hunk = (fromA: number, toA: number, fromB: number, toB: number): Hunk => ({ fromA, toA, fromB, toB }) as Hunk

describe('foldsFor', () => {
  const opts = { margin: 3, minSize: 6 }

  it('folds long unchanged stretches but keeps a margin around changes', () => {
    // 30 lines, one change at line 15 (0-based) on both sides.
    const folds = foldsFor([hunk(15, 16, 15, 16)], 30, opts)
    assert.deepEqual(folds, [
      { a: 0, b: 0, lines: 12 },
      { a: 19, b: 19, lines: 11 },
    ])
  })

  it('shifts side B when lines were inserted above', () => {
    const folds = foldsFor([hunk(2, 2, 2, 5)], 20, opts)
    assert.deepEqual(folds, [{ a: 5, b: 8, lines: 15 }])
  })

  it('leaves short stretches alone', () => {
    assert.deepEqual(foldsFor([hunk(3, 4, 3, 4)], 8, opts), [])
  })

  it('folds an identical file entirely', () => {
    assert.deepEqual(foldsFor([], 10, opts), [{ a: 0, b: 0, lines: 10 }])
  })
})

describe('diffSide', () => {
  it('builds decorations without throwing for edge hunks', () => {
    const doc = 'a\nb\nc'
    const hunks = [hunk(0, 1, 0, 3), hunk(3, 3, 5, 6)]
    for (const side of ['a', 'b'] as const) {
      const state = EditorState.create({ doc, extensions: diffSide(side, hunks, [], () => undefined) })
      assert.equal(state.doc.lines, 3)
    }
  })
})

describe('editDiff', () => {
  it('counts added and deleted lines across diffs', () => {
    assert.deepEqual(countLines([[hunk(1, 2, 1, 4)], [hunk(0, 3, 0, 0)]]), { add: 3, del: 4 })
  })

  it('previews deleted then added lines with separators', () => {
    const d = { path: 'x', oldText: 'a\nb\nc\nd', newText: 'a\nB\nc\nD\nE' }
    const lines = previewLines(d, [hunk(1, 2, 1, 2), hunk(3, 4, 3, 5)], 80)
    assert.deepEqual(
      lines.map((l) => `${l.kind}:${l.text}`),
      ['del:b', 'add:B', 'sep:⋯', 'del:d', 'add:D', 'add:E'],
    )
  })
})

describe('markerSet', () => {
  it('places markers on line starts and drops lines past the end', () => {
    const doc = Text.of(['one', 'two', 'three'])
    const set = markerSet(doc, [
      { line: 2, kind: 'modified' },
      { line: 9, kind: 'added' },
    ])
    const found: number[] = []
    set.between(0, doc.length, (from) => void found.push(from))
    assert.deepEqual(found, [4])
  })
})
