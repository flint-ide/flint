import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { EditorState } from '@codemirror/state'
import { mergeConflicts } from './mergeDecorations'

const doc = ['a', '<<<<<<< HEAD', 'ours', '=======', 'theirs', '>>>>>>> feature', 'b'].join('\n')

describe('mergeConflicts', () => {
  const noop = { resolve: () => {}, askAgent: () => {} }

  it('decorates each conflict and follows edits', () => {
    const merge = mergeConflicts({ current: 'main', incoming: 'feature' }, noop)
    let state = EditorState.create({ doc, extensions: merge.extension })
    const value = state.field(merge.field)
    assert.equal(value.conflicts.length, 1)
    // Action bar, three markers, two tags and one line of each side.
    assert.equal(value.decorations.size, 8)

    // Deleting the closing marker by hand ends the conflict.
    const close = state.doc.line(6)
    state = state.update({ changes: { from: close.from - 1, to: close.to } }).state
    assert.equal(state.field(merge.field).conflicts.length, 0)
    assert.equal(state.field(merge.field).decorations.size, 0)
  })
})
