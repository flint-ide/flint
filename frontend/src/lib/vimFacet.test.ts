import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { EditorState } from '@codemirror/state'
import { vimFileCommands } from './vimFacet'

describe('vim file commands', () => {
  it('are off unless the editor opts in', () => {
    assert.equal(EditorState.create({}).facet(vimFileCommands), false)
    assert.equal(EditorState.create({ extensions: vimFileCommands.of(true) }).facet(vimFileCommands), true)
  })
})
