import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { ICONS, isIconName } from './icons'

describe('icons', () => {
  it('draws every icon with absolute path commands only', () => {
    for (const [name, paths] of Object.entries(ICONS)) {
      assert.ok(paths.length > 0, name)
      for (const d of paths) assert.match(d, /^M[-\d.]/, `${name}: ${d}`)
      for (const d of paths) assert.doesNotMatch(d, /NaN|undefined/, `${name}: ${d}`)
    }
  })

  it('knows its names', () => {
    assert.ok(isIconName('terminal'))
    assert.ok(!isIconName('⌕'))
  })
})
