import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { isNewlineKey, macLineKey, terminalClipboardKey, type TerminalKey } from './terminalKeys'

const key = (code: string, mods: Partial<TerminalKey> = {}): TerminalKey => ({
  code, ctrlKey: true, shiftKey: true, altKey: false, metaKey: false, ...mods,
})

const enter = (mods: Partial<TerminalKey> = {}): TerminalKey => ({
  key: 'Enter', code: 'Enter', ctrlKey: false, shiftKey: true, altKey: false, metaKey: false, ...mods,
})

describe('isNewlineKey', () => {
  it('is Shift+Enter', () => {
    assert.equal(isNewlineKey(enter()), true)
  })

  it('leaves plain Enter (submit) and other Enter chords to the terminal', () => {
    assert.equal(isNewlineKey(enter({ shiftKey: false })), false)
    assert.equal(isNewlineKey(enter({ metaKey: true })), false)
    assert.equal(isNewlineKey(enter({ ctrlKey: true })), false)
    assert.equal(isNewlineKey(enter({ altKey: true })), false)
  })

  it('ignores other keys with Shift', () => {
    assert.equal(isNewlineKey(enter({ key: 'A', code: 'KeyA' })), false)
  })
})

describe('terminalClipboardKey', () => {
  it('copies and pastes with Ctrl+Shift+C / V', () => {
    assert.equal(terminalClipboardKey(key('KeyC'), true), 'copy')
    assert.equal(terminalClipboardKey(key('KeyV'), false), 'paste')
  })

  it('leaves the clipboard alone when nothing is selected', () => {
    assert.equal(terminalClipboardKey(key('KeyC'), false), 'none')
  })

  it('sends everything else to the terminal (Ctrl+C is an interrupt)', () => {
    assert.equal(terminalClipboardKey(key('KeyC', { shiftKey: false }), true), null)
    assert.equal(terminalClipboardKey(key('KeyP'), true), null)
    assert.equal(terminalClipboardKey(key('KeyC', { altKey: true }), true), null)
  })
})

describe('macLineKey', () => {
  const cmd = (k: string, mods: Partial<TerminalKey> = {}): TerminalKey => ({
    key: k, code: k, ctrlKey: false, shiftKey: false, altKey: false, metaKey: true, ...mods,
  })

  it('moves to the start and end of the line with ⌘← and ⌘→', () => {
    assert.deepEqual(macLineKey(cmd('ArrowLeft')), { send: '\x01' })
    assert.deepEqual(macLineKey(cmd('ArrowRight')), { send: '\x05' })
  })

  it('scrolls to the top and bottom of the scrollback with ⌘↑ and ⌘↓', () => {
    assert.deepEqual(macLineKey(cmd('ArrowUp')), { scroll: 'top' })
    assert.deepEqual(macLineKey(cmd('ArrowDown')), { scroll: 'bottom' })
  })

  it('leaves plain arrows, other ⌘ keys and chords with more modifiers alone', () => {
    assert.equal(macLineKey(cmd('ArrowLeft', { metaKey: false })), null)
    assert.equal(macLineKey(cmd('k')), null)
    assert.equal(macLineKey(cmd('ArrowLeft', { shiftKey: true })), null)
    assert.equal(macLineKey(cmd('ArrowRight', { altKey: true })), null)
    assert.equal(macLineKey(cmd('ArrowUp', { ctrlKey: true })), null)
  })
})
