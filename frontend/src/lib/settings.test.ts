import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceSettings, DEFAULT_SETTINGS, editorCssVars, parseExcludes, resolveTheme, themeCssVars } from './settings'
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
    const s = { ...DEFAULT_SETTINGS, editor: { fontFamily: 'Menlo', fontSize: 15, lineHeight: 1.4, vimMode: false } }
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
    assert.equal(s.checkForUpdates, true)
    assert.equal(coerceSettings({ checkForUpdates: false }).checkForUpdates, false)
  })

  it('replaces invalid values', () => {
    const s = coerceSettings({
      theme: 'neon',
      editor: { fontFamily: 'x; } body {', fontSize: 200, lineHeight: '2' },
      autoSave: 'yes',
      checkForUpdates: 0,
      keymap: { 'git.push': 'Mod+K', 'git.pull': 3 },
    })
    assert.equal(s.theme, 'dark')
    assert.deepEqual(s.editor, DEFAULT_SETTINGS.editor)
    assert.equal(s.autoSave, false)
    assert.equal(s.checkForUpdates, true)
    assert.deepEqual(s.keymap, { 'git.push': 'Mod+K' })
  })

  it('falls back to the defaults for garbage', () => {
    assert.equal(coerceSettings(null), DEFAULT_SETTINGS)
    assert.equal(coerceSettings([1, 2]), DEFAULT_SETTINGS)
  })
})

describe('custom theme', () => {
  const custom = { name: 'Acme Night', base: 'light', colors: { '--bg-editor': '#1b1d23', '--syn-keyword': '#e06c75aa' } }

  it('resolves to its base palette', () => {
    assert.equal(resolveTheme('custom', true, 'light'), 'light')
    assert.equal(resolveTheme('custom', false), 'dark')
  })

  it('sets its colors only while Theme is Custom', () => {
    const s = coerceSettings({ theme: 'custom', customTheme: custom })
    assert.equal(s.theme, 'custom')
    assert.deepEqual(themeCssVars(s), custom.colors)
    assert.deepEqual(themeCssVars({ ...s, theme: 'dark' }), {})
  })

  it('drops colors that are not hex or not CSS variables', () => {
    const s = coerceSettings({ customTheme: { name: 3, base: 'neon', colors: { '--bg': 'red; }', 'color': '#fff', '--fg': '#ABCDEF', '--editor-font-size': '#123' } } })
    assert.deepEqual(s.customTheme, { name: '', base: 'dark', colors: { '--fg': '#ABCDEF' } })
  })

  it('recolors terminals from the custom colors', () => {
    const font = DEFAULT_SETTINGS.terminal
    const t = terminalOptions(font, 'light', 0, custom.colors).theme
    assert.equal(t.background, '#1b1d23')
    assert.equal(t.cursorAccent, '#1b1d23')
    assert.equal(t.red, TERMINAL_THEMES.light.red)
  })
})

describe('vim mode', () => {
  it('is off by default and read from the editor section', () => {
    assert.equal(coerceSettings({}).editor.vimMode, false)
    assert.equal(coerceSettings({ editor: { vimMode: true } }).editor.vimMode, true)
    assert.equal(coerceSettings({ editor: { vimMode: 'yes' } }).editor.vimMode, false)
  })
})

describe('TODO excludes', () => {
  it('takes one pattern per line, trimmed and unique', () => {
    assert.deepEqual(parseExcludes(' docs/legacy \n\n*.min.js\ndocs/legacy\n'), { patterns: ['docs/legacy', '*.min.js'], invalid: [] })
  })

  it('reports what Go would refuse', () => {
    assert.deepEqual(parseExcludes('!src\na,b\ngen/').invalid, ['!src', 'a,b'])
  })

  it('drops invalid patterns from untrusted settings', () => {
    assert.deepEqual(coerceSettings({ todoExclude: ['ok', '!no', 3] }).todoExclude, ['ok'])
    assert.deepEqual(coerceSettings({}).todoExclude, [])
  })
})
