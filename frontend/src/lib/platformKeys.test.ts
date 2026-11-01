import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  canonicalKeys,
  commandForKey,
  formatKeys,
  keyHint,
  keysFromEvent,
  matchesKeys,
  parseKeys,
  registerCommand,
  TERMINAL,
  terminalOwnsKey,
  VIM_EDITOR,
  vimOwnsKey,
  type KeyEventLike,
} from './commands'
import { isMac, modKey, modifierName, setMac } from './platform'

const key = (k: string, mods: Partial<Omit<KeyEventLike, 'key'>> = {}): KeyEventLike => ({
  key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...mods,
})
const noop = () => undefined

afterEach(() => setMac(true))

describe('platform conventions', () => {
  it('defaults to macOS outside a browser', () => {
    assert.equal(isMac(), true)
  })

  it("maps 'Mod' to ⌘ on macOS and to Ctrl elsewhere", () => {
    assert.equal(parseKeys('Mod+P').meta, true)
    setMac(false)
    const c = parseKeys('Mod+P')
    assert.equal(c.ctrl, true)
    assert.equal(c.meta, false)
    assert.equal(parseKeys('Meta+P').meta, true)
    assert.ok(matchesKeys(key('p', { ctrlKey: true }), 'Mod+P'))
    assert.ok(!matchesKeys(key('p', { metaKey: true }), 'Mod+P'))
  })

  it('formats shortcuts as glyphs on macOS and as words elsewhere', () => {
    assert.equal(formatKeys('Mod+Shift+P'), '⇧⌘P')
    setMac(false)
    assert.equal(formatKeys('Mod+Shift+P'), 'Ctrl+Shift+P')
    assert.equal(formatKeys('Alt+Mod+Enter'), 'Ctrl+Alt+Enter')
    assert.equal(formatKeys('Mod+Backspace'), 'Ctrl+Backspace')
    assert.equal(formatKeys('Shift+F10'), 'Shift+F10')
    assert.equal(formatKeys('Meta+K'), 'Super+K')
  })

  it('records and compares shortcuts with Ctrl as Mod off macOS', () => {
    setMac(false)
    assert.equal(keysFromEvent(key('k', { ctrlKey: true, shiftKey: true, code: 'KeyK' })), 'Mod+Shift+K')
    assert.equal(canonicalKeys('shift+ctrl+k'), 'Mod+Shift+K')
  })

  it('names modifiers per platform', () => {
    assert.equal(modifierName('Alt'), '⌥')
    assert.ok(modKey({ metaKey: true, ctrlKey: false }))
    setMac(false)
    assert.equal(modifierName('Alt'), 'Alt')
    assert.equal(modifierName('Mod'), 'Ctrl')
    assert.ok(modKey({ metaKey: false, ctrlKey: true }))
    assert.ok(!modKey({ metaKey: true, ctrlKey: false }))
  })

  it('leaves Ctrl+letter to the shell in a Linux terminal, except Ctrl+J', () => {
    assert.equal(terminalOwnsKey(key('c', { ctrlKey: true })), false) // macOS: ⌘ is Flint's, ⌃ never was
    setMac(false)
    assert.ok(terminalOwnsKey(key('c', { ctrlKey: true })))
    assert.ok(terminalOwnsKey(key('r', { ctrlKey: true })))
    assert.ok(terminalOwnsKey(key('v', { ctrlKey: true, shiftKey: true, code: 'KeyV' })))
    assert.ok(!terminalOwnsKey(key('j', { ctrlKey: true })))
    assert.ok(!terminalOwnsKey(key('p', { ctrlKey: true, shiftKey: true, code: 'KeyP' })))

    const off = registerCommand({ id: 't.quickOpen', title: 'Quick Open', keys: 'Mod+P', run: noop })
    try {
      const inTerminal = (selector: string) => selector === TERMINAL
      assert.equal(commandForKey(key('p', { ctrlKey: true }), inTerminal), undefined)
      assert.equal(commandForKey(key('p', { ctrlKey: true }), () => false)?.id, 't.quickOpen')
    } finally {
      off()
    }
  })

  it('lets a Linux Vim editor keep only its essential Ctrl keys', () => {
    assert.ok(vimOwnsKey(key('p', { ctrlKey: true })))
    setMac(false)
    assert.ok(vimOwnsKey(key('r', { ctrlKey: true })))
    assert.ok(vimOwnsKey(key('[', { ctrlKey: true })))
    assert.ok(!vimOwnsKey(key('p', { ctrlKey: true })))
    const off = registerCommand({ id: 't.save', title: 'Save', keys: 'Mod+S', run: noop })
    try {
      const inVim = (selector: string) => selector === VIM_EDITOR
      assert.equal(commandForKey(key('s', { ctrlKey: true }), inVim)?.id, 't.save')
    } finally {
      off()
    }
  })

  it("shows a command's current shortcut", () => {
    const off = registerCommand({ id: 't.run', title: 'Run', keys: 'Shift+F10', run: noop })
    try {
      assert.equal(keyHint('t.run'), '⇧F10')
      setMac(false)
      assert.equal(keyHint('t.run'), 'Shift+F10')
      assert.equal(keyHint('t.missing'), '')
    } finally {
      off()
    }
  })
})
