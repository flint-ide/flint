import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceSections, onSectionsChanged, registerSections, sectionKeys, type SettingsSection } from './settingsSections'
import { coerceSettings, CORE_KEYS } from './settings'

// Test-only keys: the section registry is typed by FeatureSettings, which the
// features augment; these casts stand in for an augmentation.
const section = (key: string, defaults: unknown, coerce: (v: unknown) => unknown) => ({ key, defaults, coerce }) as unknown as SettingsSection

describe('settings sections', () => {
  const limit = section('zzLimit', 5, (v) => (typeof v === 'number' && v > 0 ? v : 5))
  const flag = section('zzFlag', true, (v) => (typeof v === 'boolean' ? v : true))

  it('fills registered keys from JSON, with defaults for missing or invalid ones', () => {
    const off = registerSections([limit, flag], CORE_KEYS)
    try {
      assert.deepEqual(sectionKeys(), ['zzLimit', 'zzFlag'])
      const s = coerceSettings({ theme: 'light', zzLimit: 9, zzFlag: 'no', zzOther: 1 }) as unknown as Record<string, unknown>
      assert.equal(s.theme, 'light')
      assert.equal(s.zzLimit, 9)
      assert.equal(s.zzFlag, true)
      assert.equal('zzOther' in s, false, 'unknown keys are dropped, as in Go')
      assert.deepEqual(coerceSections({}), { zzLimit: 5, zzFlag: true })
    } finally {
      off()
    }
    assert.deepEqual(sectionKeys(), [])
  })

  it('refuses core keys and a second section for a key, and ignores the same section twice', () => {
    assert.throws(() => registerSections([section('theme', 'x', String)], CORE_KEYS), /core key/)
    const off = registerSections([limit], CORE_KEYS)
    try {
      registerSections([limit], CORE_KEYS) // the same section again: no error
      assert.throws(() => registerSections([section('zzLimit', 1, Number)], CORE_KEYS), /registered twice/)
      assert.deepEqual(sectionKeys(), ['zzLimit'])
    } finally {
      off()
    }
  })

  it('tells listeners when sections come and go', () => {
    let calls = 0
    const stop = onSectionsChanged(() => calls++)
    const off = registerSections([flag], CORE_KEYS)
    off()
    stop()
    registerSections([flag], CORE_KEYS)()
    assert.equal(calls, 2)
  })
})
