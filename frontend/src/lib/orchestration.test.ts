import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceOrchestration, DEFAULT_ORCHESTRATION } from './orchestration'

describe('coerceOrchestration', () => {
  it('is off by default and fills in what is missing', () => {
    assert.equal(DEFAULT_ORCHESTRATION.enabled, false)
    assert.deepEqual(coerceOrchestration(undefined), DEFAULT_ORCHESTRATION)
    assert.deepEqual(coerceOrchestration({ enabled: true }), { ...DEFAULT_ORCHESTRATION, enabled: true })
  })

  it('clamps like Go', () => {
    assert.deepEqual(coerceOrchestration({ maxChildren: 99, maxDepth: -1, maxWaitSeconds: 99999, outputLines: 0, notifyParent: false }), {
      enabled: false,
      maxChildren: 16,
      maxDepth: 1,
      maxWaitSeconds: 600,
      notifyParent: false,
      outputLines: 200,
    })
    assert.equal(coerceOrchestration({ maxChildren: 'x' }).maxChildren, 4)
    assert.equal(coerceOrchestration({ maxDepth: 2.6 }).maxDepth, 3)
  })
})
