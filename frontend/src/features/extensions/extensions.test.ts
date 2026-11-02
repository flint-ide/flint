import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceExtensions, themeContributions } from './extensions'

const fromGo = [
  {
    id: 'me.nord', name: 'Nord', version: '1.0.0', publisher: 'Me', description: 'Arctic colors.', dir: '/x/me.nord', permissions: [],
    program: false, contributes: ['2 color themes'],
    themes: [
      { id: 'me.nord.dark', label: 'Nord', base: 'dark', colors: { '--bg-editor': '#2e3440', '--evil': '#fff' }, terminal: { red: '#bf616a', pink: '#f0f' } },
      { id: 'me.nord.light', label: 'Nord Light', base: 'light', colors: { '--bg-editor': 'url(x)' }, terminal: {} },
    ],
  },
  { id: 'me.bad', dir: '/x/me.bad', error: 'built for contract 7', permissions: [], contributes: [], themes: [] },
  { name: 'no id' },
  'junk',
]

describe('installed extensions', () => {
  it('are read from untrusted JSON, filtering colors like a custom theme', () => {
    const list = coerceExtensions(fromGo)
    assert.deepEqual(list.map((e) => e.id), ['me.nord', 'me.bad'])
    const [nord, bad] = list
    assert.deepEqual(nord.themes[0].colors, { '--bg-editor': '#2e3440' })
    assert.deepEqual(nord.themes[0].terminal, { red: '#bf616a' })
    assert.deepEqual(nord.themes[1].colors, {})
    assert.equal(nord.themes[1].base, 'light')
    assert.equal(bad.name, 'me.bad')
    assert.equal(bad.error, 'built for contract 7')
    assert.deepEqual(coerceExtensions(null), [])
  })

  it('contribute their themes, grouped under their name, unless disabled', () => {
    const [nord, bad] = coerceExtensions(fromGo)
    assert.deepEqual(themeContributions(nord).map((t) => [t.id, t.group, t.base]), [
      ['me.nord.dark', 'Nord', 'dark'],
      ['me.nord.light', 'Nord', 'light'],
    ])
    assert.deepEqual(themeContributions({ ...nord, error: 'broken' }), [])
    assert.deepEqual(themeContributions(bad), [])
  })
})
