import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { kindTone, linkState, logKind, runProgress, stepLook, waitingNote, type StepStatus } from './stepLook'
import type { RunInfo, StepInfo } from './workflows'

const step = (over: Partial<StepInfo> = {}): StepInfo => ({
  id: 's', title: 'Step', kind: 'agent', agent: '', run: '', onFail: '', text: '', state: 'pending', attempts: 0, chat: '', runTab: '', output: '',
  skipped: false, takenBy: '', ...over,
})

type RunState = Pick<RunInfo, 'state' | 'current' | 'queued' | 'hold'>
const run = (over: Partial<RunState> = {}): RunState => ({ state: 'running', current: 1, queued: false, hold: null, ...over })

const statuses = (r: RunState | null, steps: StepInfo[]): StepStatus[] => steps.map((s, i) => stepLook(r, i, s).status)

describe('stepLook', () => {
  it('has no state in a preview', () => {
    assert.deepEqual(stepLook(null, 0, step({ state: 'done' })), { status: 'none', label: '', tone: 'grey' })
  })

  it('follows the steps of a running run', () => {
    const steps = [step({ state: 'done' }), step({ state: 'running' }), step()]
    assert.deepEqual(statuses(run(), steps), ['done', 'running', 'pending'])
    assert.deepEqual(stepLook(run(), 1, steps[1]), { status: 'running', label: 'Running', tone: 'blue' })
    assert.deepEqual(stepLook(run(), 2, steps[2]), { status: 'pending', label: 'Not started', tone: 'grey' })
  })

  it('shows "waiting for you" on the step a waiting run is at, not "Not started"', () => {
    const steps = [step({ state: 'done' }), step({ state: 'done' }), step({ kind: 'gate' })]
    assert.deepEqual(stepLook(run({ state: 'waiting', current: 2 }), 2, steps[2]), { status: 'waiting', label: 'Waiting for you', tone: 'indigo' })
  })

  it('shows a pause (by the user or a limit) and a queue on the step the run is at', () => {
    const steps = [step({ state: 'done' }), step()]
    assert.equal(stepLook(run({ state: 'paused' }), 1, steps[1]).status, 'paused')
    assert.equal(stepLook(run({ hold: { kind: 'limit', reason: 'x' } }), 1, steps[1]).status, 'paused')
    assert.deepEqual(stepLook(run({ queued: true }), 1, steps[1]), { status: 'queued', label: 'Queued', tone: 'yellow' })
    // A pause does not hide a step that already failed.
    assert.equal(stepLook(run({ state: 'paused' }), 1, step({ state: 'failed' })).status, 'failed')
  })

  it('tells a failed step from the one a stop interrupted, and a step continued anyway', () => {
    assert.deepEqual(stepLook(run({ state: 'failed' }), 1, step({ state: 'failed' })), { status: 'failed', label: 'Failed', tone: 'red' })
    assert.deepEqual(stepLook(run({ state: 'stopped' }), 1, step({ state: 'failed' })), { status: 'stopped', label: 'Stopped', tone: 'grey' })
    assert.deepEqual(stepLook(run({ state: 'done', current: 3 }), 0, step({ state: 'done', skipped: true })), { status: 'skipped', label: 'Continued', tone: 'yellow' })
  })
})

describe('linkState', () => {
  it('draws travelled connectors after passed steps, the rest ahead, and plain ones in a preview', () => {
    const looks = (['done', 'skipped', 'running', 'pending'] as StepStatus[]).map((status) => ({ status }))
    assert.deepEqual([0, 1, 2, 3].map((i) => linkState(looks, i)), ['done', 'done', 'todo', 'todo'])
    assert.equal(linkState([{ status: 'none' }, { status: 'none' }], 0), 'plain')
    assert.equal(linkState([], 4), 'plain')
  })
})

describe('kindTone', () => {
  it('gives every kind its own hue and none a state colour', () => {
    const tones = ['agent', 'review', 'script', 'gate'].map(kindTone)
    assert.deepEqual(tones, ['purple', 'pink', 'neutral', 'indigo'])
    assert.equal(new Set(tones).size, 4)
    assert.equal(kindTone(''), 'purple')
  })
})

describe('runProgress', () => {
  it('counts the steps done or continued', () => {
    const steps = [step({ state: 'done' }), step({ state: 'done', skipped: true }), step({ state: 'running' }), step(), step()]
    assert.deepEqual(runProgress({ steps }), { done: 2, total: 5, percent: 40, text: '2 of 5 steps done' })
    assert.deepEqual(runProgress({ steps: [] }), { done: 0, total: 0, percent: 0, text: '0 of 0 steps done' })
    assert.equal(runProgress({ steps: [step({ state: 'done' })] }).text, '1 of 1 step done')
  })
})

describe('logKind', () => {
  it('reads the kind of a log line from Go\'s wording', () => {
    const lines: [string, string][] = [
      ['Run started', 'start'],
      ['Started Plan (attempt 1)', 'start'],
      ['Passed: Plan', 'pass'],
      ['Approved: Approve', 'pass'],
      ['Failed: Test: exit code 1', 'fail'],
      ['Waiting for your approval: Approve', 'wait'],
      ['Paused before Test', 'pause'],
      ['Pause requested: the run pauses before the next step', 'pause'],
      ['Resumed', 'resume'],
      ['Sent back to Plan automatically', 'back'],
      ['Continued anyway past Test', 'skip'],
      ['Checked again: Plan ends with RESULT: DONE', 'check'],
      ['claude reached its usage limit: codex takes the step', 'limit'],
      ['Run done', 'done'],
      ['Stopped', 'stop'],
      ['The run configuration changed', 'info'],
    ]
    for (const [text, kind] of lines) assert.equal(logKind(text), kind, text)
  })
})

describe('waitingNote', () => {
  const steps = [step({ state: 'done' }), step({ kind: 'gate' })]
  it('says what the primary action does, and the way back when it is offered', () => {
    assert.equal(waitingNote({ actions: ['approve', 'sendBack', 'stop'], steps, current: 1 }), 'Your turn: approve and finish the run. Or send the work back to an earlier step with a note.')
    assert.equal(waitingNote({ actions: ['approve'], steps: [...steps, step()], current: 1 }), 'Your turn: pass this step: the run goes on with the next one.')
  })
  it('is empty without a primary action', () => {
    assert.equal(waitingNote({ actions: ['stop'], steps, current: 1 }), '')
  })
})
