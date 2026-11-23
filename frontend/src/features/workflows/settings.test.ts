import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  coerceSchedule, coerceSchedules, coerceWorkflowSettings, coerceWritable, DEFAULT_WORKFLOW_SETTINGS, hasProblem, normalizeDays, parseFolders,
  firstProblem, scheduleProblems, writableProblem, type Schedule,
} from './settings'

const schedule = (over: Partial<Schedule> = {}): Schedule => ({
  name: 'nightly', workflow: 'plan-build-review', folder: '/w/api', task: 'Update the changelog', agent: '', at: '02:30', days: [], ...over,
})

describe('sandbox folders under ~/', () => {
  const home = ['/var/cache', '~/Library/Caches/go-build', '~/.cache/pnpm']
  it('keeps them through coerce, edit and save', () => {
    const coerced = coerceWorkflowSettings({ sandboxWritable: home })
    assert.deepEqual(coerced.sandboxWritable, home)
    // The settings page shows one per line, parses the field on blur and saves when valid.
    const lines = parseFolders(coerced.sandboxWritable.join('\n'))
    assert.equal(writableProblem(lines), '')
    assert.deepEqual(coerceWorkflowSettings({ ...coerced, sandboxWritable: lines }).sandboxWritable, home)
  })
  it('refuses a bare ~/, ~user and a relative folder', () => {
    assert.deepEqual(coerceWritable(['~/', '~', '~user/x', 'rel', '~/a']), ['~/a'])
    assert.match(writableProblem(['~/']), /absolute folder, or one under ~\//)
  })
  it('leaves a schedule folder absolute', () => {
    assert.equal(coerceSchedule(schedule({ folder: '~/work' })), null)
  })
})

describe('workflow settings', () => {
  it('has the defaults', () => {
    assert.equal(DEFAULT_WORKFLOW_SETTINGS.sandbox, 'on')
    assert.equal(DEFAULT_WORKFLOW_SETTINGS.sandboxNetwork, false)
    assert.deepEqual(DEFAULT_WORKFLOW_SETTINGS.sandboxWritable, [])
    assert.deepEqual(DEFAULT_WORKFLOW_SETTINGS.schedules, [])
  })
  it('falls back to the defaults for anything that is not an object', () => {
    for (const v of [null, 'x', 3, [], undefined]) assert.deepEqual(coerceWorkflowSettings(v), DEFAULT_WORKFLOW_SETTINGS)
  })
  it('switches the sandbox off only for a literal "off" and the network on only for true', () => {
    const on = coerceWorkflowSettings({ sandbox: 'on', sandboxNetwork: true })
    assert.equal(on.sandbox, 'on')
    assert.equal(on.sandboxNetwork, true)
    assert.equal(coerceWorkflowSettings({ sandbox: 'off' }).sandbox, 'off')
    for (const v of [true, false, 'OFF', 'docker', '', null, undefined, 0]) assert.equal(coerceWorkflowSettings({ sandbox: v }).sandbox, 'on')
    assert.equal(coerceWorkflowSettings({}).sandbox, 'on')
    assert.equal(coerceWorkflowSettings(null).sandbox, 'on')
    assert.equal(coerceWorkflowSettings({ sandboxNetwork: 'yes' }).sandboxNetwork, false)
  })
  it('keeps the older keys', () => {
    const s = coerceWorkflowSettings({ maxAttempts: 5, fallbackAgent: 'codex', resumeAfterMinutes: 30 })
    assert.deepEqual([s.maxAttempts, s.fallbackAgent, s.resumeAfterMinutes], [5, 'codex', 30])
  })
  it('survives a save and a load', () => {
    const s = coerceWorkflowSettings({ sandbox: 'on', sandboxWritable: ['/a'], schedules: [schedule({ days: ['mon', 'fri'] })] })
    assert.deepEqual(coerceWorkflowSettings(JSON.parse(JSON.stringify(s))), s)
  })
})

describe('writable folders', () => {
  it('keeps distinct absolute folders, at most 10', () => {
    assert.deepEqual(coerceWritable(['/a', 'b', '/a', 4, '/c\n']), ['/a'])
    const many = Array.from({ length: 14 }, (_, i) => `/f${i}`)
    assert.equal(coerceWritable(many).length, 10)
    assert.deepEqual(coerceWritable('/a'), [])
  })
  it('says why a list cannot be saved', () => {
    assert.equal(writableProblem(['/a', '/b']), '')
    assert.match(writableProblem(['/a', 'rel']), /not an absolute folder/)
    assert.match(writableProblem(['/a', '/a']), /twice/)
    assert.match(writableProblem(Array.from({ length: 11 }, (_, i) => `/f${i}`)), /At most 10/)
  })
  it('reads a text field one folder per line', () => {
    assert.deepEqual(parseFolders(' /a \n\n/b\n'), ['/a', '/b'])
  })
})

describe('schedules', () => {
  it('keeps a valid schedule and orders its days', () => {
    assert.deepEqual(coerceSchedule({ ...schedule(), days: ['sun', 'mon', 'mon', 'xyz'] })?.days, ['mon', 'sun'])
  })
  it('drops one that cannot be used', () => {
    assert.equal(coerceSchedule(null), null)
    assert.equal(coerceSchedule(schedule({ name: 'Bad Name' })), null)
    assert.equal(coerceSchedule(schedule({ at: '25:00' })), null)
    assert.equal(coerceSchedule(schedule({ folder: 'rel' })), null)
    assert.equal(coerceSchedule(schedule({ task: '  ' })), null)
    assert.equal(coerceSchedule(schedule({ workflow: '' })), null)
  })
  it('forgets an agent name that is not valid', () => {
    assert.equal(coerceSchedule(schedule({ agent: 'Not Valid' }))?.agent, '')
    assert.equal(coerceSchedule(schedule({ agent: 'codex' }))?.agent, 'codex')
  })
  it('keeps at most 20 and the first of a name', () => {
    const list = Array.from({ length: 25 }, (_, i) => schedule({ name: `s${i}` }))
    assert.equal(coerceSchedules(list).length, 20)
    assert.equal(coerceSchedules([schedule(), schedule({ task: 'other' })]).length, 1)
    assert.deepEqual(coerceSchedules('x'), [])
  })
  it('names the problem next to its field', () => {
    assert.equal(firstProblem(scheduleProblems(schedule(), [])), '')
    assert.equal(firstProblem(scheduleProblems(schedule({ name: '', task: '' }), [])), 'Give the schedule a name.')
    assert.equal(firstProblem(scheduleProblems(schedule({ workflow: '', task: '' }), [])), 'Choose a workflow.')
    assert.equal(firstProblem(scheduleProblems(schedule({ task: '', at: '' }), [])), 'Say what should be done.')
    assert.equal(hasProblem(scheduleProblems(schedule(), [])), false)
    assert.equal(scheduleProblems(schedule({ name: '' }), []).name, 'Give the schedule a name.')
    assert.match(scheduleProblems(schedule({ name: 'Bad' }), []).name, /lowercase/)
    assert.match(scheduleProblems(schedule(), ['nightly']).name, /Another schedule/)
    assert.equal(scheduleProblems(schedule({ at: '7:5' }), []).at, 'Use a time like 02:30.')
    assert.match(scheduleProblems(schedule({ task: '' }), []).task, /Say what/)
    assert.match(scheduleProblems(schedule({ task: 'é'.repeat(4001) }), []).task, /too long/)
    assert.equal(scheduleProblems(schedule({ task: 'é'.repeat(4000) }), []).task, '')
    assert.match(scheduleProblems(schedule({ workflow: '', folder: '' }), []).workflow, /Choose/)
  })
  it('normalizes days', () => {
    assert.deepEqual(normalizeDays(['fri', 'mon']), ['mon', 'fri'])
    assert.deepEqual(normalizeDays('mon'), [])
  })
})
