import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { activeThemeId, customThemeFrom, groupThemes } from './contributedThemes'
import { contribute, useContributions } from './contributions'
import { coerceSettings } from './settings'
import type { ThemeContribution } from './features'

const nord: ThemeContribution = {
  id: 'me.nord.dark', label: 'Nord', group: 'Nord', base: 'dark',
  colors: { '--bg-editor': '#2e3440' }, terminal: { red: '#bf616a' },
}
const nordLight: ThemeContribution = { ...nord, id: 'me.nord.light', label: 'Nord Light', base: 'light', terminal: {} }
const solar: ThemeContribution = { ...nord, id: 'me.solar.dark', label: 'Solarized', group: 'Solarized' }

describe('contributed themes', () => {
  it('become a custom theme that survives coercion unchanged', () => {
    const custom = customThemeFrom(nord)
    assert.deepEqual(custom, { name: 'Nord', base: 'dark', colors: { '--bg-editor': '#2e3440' }, terminal: { red: '#bf616a' }, source: 'me.nord.dark' })
    assert.notEqual(custom.colors, nord.colors)
    assert.deepEqual(coerceSettings({ theme: 'custom', customTheme: custom }).customTheme, custom)
  })

  it('know which one is in use', () => {
    const themes = [nord, nordLight]
    const s = coerceSettings({ theme: 'custom', customTheme: customThemeFrom(nordLight) })
    assert.equal(activeThemeId(s, themes), 'me.nord.light')
    assert.equal(activeThemeId({ ...s, theme: 'dark' }, themes), '')
    assert.equal(activeThemeId(s, [nord]), '') // its extension was uninstalled
    assert.equal(activeThemeId(coerceSettings({ theme: 'custom', customTheme: { colors: {} } }), themes), '')
  })

  it('group by who offers them, in order', () => {
    assert.deepEqual(groupThemes([nord, solar, nordLight]).map((g) => [g.group, g.themes.map((t) => t.id)]), [
      ['Nord', ['me.nord.dark', 'me.nord.light']],
      ['Solarized', ['me.solar.dark']],
    ])
  })

  it('are a contribution kind with unique ids', () => {
    const off = contribute('extension:me.nord', { themes: [nord, nordLight] })
    try {
      assert.deepEqual(useContributions.getState().themes.map((t) => t.id), ['me.nord.dark', 'me.nord.light'])
      assert.throws(() => contribute('extension:copy', { themes: [nord] }), /duplicate themes id/)
    } finally {
      off()
    }
    assert.equal(useContributions.getState().themes.length, 0)
  })
})
