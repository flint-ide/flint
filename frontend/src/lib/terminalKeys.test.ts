import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { isNewlineKey, terminalClipboardKey, type TerminalKey } from './terminalKeys'

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
