import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { SearchMatch } from '../api/backend'
import { fileKey, matchKey, replaceTargets } from './replaceTargets'

const match = (path: string, line: number, n = 1): SearchMatch => ({
  root: '/p', path, line, text: 'x', ranges: [], replacements: Array.from({ length: n }, () => 'y'),
})

describe('replaceTargets', () => {
  it('groups ticked lines by file and counts replacements', () => {
    const ms = [match('a.go', 1, 2), match('a.go', 4), match('b.go', 2)]
    const { targets, count } = replaceTargets(ms, new Set(), new Set())
    assert.deepEqual(targets, [{ root: '/p', path: 'a.go', lines: [1, 4] }, { root: '/p', path: 'b.go', lines: [2] }])
    assert.equal(count, 4)
  })

  it('leaves out unticked files and lines', () => {
    const ms = [match('a.go', 1), match('a.go', 4), match('b.go', 2)]
    const excluded = new Set([fileKey(ms[2]), matchKey(ms[0])])
    assert.deepEqual(replaceTargets(ms, excluded, new Set()).targets, [{ root: '/p', path: 'a.go', lines: [4] }])
  })

  it('skips files with unsaved edits and names them', () => {
    const ms = [match('a.go', 1), match('b.go', 2)]
    const r = replaceTargets(ms, new Set(), new Set(['/p/a.go']))
    assert.deepEqual(r.targets.map((t) => t.path), ['b.go'])
    assert.deepEqual(r.skippedDirty, ['/p/a.go'])
  })
})
