import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { SCRATCH_FILE_TYPES, scratchBadge, scratchFileType } from './scratchTypes'

describe('scratch file types', () => {
  it('have unique ids and file names with an extension', () => {
    assert.equal(new Set(SCRATCH_FILE_TYPES.map((t) => t.id)).size, SCRATCH_FILE_TYPES.length)
    for (const t of SCRATCH_FILE_TYPES) assert.match(t.fileName, /^[^./]+\.[a-z]+$/i, t.id)
  })
  it('start an HTTP request inside its template', () => {
    const http = scratchFileType('http')
    assert.equal(http?.fileName, 'requests.http')
    const lines = http?.template?.split('\n') ?? []
    assert.ok((http?.line ?? 0) <= lines.length)
  })
})

describe('scratchBadge', () => {
  it('shows the extension', () => {
    assert.equal(scratchBadge('requests-2.http'), 'HTTP')
    assert.equal(scratchBadge('scratch.markdown'), 'MARK')
    assert.equal(scratchBadge('Makefile'), '')
    assert.equal(scratchBadge('.env'), '')
  })
})
