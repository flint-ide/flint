import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { vimModeLabel, vimStateLabel } from './vimMode'

describe('vim mode labels', () => {
  it('names the modes of mode-change events', () => {
    assert.equal(vimModeLabel({ mode: 'normal' }), 'NORMAL')
    assert.equal(vimModeLabel({ mode: 'insert' }), 'INSERT')
    assert.equal(vimModeLabel({ mode: 'replace' }), 'REPLACE')
    assert.equal(vimModeLabel({ mode: 'visual', subMode: '' }), 'VISUAL')
    assert.equal(vimModeLabel({ mode: 'visual', subMode: 'linewise' }), 'VISUAL LINE')
    assert.equal(vimModeLabel({ mode: 'visual', subMode: 'blockwise' }), 'VISUAL BLOCK')
    assert.equal(vimModeLabel({ mode: '' }), 'NORMAL')
  })

  it('reads the mode of a focused editor from its state', () => {
    assert.equal(vimStateLabel(null), 'NORMAL')
    assert.equal(vimStateLabel({ insertMode: true }), 'INSERT')
    assert.equal(vimStateLabel({ visualMode: true, visualLine: true }), 'VISUAL LINE')
    assert.equal(vimStateLabel({ visualMode: true, visualBlock: true }), 'VISUAL BLOCK')
    assert.equal(vimStateLabel({ visualMode: true }), 'VISUAL')
  })
})
