import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { bootSidebarWidth, bootTheme } from './bootTheme'

describe('bootTheme', () => {
  it('follows the cached theme', () => {
    assert.equal(bootTheme('{"theme":"light"}', true), 'light')
    assert.equal(bootTheme('{"theme":"dark"}', false), 'dark')
  })

  it('resolves System with the macOS appearance', () => {
    assert.equal(bootTheme('{"theme":"system"}', true), 'dark')
    assert.equal(bootTheme('{"theme":"system"}', false), 'light')
  })

  it('uses the custom theme base', () => {
    assert.equal(bootTheme('{"theme":"custom","customTheme":{"base":"light"}}', true), 'light')
    assert.equal(bootTheme('{"theme":"custom","customTheme":{}}', false), 'dark')
  })

  it('falls back to dark on missing or broken input', () => {
    for (const raw of [null, '', 'not json', '42', '{"theme":"neon"}', 'null']) {
      assert.equal(bootTheme(raw, false), 'dark', String(raw))
    }
  })
})

describe('bootSidebarWidth', () => {
  it('keeps sensible widths and drops the rest', () => {
    assert.equal(bootSidebarWidth('312'), 312)
    assert.equal(bootSidebarWidth('280.6'), 281)
    for (const raw of [null, '', 'abc', '10', '99999', 'Infinity']) assert.equal(bootSidebarWidth(raw), null, String(raw))
  })
})
