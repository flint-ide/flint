import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { escapeKey } from './editorAiKeys'

describe('escapeKey', () => {
  const setup = (suggestion: boolean, edit: boolean) => {
    const calls: string[] = []
    const esc = escapeKey(
      () => (calls.push('dismiss'), suggestion),
      () => (calls.push('close'), edit),
    )
    return { esc, calls }
  }

  it('dismisses a suggestion before it touches a running inline edit', () => {
    const { esc, calls } = setup(true, true)
    assert.equal(esc(null), true)
    assert.deepEqual(calls, ['dismiss'])
  })

  it('closes the inline edit when no suggestion is shown', () => {
    const { esc, calls } = setup(false, true)
    assert.equal(esc(null), true)
    assert.deepEqual(calls, ['dismiss', 'close'])
  })

  it('leaves Esc to the editor when neither is active', () => {
    assert.equal(setup(false, false).esc(null), false)
  })
})
