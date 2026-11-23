import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { ICONS, isIconName } from './icons'

describe('icons', () => {
  it('maps every name to a component', () => {
    assert.equal(Object.keys(ICONS).length, 17)
    for (const [name, glyph] of Object.entries(ICONS)) {
      // Lucide icons are forwardRef objects (or functions), never undefined.
      assert.ok(typeof glyph === 'function' || (typeof glyph === 'object' && glyph !== null), name)
    }
  })

  it('knows its names', () => {
    for (const name of Object.keys(ICONS)) assert.ok(isIconName(name), name)
    assert.ok(isIconName('terminal'))
    assert.ok(isIconName('workflow'))
    assert.ok(isIconName('board'))
    assert.notEqual(ICONS.board, ICONS.checkbox)
    assert.ok(!isIconName('⌕'))
    assert.ok(!isIconName('toString'))
    assert.ok(!isIconName(''))
  })
})
