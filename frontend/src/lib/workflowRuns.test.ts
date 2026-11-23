import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { runStateText, runStateTone, runTabData } from './workflowRuns'

describe('run state words', () => {
  it('words every state', () => {
    assert.deepEqual(
      ['running', 'waiting', 'failed', 'paused', 'done', 'stopped'].map((s) => runStateText(s)),
      ['Running', 'Waiting for you', 'Failed', 'Paused', 'Done', 'Stopped'],
    )
  })
  it('says Queued only for a running run that waits', () => {
    assert.equal(runStateText('running', true), 'Queued')
    assert.equal(runStateText('failed', true), 'Failed')
  })
  it('keeps an unknown state as it is and names an empty one', () => {
    assert.equal(runStateText('odd'), 'odd')
    assert.equal(runStateText(''), 'Unknown')
  })
  it('colours the states', () => {
    assert.equal(runStateTone('running'), 'blue')
    assert.equal(runStateTone('running', true), 'yellow')
    assert.equal(runStateTone('waiting'), 'indigo')
    assert.equal(runStateTone('failed'), 'red')
    assert.equal(runStateTone('done'), 'green')
    assert.equal(runStateTone('stopped'), 'grey')
    assert.equal(runStateTone(''), 'grey')
  })
  it('makes the tab data the Workflows feature reads', () => {
    assert.deepEqual(runTabData('r1'), { runId: 'r1' })
  })
})
