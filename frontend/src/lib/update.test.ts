import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceUpdate, offerable, offerText, upToDateText, type UpdateResult } from './update'

const result = (over: Partial<UpdateResult> = {}): UpdateResult => ({
  current: 'v1.2.0',
  latest: { tag: 'v1.3.0', name: 'Flint 1.3', url: 'https://github.com/ODudek/flint/releases/tag/v1.3.0', publishedAt: '2026-10-30T09:15:00Z' },
  available: true,
  dev: false,
  skipped: false,
  ...over,
})

describe('update notice', () => {
  it('reads a result from Go', () => {
    assert.deepEqual(coerceUpdate(result()), result())
    assert.deepEqual(coerceUpdate({ latest: { tag: 'v1.3.0' }, available: 'yes' }), {
      current: '',
      latest: { tag: 'v1.3.0', name: '', url: '', publishedAt: '' },
      available: false,
      dev: false,
      skipped: false,
    })
  })

  it('rejects anything else', () => {
    for (const v of [null, undefined, 'v1.3.0', [], {}, { latest: null }, { latest: { tag: '' } }, { latest: { tag: 3 } }]) {
      assert.equal(coerceUpdate(v), null, JSON.stringify(v))
    }
  })

  it('offers newer releases of release builds only', () => {
    assert.equal(offerable(result()), true)
    assert.equal(offerable(result({ skipped: true })), true) // asked for explicitly
    assert.equal(offerable(result({ available: false })), false)
    assert.equal(offerable(result({ dev: true })), false)
  })

  it('says what was found', () => {
    assert.equal(offerText(result()), 'Flint v1.3.0 is available (you have v1.2.0)')
    assert.equal(upToDateText(result({ current: 'v1.3.0', available: false })), "You're up to date (v1.3.0)")
    assert.equal(
      upToDateText(result({ current: 'dev-0123456789ab', dev: true, available: false })),
      'This is a development build (dev-0123456789ab); the latest release is v1.3.0',
    )
  })
})
