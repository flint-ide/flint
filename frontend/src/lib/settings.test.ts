import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceSettings, DEFAULT_SETTINGS, editorCssVars, resolveTheme } from './settings'
import { TERMINAL_THEMES, terminalOptions } from './terminalTheme'

describe('theme resolution', () => {
  it('uses an explicit theme as is', () => {
    assert.equal(resolveTheme('dark', false), 'dark')
    assert.equal(resolveTheme('light', true), 'light')
  })

  it('follows macOS for System', () => {
    assert.equal(resolveTheme('system', true), 'dark')
    assert.equal(resolveTheme('system', false), 'light')
  })

  it('gives terminals the palette of the resolved theme', () => {
    const font = DEFAULT_SETTINGS.terminal
    assert.equal(terminalOptions(font, 'light').theme, TERMINAL_THEMES.light)
    assert.equal(terminalOptions(font, 'dark').theme.background, '#141518')
    assert.equal(terminalOptions({ ...font, fontSize: 14 }, 'dark', -1).fontSize, 13)
    assert.equal(terminalOptions({ ...font, fontSize: 6 }, 'dark', -1).fontSize, 6)
  })

  it('defines every terminal color in both palettes', () => {
    assert.deepEqual(Object.keys(TERMINAL_THEMES.light).sort(), Object.keys(TERMINAL_THEMES.dark).sort())
  })
})

describe('editor variables', () => {
  it('maps the editor font to CSS custom properties', () => {
    const s = { ...DEFAULT_SETTINGS, editor: { fontFamily: 'Menlo', fontSize: 15, lineHeight: 1.4 } }
    assert.deepEqual(editorCssVars(s), {
      '--editor-font': 'Menlo',
      '--editor-font-size': '15px',
      '--editor-line-height': '1.4',
    })
  })
})

describe('coerceSettings', () => {
  it('fills missing keys with defaults', () => {
    const s = coerceSettings({ theme: 'light', editor: { fontSize: 16 } })
    assert.equal(s.theme, 'light')
    assert.equal(s.editor.fontSize, 16)
    assert.equal(s.editor.fontFamily, DEFAULT_SETTINGS.editor.fontFamily)
    assert.deepEqual(s.terminal, DEFAULT_SETTINGS.terminal)
    assert.deepEqual(s.keymap, {})
  })

  it('replaces invalid values', () => {
    const s = coerceSettings({
      theme: 'neon',
      editor: { fontFamily: 'x; } body {', fontSize: 200, lineHeight: '2' },
      autoSave: 'yes',
      keymap: { 'git.push': 'Mod+K', 'git.pull': 3 },
    })
    assert.equal(s.theme, 'dark')
    assert.deepEqual(s.editor, DEFAULT_SETTINGS.editor)
    assert.equal(s.autoSave, false)
    assert.deepEqual(s.keymap, { 'git.push': 'Mod+K' })
  })

  it('falls back to the defaults for garbage', () => {
    assert.equal(coerceSettings(null), DEFAULT_SETTINGS)
    assert.equal(coerceSettings([1, 2]), DEFAULT_SETTINGS)
  })
})
