import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { runSummary } from './alerts'

const none = { passed: 0, failed: 0, skipped: 0, running: 0, total: 0 }

describe('background notifications', () => {
  it('reports how a run ended', () => {
    assert.equal(runSummary({ kind: 'config', exit: { code: 0, stopped: false, elapsedMs: 4200 }, tests: none }), 'Finished in 4.2 s')
    assert.equal(runSummary({ kind: 'config', exit: { code: 2, stopped: false, elapsedMs: 65_000 }, tests: none }), 'Exited with code 2 after 65 s')
    assert.equal(runSummary({ kind: 'config', exit: { code: -1, stopped: false, elapsedMs: 0, error: 'no such file' } , tests: none }), 'Failed: no such file')
  })

  it('counts tests', () => {
    const tests = { ...none, passed: 9, failed: 1, total: 10 }
    assert.equal(runSummary({ kind: 'tests', exit: { code: 1, stopped: false, elapsedMs: 3000 }, tests }), '1 of 10 tests failed')
    assert.equal(runSummary({ kind: 'tests', exit: { code: 0, stopped: false, elapsedMs: 3000 }, tests: { ...tests, passed: 10, failed: 0 } }), '10 tests passed in 3.0 s')
  })

  it('stays quiet when the user stopped it', () => {
    assert.equal(runSummary({ kind: 'config', exit: { code: -1, stopped: true, elapsedMs: 100 }, tests: none }), null)
  })
})
