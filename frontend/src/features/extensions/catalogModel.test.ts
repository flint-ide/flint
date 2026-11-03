import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { catalogRows, compareVersions, sentence } from './catalogModel'

const entry = (id: string, version = '1.0.0', error = '') => ({ id, version, error })

describe('extensions repository', () => {
  it('marks each entry available, installed, update, newer or broken', () => {
    const rows = catalogRows(
      [entry('a'), entry('b', '2.0.0'), entry('c'), entry('d', '', 'bad manifest'), entry('e', '1.0.0')],
      [
        { id: 'b', version: '1.0.0' },
        { id: 'c', version: '1.0.0' },
        { id: 'd', version: '1.0.0' },
        { id: 'e', version: '1.2.0' },
      ],
    )
    assert.deepEqual(
      rows.map((r) => [r.entry.id, r.status, r.installedVersion]),
      [
        ['a', 'available', ''],
        ['b', 'update', '1.0.0'],
        ['c', 'installed', '1.0.0'],
        ['d', 'broken', '1.0.0'],
        ['e', 'newer', '1.2.0'],
      ],
    )
  })

  it('keeps the entries in the given order', () => {
    assert.deepEqual(
      catalogRows([entry('z'), entry('a')], []).map((r) => r.entry.id),
      ['z', 'a'],
    )
  })

  it('shows Go messages as sentences', () => {
    assert.equal(sentence('couldn’t download it. Click Refresh Repository'), 'Couldn’t download it. Click Refresh Repository.')
    assert.equal(sentence('done.'), 'Done.')
    assert.equal(sentence('details:\nline'), 'Details:\nline')
    assert.equal(sentence('  '), '')
  })

  it('compares versions numerically', () => {
    assert.equal(compareVersions('1.10.0', '1.9.0'), 1)
    assert.equal(compareVersions('0.1', '0.1.0'), 0)
    assert.equal(compareVersions('v2.0.0', '2.0.1'), -1)
    assert.equal(compareVersions('1.0.0-beta', '1.0.0'), 0)
  })
})
