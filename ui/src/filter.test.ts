import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { addedBlocks, filterByLabel, moveIndex } from './filter'

const items = [{ label: 'Git: Commit' }, { label: 'Git: Push' }, { label: 'View: Toggle Terminal' }]

describe('filterByLabel', () => {
  it('returns every item for an empty query', () => {
    assert.equal(filterByLabel(items, '  ').length, 3)
  })
  it('matches every word, case-insensitively, in any order', () => {
    assert.deepEqual(filterByLabel(items, 'push GIT'), [{ label: 'Git: Push' }])
  })
  it('returns nothing when a word is missing', () => {
    assert.deepEqual(filterByLabel(items, 'git terminal'), [])
  })
})

describe('moveIndex', () => {
  it('clamps at both ends', () => {
    assert.equal(moveIndex(0, -1, 3), 0)
    assert.equal(moveIndex(2, 1, 3), 2)
    assert.equal(moveIndex(1, 1, 3), 2)
  })
  it('is 0 for an empty list', () => {
    assert.equal(moveIndex(4, 1, 0), 0)
  })
})

describe('addedBlocks', () => {
  it('splits blocks by the share of additions', () => {
    assert.equal(addedBlocks(142, 0), 5)
    assert.equal(addedBlocks(0, 9), 0)
    assert.equal(addedBlocks(24, 18), 3)
  })
  it('is 0 when nothing changed', () => {
    assert.equal(addedBlocks(0, 0), 0)
  })
})
