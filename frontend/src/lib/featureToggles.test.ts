import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { FeatureModule } from './features'
import { coerceToggles, featureOn, initialActive, pendingRestart, withFeature } from './featureToggles'

const mods: FeatureModule[] = [{ id: 'database', optional: true }, { id: 'todo' }, { id: 'acme', optional: true }]

describe('feature toggles', () => {
  it('keeps boolean flags of valid ids only', () => {
    assert.deepEqual(coerceToggles({ enabled: { database: false, 'Bad Id': false, acme: 'no' } }), { enabled: { database: false } })
    assert.deepEqual(coerceToggles(null), { enabled: {} })
    assert.deepEqual(coerceToggles({ enabled: [] }), { enabled: {} })
  })

  it('switches optional features only, defaulting to on', () => {
    const t = { enabled: { database: false, todo: false } }
    assert.equal(featureOn(t, mods[0]), false)
    assert.equal(featureOn(t, mods[1]), true)
    assert.equal(featureOn(t, mods[2]), true)
  })

  it('stores only what is off', () => {
    const off = withFeature({ enabled: {} }, 'acme', false)
    assert.deepEqual(off, { enabled: { acme: false } })
    assert.deepEqual(withFeature(off, 'acme', true), { enabled: {} })
  })

  it('starts with what Go ran last time, else with the settings', () => {
    assert.deepEqual([...initialActive(mods, ['acme'], { enabled: {} })], ['todo', 'acme'])
    assert.deepEqual([...initialActive(mods, null, { enabled: { database: false } })], ['todo', 'acme'])
  })

  it('lists the switches that wait for a restart', () => {
    const active = new Set(['database', 'todo'])
    assert.deepEqual(pendingRestart(mods, active, { enabled: { database: false } }), ['database', 'acme'])
    assert.deepEqual(pendingRestart(mods, active, { enabled: { acme: false } }), [])
  })
})
