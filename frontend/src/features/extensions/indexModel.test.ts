import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { availableUpdates, formatSize, indexRows, kindLabel, updatesSummary } from './indexModel'
import { coerceExtensions, indexSettings, validIndexKey, validIndexUrl, withIndexSetting } from './settings'

const entry = (id: string, version: string, error = '') => ({ id, version, error })

describe('extension index', () => {
  const entries = [entry('flint.stats', '1.2.0'), entry('flint.nord', '1.0.0'), entry('flint.docker', '2.0.0'), entry('flint.aws', '3.0.0', 'needs a newer Flint')]

  it('finds the installed extensions the index has a newer version of', () => {
    const installed = [
      { id: 'flint.stats', version: '1.1.9' }, // older: update
      { id: 'flint.nord', version: '1.0.0' }, // the same
      { id: 'flint.docker', version: '2.1.0' }, // a newer local build
      { id: 'flint.aws', version: '1.0.0' }, // the index's version can't be installed here
      { id: 'me.local', version: '0.1.0' }, // installed from a folder, not in the index
    ]
    assert.deepEqual([...availableUpdates(entries, installed)], [['flint.stats', '1.2.0']])
    assert.deepEqual([...availableUpdates(entries, [])], [])
    assert.deepEqual([...availableUpdates([], installed)], [], 'before the index was fetched nothing is offered')
  })

  it('compares versions numerically', () => {
    assert.deepEqual([...availableUpdates([entry('a.b', '1.10.0')], [{ id: 'a.b', version: '1.9.3' }])], [['a.b', '1.10.0']])
    assert.deepEqual([...availableUpdates([entry('a.b', '1.9.3')], [{ id: 'a.b', version: '1.10.0' }])], [])
    assert.deepEqual([...availableUpdates([entry('a.b', '1.0.0')], [{ id: 'a.b', version: '' }])], [], 'a broken install has no version')
  })

  it('gives each entry a status', () => {
    const rows = indexRows(entries, [
      { id: 'flint.stats', version: '1.1.9' },
      { id: 'flint.nord', version: '1.0.0' },
      { id: 'flint.docker', version: '2.1.0' },
    ])
    assert.deepEqual(
      rows.map((r) => [r.entry.id, r.status, r.installedVersion]),
      [
        ['flint.stats', 'update', '1.1.9'],
        ['flint.nord', 'installed', '1.0.0'],
        ['flint.docker', 'newer', '2.1.0'],
        ['flint.aws', 'broken', ''],
      ],
    )
  })

  it('labels kinds and sizes', () => {
    assert.equal(kindLabel('program'), 'Program')
    assert.equal(kindLabel('themes'), 'Color themes')
    assert.equal(kindLabel('languages'), 'Languages')
    assert.equal(kindLabel('data'), 'Data only')
    assert.equal(formatSize(0), '')
    assert.equal(formatSize(812), '812 B')
    assert.equal(formatSize(14 * 1024), '14 KB')
    assert.equal(formatSize(3.2 * 1024 * 1024), '3.2 MB')
  })

  it('summarizes a check for updates', () => {
    const name = (id: string) => id.split('.')[1]
    assert.match(updatesSummary(new Map(), name), /up to date/)
    assert.equal(updatesSummary(new Map([['flint.stats', '1.2.0']]), name), 'An update is available in Settings ▸ Extensions: stats 1.2.0.')
    assert.match(updatesSummary(new Map([['flint.stats', '1.2.0'], ['flint.nord', '2.0.0']]), name), /^Updates are available .*stats 1\.2\.0, nord 2\.0\.0\.$/)
  })
})

describe('extension index settings', () => {
  const key = `${'A'.repeat(43)}=`

  it('accepts https addresses, and http only on this machine', () => {
    for (const ok of ['', 'https://ext.example.com/index.json', 'http://127.0.0.1:8080/index.json', 'http://localhost/index.json', 'http://[::1]:9/i'])
      assert.equal(validIndexUrl(ok), true, ok)
    for (const bad of ['index.json', 'http://ext.example.com/index.json', 'ftp://ext.example.com/i', 'https://user:pw@ext.example.com/i', 'file:///tmp/index.json', `https://e.com/${'a'.repeat(2000)}`])
      assert.equal(validIndexUrl(bad), false, bad)
  })

  it('accepts a base64 Ed25519 public key', () => {
    assert.equal(validIndexKey(''), true)
    assert.equal(validIndexKey(key), true)
    for (const bad of ['abc', key.slice(1), `${key}=`, `${'A'.repeat(86)}==`]) assert.equal(validIndexKey(bad), false, bad)
  })

  it('keeps valid values and drops the rest, like Go', () => {
    const s = coerceExtensions({ indexUrl: ' https://ext.example.com/index.json ', indexPublicKey: key, 'acme.jira': { a: 1 } })
    assert.deepEqual(s, { 'acme.jira': { a: 1 }, indexUrl: 'https://ext.example.com/index.json', indexPublicKey: key })
    assert.deepEqual(indexSettings(s), { url: 'https://ext.example.com/index.json', publicKey: key })
    assert.deepEqual(coerceExtensions({ indexUrl: 'http://ext.example.com/', indexPublicKey: 5 }), {})
    assert.deepEqual(indexSettings({}), { url: '', publicKey: '' })
  })

  it('sets and clears them', () => {
    let s = withIndexSetting({}, 'indexUrl', 'https://ext.example.com/index.json')
    assert.deepEqual(s, { indexUrl: 'https://ext.example.com/index.json' })
    s = withIndexSetting(s, 'indexPublicKey', key)
    s = withIndexSetting(s, 'indexUrl', '')
    assert.deepEqual(s, { indexPublicKey: key }, 'empty is the default, so the key goes')
  })
})
