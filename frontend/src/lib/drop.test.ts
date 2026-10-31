import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { MAX_DROPPED_FILES, planDrop } from './drop'

describe('dropping from Finder', () => {
  it('opens the first folder when no project is open and attaches the rest', () => {
    const plan = planDrop([{ path: '/p/api', dir: true }, { path: '/p/web', dir: true }], [])
    assert.deepEqual(plan, { open: '/p/api', add: ['/p/web'], files: [] })
  })

  it('attaches folders to an open workspace, skipping the ones already in it', () => {
    const plan = planDrop([{ path: '/p/api', dir: true }, { path: '/p/web', dir: true }], ['/p/api'])
    assert.deepEqual(plan, { add: ['/p/web'], files: [] })
  })

  it('opens files, but not too many', () => {
    const many = Array.from({ length: 30 }, (_, i) => ({ path: `/f/${i}.go`, dir: false }))
    const plan = planDrop(many, ['/p'])
    assert.equal(plan.files.length, MAX_DROPPED_FILES)
    assert.equal(plan.open, undefined)
  })
})
