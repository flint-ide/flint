import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { ScheduleView } from '../../../bindings/flint/internal/features/workflows/models'
import {
  actionHint, AGENT_ANSWER_NOTE, canRecheck, attemptsText, chatButton, defaultSendBack, holdDetail, originText, sendBackLabel, formatWhen, groupRuns, holdText, initialStep, nameProblem, needsYouCount, needsYouText, noteProblem,
  onFailHint, orderedActions, pickWorkflow, primaryAction, runIdOf, restrictedNote, runStateLabel, runStatus, runTone, sendBackChoices, sourceLabel,
  MAX_TASK_BYTES, stepBadge, stoppedStep, outputLabel, capitalize, stepCount, stepKind, stepWho, taskProblem, usable, workflowMeta,
  daysText, goalProblem, isRestrictedError, lastRunText, nextText, SCHEDULE_CHANGED_NOTE, scheduleLines, scheduleWhen, scriptNotice, sandboxText, skippedText, consentText, UNSANDBOXED_ASKS, UNSANDBOXED_ALLOWED,
  runTooltip, workflowViewTitle, workflowViewData, workflowViewKey, workflowHeader, workflowViewState, WORKFLOW_GONE, clampStep, noTextNote, isWorkflowFile, workflowForPath, workflowFileRoot,
  workflowHistory,
  type RunInfo, type StepInfo, type WorkflowInfo,
} from './workflows'

const step = (over: Partial<StepInfo> = {}): StepInfo => ({
  id: 's', title: 'Step', kind: 'agent', agent: '', run: '', onFail: '', text: '', state: 'pending', attempts: 0, chat: '', runTab: '', output: '',
  skipped: false, takenBy: '', ...over,
})

const FLOW = [
  step({ id: 'plan', title: 'Plan', state: 'done' }),
  step({ id: 'implement', title: 'Implement', state: 'done' }),
  step({ id: 'test', title: 'Test', kind: 'script', run: 'test', onFail: 'Implement', state: 'running' }),
  step({ id: 'review', title: 'Review', kind: 'review', onFail: 'implement' }),
  step({ id: 'approve', title: 'Approve', kind: 'gate' }),
]

const run = (over: Partial<RunInfo> = {}): RunInfo => ({
  id: 'r1', workflow: 'ship', title: 'Add login', description: '', task: 't1', dir: '/w', agent: 'claude', origin: '', state: 'running', error: '', current: 1,
  paused: false, feedback: '', taskState: 'working', queued: false, needsUser: false, hold: null, actions: [], steps: FLOW, log: [], created: 0,
  updated: 0, ...over,
})

const at = (h: number, m: number, dayOffset = 0) => new Date(2026, 0, 10 + dayOffset, h, m).getTime() / 1000
const NOW = new Date(2026, 0, 10, 12, 0).getTime()

describe('state words', () => {
  it('uses plain labels', () => {
    assert.deepEqual(
      ['running', 'waiting', 'failed', 'paused', 'done', 'stopped'].map((state) => runStateLabel({ state, queued: false })),
      ['Running', 'Waiting for you', 'Failed', 'Paused', 'Done', 'Stopped'],
    )
    assert.equal(runStateLabel({ state: 'running', queued: true }), 'Queued')
    assert.equal(runStateLabel({ state: 'queued?', queued: false }), 'queued?')
  })
  it('gives each state a tone', () => {
    assert.equal(runTone({ state: 'waiting', queued: false }), 'indigo')
    assert.equal(runTone({ state: 'running', queued: true }), 'yellow')
    assert.equal(runTone({ state: 'stopped', queued: false }), 'grey')
  })
})

describe('formatWhen', () => {
  it('shows the clock for today, then tomorrow and a date', () => {
    assert.equal(formatWhen(at(15, 40), NOW), '15:40')
    assert.equal(formatWhen(at(2, 5, 1), NOW), 'tomorrow 02:05')
    assert.equal(formatWhen(at(9, 0, 5), NOW), '15 Jan 09:00')
  })
})

describe('runStatus', () => {
  it('numbers the running step', () => assert.equal(runStatus(run(), NOW), 'Step 2 of 5 · Implement'))
  it('asks for approval at a gate', () => {
    assert.equal(runStatus(run({ state: 'waiting', current: 4 }), NOW), 'Waiting for your approval · Approve')
    assert.equal(runStatus(run({ state: 'waiting', current: 1 }), NOW), 'Waiting for you · Implement')
  })
  it('says what failed, paused, stopped and finished', () => {
    assert.equal(runStatus(run({ state: 'failed', current: 2 }), NOW), 'Failed · Test')
    assert.equal(runStatus(run({ state: 'paused', current: 2 }), NOW), 'Paused · next: Test')
    assert.equal(runStatus(run({ state: 'stopped', current: 2 }), NOW), 'Stopped at step 3 of 5 · Test')
    assert.equal(runStatus(run({ state: 'done', current: 5 }), NOW), 'All 5 steps done')
    assert.equal(runStatus(run({ queued: true }), NOW), 'Waits for a free agent slot')
  })
  it('says a pause is asked for while a step runs', () => {
    assert.equal(runStatus(run({ paused: true }), NOW), 'Step 2 of 5 · Implement · pauses after this step')
    assert.equal(runStatus(run({ paused: true, state: 'paused', current: 2 }), NOW), 'Paused · next: Test')
    assert.equal(runStatus(run({ paused: false }), NOW), 'Step 2 of 5 · Implement')
  })
  it('says who started a run', () => {
    assert.equal(originText(''), '')
    assert.equal(originText('agent'), 'Started by an agent')
    assert.equal(originText('item'), 'Started from a project item')
    assert.equal(originText('schedule:nightly'), 'Started by the schedule nightly')
    assert.equal(originText('schedule:'), '')
    assert.equal(originText('something-new'), '')
  })
  it('explains a hold with the agent and the time', () => {
    const limit = run({ state: 'paused', hold: { kind: 'limit', reason: 'x', until: at(15, 40) }, steps: [step({ title: 'A', takenBy: 'codex' })], current: 0 })
    assert.equal(runStatus(limit, NOW), 'Paused by a limit: codex reached its usage limit · resumes at 15:40')
    const waits = run({ state: 'paused', hold: { kind: 'limit', reason: 'x', until: 0 }, current: 1 })
    assert.equal(holdText(waits, NOW), 'Paused by a limit: claude reached its usage limit')
    const budget = run({ state: 'paused', hold: { kind: 'budget', reason: 'the weekly budget of 20 USD is spent' } })
    assert.equal(runStatus(budget, NOW), 'Paused by a limit: the weekly budget of 20 USD is spent')
    assert.equal(holdText(run({ hold: { kind: 'budget', reason: '' } }), NOW), 'Paused by a limit: the usage budget is spent')
    assert.ok(!/today/.test(holdText(budget, NOW)))
  })
  it('shows the rest of Go\'s reason only when the headline does not hold it', () => {
    assert.equal(holdDetail(null), '')
    assert.equal(holdDetail({ kind: 'budget', reason: 'short' }), '')
    assert.equal(holdDetail({ kind: 'limit', reason: 'rate limit hit' }), 'Rate limit hit')
    const long = 'x'.repeat(300)
    assert.equal(holdDetail({ kind: 'budget', reason: long }), capitalize(long))
    assert.ok([...holdText(run({ hold: { kind: 'budget', reason: long } }), NOW)].length < 200)
  })
})

describe('groupRuns', () => {
  const runs = [
    run({ id: 'a', state: 'done', updated: 9 }),
    run({ id: 'b', state: 'running', updated: 1 }),
    run({ id: 'c', state: 'waiting', needsUser: true, updated: 2 }),
    run({ id: 'd', state: 'running', updated: 5 }),
    run({ id: 'e', state: 'paused', needsUser: true, updated: 8 }),
    run({ id: 'f', state: 'stopped', updated: 3 }),
  ]
  it('puts the runs that need the user first, newest first', () => {
    const groups = groupRuns(runs)
    assert.deepEqual(groups.map((g) => [g.title, g.runs.map((r) => r.id)]), [
      ['Needs you', ['e', 'c']],
      ['Running', ['d', 'b']],
      ['Finished', ['a', 'f']],
    ])
  })
  it('drops empty groups and leaves the input alone', () => {
    assert.deepEqual(groupRuns([run({ state: 'done' })]).map((g) => g.id), ['finished'])
    assert.equal(runs[0].id, 'a')
    assert.deepEqual(groupRuns([]), [])
  })
  it('counts the runs that need the user', () => {
    assert.equal(needsYouCount(runs), 2)
    assert.equal(needsYouText(1), 'Workflows: 1 needs you')
    assert.equal(needsYouText(2), 'Workflows: 2 need you')
  })
})

describe('steps', () => {
  it('names kinds', () => {
    assert.deepEqual(stepKind('gate'), { label: 'Approval', icon: 'gate' })
    assert.equal(stepKind('script').label, 'Script')
    assert.equal(stepKind('odd').label, 'odd')
  })
  it('says who takes a step', () => {
    assert.equal(stepWho(step({ takenBy: 'codex', agent: 'planner' }), 'claude'), 'codex')
    assert.equal(stepWho(step({ agent: 'planner' }), 'claude'), 'planner')
    assert.equal(stepWho(step(), 'claude'), 'claude')
    assert.equal(stepWho(step(), ''), 'The agent you choose')
    assert.equal(stepWho(step({ kind: 'script', run: 'test' }), 'claude'), 'Runs “test”')
    assert.equal(stepWho(step({ kind: 'gate' }), 'claude'), 'You')
  })
  it('turns a state into a badge', () => {
    assert.deepEqual(stepBadge(step({ state: 'running' })), { label: 'Running', tone: 'blue' })
    assert.deepEqual(stepBadge(step({ state: 'done', skipped: true })), { label: 'Continued', tone: 'yellow' })
    assert.equal(stepBadge(step({ state: '' })), null)
  })
  it('shows the step a stop interrupted as Stopped, not Failed', () => {
    const failed = step({ state: 'failed' })
    assert.equal(stoppedStep({ state: 'stopped', current: 1 }, 1, failed), true)
    assert.equal(stoppedStep({ state: 'stopped', current: 1 }, 0, failed), false)
    assert.equal(stoppedStep({ state: 'failed', current: 1 }, 1, failed), false)
    assert.equal(stoppedStep({ state: 'stopped', current: 1 }, 1, step({ state: 'done' })), false)
    assert.deepEqual(stepBadge(failed, true), { label: 'Stopped', tone: 'grey' })
    assert.deepEqual(stepBadge(failed), { label: 'Failed', tone: 'red' })
  })
  it('labels the output of a step that waits to be taken again as old', () => {
    assert.equal(outputLabel(step({ state: 'done', attempts: 1, output: 'x' })), 'The end of what it said')
    assert.equal(outputLabel(step({ state: 'failed', attempts: 2, output: 'x' })), 'The end of what it said')
    assert.equal(outputLabel(step({ state: 'pending', attempts: 2, output: 'x' })), 'What it said last time (attempt 2)')
    assert.equal(outputLabel(step({ state: 'pending', attempts: 0, output: 'x' })), '')
    assert.equal(outputLabel(step({ state: 'done', attempts: 1, output: '' })), '')
  })
  it('capitalises the first letter of a sentence', () => {
    assert.equal(capitalize('the run moved on'), 'The run moved on')
    assert.equal(capitalize('Already fine'), 'Already fine')
    assert.equal(capitalize(''), '')
    assert.equal(holdDetail({ kind: 'limit', reason: 'rate limit hit' }), 'rate limit hit'.replace('r', 'R'))
  })
  it('words on-fail and attempts', () => {
    assert.equal(onFailHint(FLOW[2], FLOW), 'fails → back to Implement')
    assert.equal(onFailHint(FLOW[3], FLOW), 'fails → back to Implement')
    assert.equal(onFailHint(step({ onFail: 'gone' }), FLOW), 'fails → back to gone')
    assert.equal(onFailHint(FLOW[0], FLOW), '')
    assert.equal(attemptsText(step({ attempts: 1 })), '')
    assert.equal(attemptsText(step({ attempts: 3 })), 'Attempt 3')
    assert.equal(stepCount(1), '1 step')
    assert.equal(stepCount(5), '5 steps')
  })
  it('starts on the current step', () => {
    assert.equal(initialStep(run({ current: 2 })), 2)
    assert.equal(initialStep(run({ current: 5 })), 4)
    assert.equal(initialStep(run({ steps: [], current: 0 })), 0)
  })
})

describe('actions', () => {
  it('orders buttons with the primary first and the ending ones last', () => {
    const list = orderedActions(['remove', 'stop', 'skip', 'sendBack', 'retry', 'unknown'])
    assert.deepEqual(list.map((a) => a.name), ['retry', 'sendBack', 'skip', 'stop', 'remove'])
  })
  it('words them', () => {
    const [approve] = orderedActions(['approve'])
    assert.deepEqual([approve.label, approve.tone], ['Approve', 'primary'])
    assert.equal(orderedActions(['sendBack'])[0].label, 'Send back…')
    assert.equal(orderedActions(['skip'])[0].label, 'Continue anyway')
  })
  it('asks before anything that ends or overrides', () => {
    for (const a of orderedActions(['approve', 'resume', 'recheck', 'retry', 'sendBack', 'skip', 'pause', 'stop', 'remove'])) {
      assert.equal(a.confirm !== '', ['skip', 'stop', 'remove'].includes(a.name), a.name)
      assert.ok(a.hint.length > 0)
    }
    assert.equal(orderedActions(['stop'])[0].tone, 'danger')
  })
  it('picks the primary action for a row', () => {
    assert.equal(primaryAction(['pause', 'stop'])?.name, undefined)
    assert.equal(primaryAction(['stop', 'resume'])?.name, 'resume')
    assert.equal(primaryAction([]), null)
  })
})

describe('send back', () => {
  it('offers every step up to and including the current one', () => {
    assert.deepEqual(sendBackChoices(run({ current: 2 })).map((c) => c.id), ['plan', 'implement', 'test'])
    assert.deepEqual(sendBackChoices(run({ current: 1 })).map((c) => c.id), ['plan', 'implement'])
    assert.deepEqual(sendBackChoices(run({ current: 0 })).map((c) => c.id), ['plan'])
    assert.deepEqual(sendBackChoices(run({ current: 5 })).map((c) => c.id), ['plan', 'implement', 'test', 'review', 'approve'])
  })
  it('shows each choice with its kind', () => {
    assert.deepEqual(sendBackChoices(run({ current: 2 })).map(sendBackLabel), ['1. Plan (Agent)', '2. Implement (Agent)', '3. Test (Script)'])
    assert.equal(sendBackLabel(sendBackChoices(run({ current: 4 }))[4]), '5. Approve (Approval)')
  })
  it('defaults to the on-fail target, else the step before', () => {
    assert.equal(defaultSendBack(run({ current: 2 })), 'implement')
    assert.equal(defaultSendBack(run({ current: 4 })), 'implement')
    assert.equal(defaultSendBack(run({ current: 1 })), 'plan')
    assert.equal(defaultSendBack(run({ current: 0 })), 'plan')
    assert.equal(defaultSendBack(run({ steps: [step({ id: 'g', kind: 'gate' })], current: 0 })), 'g')
    assert.equal(defaultSendBack(run({ steps: [], current: 0 })), '')
  })
  it('bounds the note', () => {
    assert.equal(noteProblem('short'), '')
    assert.match(noteProblem('x'.repeat(4001)), /At most 4000/)
  })
})

describe('workflows', () => {
  const flow = (over: Partial<WorkflowInfo>): WorkflowInfo => ({ name: 'a', description: 'd', source: 'builtin', path: '', steps: FLOW, problem: '', ...over })
  it('knows which can run', () => {
    assert.ok(usable(flow({})))
    assert.ok(!usable(flow({ problem: 'bad' })))
    assert.ok(!usable(flow({ steps: [] })))
    const list = [flow({ name: 'x', problem: 'bad' }), flow({ name: 'y' }), flow({ name: 'z' })]
    assert.equal(pickWorkflow(list, 'z'), 'z')
    assert.equal(pickWorkflow(list, 'x'), 'y')
    assert.equal(pickWorkflow([], ''), '')
  })
  it('names sources', () => {
    assert.deepEqual(['builtin', 'personal', 'project'].map(sourceLabel), ['built-in', 'personal', 'this repository'])
  })
  it('summarises one', () => {
    assert.deepEqual(workflowMeta(flow({})), ['d', 'built-in', '5 steps'])
    assert.deepEqual(workflowMeta(flow({ description: '', problem: 'x', source: 'project' })), ['this repository'])
  })
  it('checks the task and the name', () => {
    assert.equal(taskProblem('  '), 'Say what should be done.')
    assert.equal(taskProblem('Add login'), '')
    assert.match(taskProblem('x'.repeat(50000)), /too long/)
    assert.equal(MAX_TASK_BYTES, 8000)
    assert.equal(taskProblem('x'.repeat(8000)), '')
    assert.equal(taskProblem('é'.repeat(4000)), '')
    assert.equal(taskProblem('é'.repeat(4001)), 'The text is too long (at most 8000 bytes).')
    assert.equal(nameProblem('ship-it2'), '')
    assert.ok(nameProblem(''))
    assert.ok(nameProblem('Ship'))
    assert.ok(nameProblem('-x'))
    assert.ok(nameProblem('a b'))
    assert.ok(nameProblem('a'.repeat(65)))
  })
  it('words the restricted note', () => {
    assert.match(restrictedNote(1), /its workflow is hidden/)
    assert.match(restrictedNote(3), /3 workflows are hidden/)
    assert.match(restrictedNote(0), /hidden/)
  })
})

describe('runIdOf', () => {
  it('reads the id from the tab data and rejects anything else', () => {
    assert.equal(runIdOf({ runId: 'r1' }), 'r1')
    assert.equal(runIdOf({ runId: 4 }), '')
    assert.equal(runIdOf(null), '')
    assert.equal(runIdOf('r1'), '')
  })
})

describe('Coordinate with Agent', () => {
  it('checks the goal', () => {
    assert.equal(goalProblem('  '), 'Say what the goal is.')
    assert.equal(goalProblem('ship it'), '')
    assert.equal(goalProblem('é'.repeat(2000)), '')
    assert.match(goalProblem('é'.repeat(2001)), /At most 2000/)
  })
  it('knows a refusal for a folder in Restricted Mode', () => {
    assert.equal(isRestrictedError('the folder is in Restricted Mode'), true)
    assert.equal(isRestrictedError('boom'), false)
  })
})

describe('schedule lines', () => {
  const now = new Date(2026, 1, 3, 12, 0).getTime()
  const at = (d: number, h: number, m: number) => new Date(2026, 1, d, h, m).getTime() / 1000
  const info = (over: Partial<ScheduleView> = {}): ScheduleView => ({
    name: 'nightly', workflow: 'w', folder: '/f', task: 't', agent: '', at: '02:30', days: [], next: 0, lastRun: '', lastAt: 0, lastSkipped: '', changed: false, ...over,
  })
  it('words the days', () => {
    assert.equal(daysText([]), 'every day')
    assert.equal(daysText(['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']), 'every day')
    assert.equal(daysText(['fri', 'mon', 'tue', 'wed', 'thu']), 'weekdays')
    assert.equal(daysText(['sun', 'sat']), 'weekends')
    assert.equal(daysText(['wed', 'mon']), 'Mon, Wed')
    assert.equal(scheduleWhen(info({ days: ['mon'] })), '02:30 Mon')
  })
  it('says when it runs next', () => {
    assert.equal(nextText({ next: at(3, 14, 30) }, now), 'Next: today 14:30')
    assert.equal(nextText({ next: at(4, 2, 30) }, now), 'Next: tomorrow 02:30')
    assert.equal(nextText({ next: at(9, 2, 30) }, now), 'Next: 9 Feb 02:30')
    assert.match(nextText({ next: 0 }, now), /Cannot run now/)
  })
  it('says why the last time did not start', () => {
    assert.equal(skippedText({ lastSkipped: 'the folder is not open in Flint.' }), 'Did not start last time: the folder is not open in Flint')
    assert.equal(skippedText({ lastSkipped: '' }), '')
  })
  it('says when the last run started', () => {
    assert.equal(lastRunText({ lastRun: 'r1', lastAt: at(3, 2, 30) }, now), 'Last run: today 02:30')
    assert.equal(lastRunText({ lastRun: 'r1', lastAt: 0 }, now), 'Last run: r1')
    assert.equal(lastRunText({ lastRun: '', lastAt: 0 }, now), '')
  })
  it('lists the lines that apply', () => {
    assert.deepEqual(scheduleLines(info({ next: at(4, 2, 30), lastSkipped: 'busy', lastRun: 'r', lastAt: at(3, 1, 0) }), now), [
      'Next: tomorrow 02:30', 'Did not start last time: busy', 'Last run: today 01:00',
    ])
  })
})

describe('Open chat of a step', () => {
  const withChat = [step({ id: 'a', chat: 'c1' }), step({ id: 'b' })]
  it('opens the step\'s own chat when it has one', () => {
    assert.deepEqual(chatButton({ steps: withChat, task: 't1', queued: false }, 0), { target: 'step', enabled: true, hint: 'Show the chat of this step' })
  })
  it('opens the run\'s agent task for the first step while its chat is not recorded yet', () => {
    const b = chatButton({ steps: [step(), step()], task: 't1', queued: false }, 0)
    assert.equal(b.target, 'task')
    assert.equal(b.enabled, true)
  })
  it('is disabled while the run waits for a slot', () => {
    assert.deepEqual(chatButton({ steps: [step()], task: 't1', queued: true }, 0), {
      target: 'none', enabled: false, hint: 'Waiting for a free slot: the agent has not started yet',
    })
  })
  it('has nothing to open for a later step without a chat, or a run without a task', () => {
    assert.deepEqual(chatButton({ steps: withChat, task: 't1', queued: false }, 1), { target: 'none', enabled: false, hint: 'This step has no chat yet' })
    assert.equal(chatButton({ steps: [step()], task: '', queued: false }, 0).target, 'none')
    assert.equal(chatButton({ steps: [], task: 't', queued: false }, 3).target, 'none')
  })
})

describe('when the agent needs an answer', () => {
  it('is Check again being offered', () => {
    assert.equal(canRecheck({ actions: ['recheck', 'retry', 'skip'] }), true)
    assert.equal(canRecheck({ actions: ['retry', 'skip'] }), false)
  })
  it('says what each choice does', () => {
    assert.match(AGENT_ANSWER_NOTE, /choose Check again\. Retry takes the step again in a new chat; Continue anyway accepts it as it is\.$/)
  })
})

describe('Check again', () => {
  it('has its own words and is the primary button', () => {
    const [recheck] = orderedActions(['recheck'])
    assert.equal(recheck.label, 'Check again')
    assert.equal(recheck.hint, "Read the end of the step's chat again: if the agent has finished there, the run goes on")
    assert.equal(primaryAction(['skip', 'retry', 'recheck'])?.name, 'recheck')
  })
  it('puts it before Retry and makes Retry a plain button next to it', () => {
    assert.deepEqual(orderedActions(['stop', 'skip', 'retry', 'recheck']).map((a) => `${a.name}:${a.tone}`), [
      'recheck:primary', 'retry:default', 'skip:default', 'stop:danger',
    ])
    assert.equal(orderedActions(['retry'])[0].tone, 'primary')
  })
})

describe('script steps before a run', () => {
  const script = (over: Partial<StepInfo> = {}) => step({ id: 'test', title: 'Test', kind: 'script', run: 'test', ...over })
  it('has nothing to say without a script step', () => {
    assert.equal(scriptNotice([step(), step({ kind: 'gate' })], 'off'), null)
  })
  it('puts the command in its own part, for <code>', () => {
    const n = scriptNotice([step(), script({ command: 'npm test' })], 'on')
    assert.deepEqual(n?.lines, [{ key: 'test:1', before: 'Test runs ', command: 'npm test', after: ' on the code the agent wrote.' }])
    assert.equal(n?.sandbox, "Script steps run in a sandbox: they can write only in the run's folder and temp folders, and have no network unless you allow it in Settings ▸ Workflows.")
    assert.equal(n?.warn, false)
  })
  it('shows the problem in place of the command', () => {
    const n = scriptNotice([script({ commandProblem: 'no such run configuration' })], 'on')
    assert.deepEqual(n?.lines.map((l) => [l.before, l.command]), [['Test cannot run: no such run configuration', '']])
  })
  it('names the run configuration when Go gave no command', () => {
    assert.equal(scriptNotice([script()], 'on')?.lines[0].before, 'Test runs the run configuration “test” on the code the agent wrote.')
  })
  it('keeps a command with markup as text', () => {
    assert.equal(scriptNotice([script({ command: '<b>x</b> && echo' })], 'on')?.lines[0].command, '<b>x</b> && echo')
  })
  it('warns when the sandbox is not on', () => {
    assert.equal(scriptNotice([script()], 'off')?.warn, true)
    assert.equal(scriptNotice([script()], 'unavailable')?.warn, true)
  })

  it('says what happens before the first run without a sandbox', () => {
    assert.equal(consentText('off', false), UNSANDBOXED_ASKS)
    assert.equal(consentText('off', true), UNSANDBOXED_ALLOWED)
    assert.equal(consentText('on', true), '')
    assert.equal(consentText('unavailable', false), '')
    assert.match(sandboxText('on'), /^Script steps run in a sandbox: .*no network unless you allow it in Settings ▸ Workflows\.$/)
    assert.match(sandboxText('off'), /without a sandbox.*The sandbox is switched off in Settings ▸ Workflows\.$/)
    assert.ok(!sandboxText('off').includes('You can switch'))
    assert.match(sandboxText('unavailable'), /not available on this computer, so script steps will fail: switch off “Run script steps in a sandbox” in Settings ▸ Workflows to run them without one \(Flint asks you once per folder\)\.$/)
    assert.equal(sandboxText('other'), '')
  })
})

describe('a changed schedule', () => {
  it('has one sentence', () => {
    assert.equal(
      SCHEDULE_CHANGED_NOTE,
      'The workflow or a command it runs changed since you saved this schedule. It will not run until you accept the change.',
    )
  })
})

describe('action tooltips', () => {
  const approve = orderedActions(['approve'])[0]
  it('says Approve finishes the run on the last step', () => {
    assert.equal(actionHint(approve, run({ current: 4 })), 'Approve and finish the run')
    assert.match(actionHint(approve, run({ current: 3 })), /goes on with the next one/)
    assert.equal(actionHint(orderedActions(['retry'])[0], run({ current: 4 })), 'Take the failed step again')
  })
  it('uses one wording for continuing and pausing', () => {
    assert.deepEqual(orderedActions(['skip', 'pause']).map((a) => a.label), ['Continue anyway', 'Pause'])
    for (const a of orderedActions(['approve', 'resume', 'recheck', 'retry', 'sendBack', 'skip', 'pause', 'stop', 'remove'])) {
      assert.ok(!/\bskip|\bhold\b|\bheld\b|\bgate\b/i.test(`${a.label} ${a.hint} ${a.confirm}`), a.name)
    }
  })
})

describe('the diagram view of a workflow', () => {
  const wf = (over: Partial<WorkflowInfo> = {}): WorkflowInfo => ({ name: 'checked', description: ' Plan, test. ', source: 'project', path: '/r/.flint/workflows/checked.md', steps: FLOW, problem: '', ...over })
  it('titles the tab and keeps folder and name', () => {
    assert.equal(workflowViewTitle('checked'), 'Workflow: checked')
    assert.notEqual(workflowViewKey('/a', 'b'), workflowViewKey('/a/b', ''))
    assert.deepEqual(workflowViewData({ dir: '/r', name: 'x' }), { dir: '/r', name: 'x' })
    for (const bad of [null, 'x', {}, { dir: '', name: 'x' }, { dir: '/r', name: 3 }]) assert.equal(workflowViewData(bad), null)
  })
  it('words the header', () => {
    assert.deepEqual(workflowHeader(wf()), { name: 'checked', source: 'this repository', description: 'Plan, test.', steps: '5 steps' })
    assert.equal(workflowHeader(wf({ source: 'builtin', steps: [FLOW[0]] })).steps, '1 step')
    assert.equal(workflowHeader(wf({ source: 'builtin' })).source, 'built-in')
  })
  it('tells loading, error, gone, problem and ready apart', () => {
    assert.deepEqual(workflowViewState(null, '', 'checked'), { kind: 'loading' })
    assert.deepEqual(workflowViewState(null, 'boom', 'checked'), { kind: 'error', text: 'boom' })
    assert.deepEqual(workflowViewState({ workflows: [] }, '', 'checked'), { kind: 'missing', text: WORKFLOW_GONE })
    const ok = workflowViewState({ workflows: [wf()] }, '', 'checked')
    assert.equal(ok.kind === 'ready' && ok.problem, '')
    const bad = workflowViewState({ workflows: [wf({ problem: 'bad front matter' })] }, '', 'checked')
    assert.equal(bad.kind === 'ready' && bad.problem, 'bad front matter')
    const empty = workflowViewState({ workflows: [wf({ steps: [] })] }, '', 'checked')
    assert.equal(empty.kind === 'ready' && empty.problem, 'It has no steps.')
  })
  it('keeps a pick only while the step exists', () => {
    assert.equal(clampStep(null, 3), null)
    assert.equal(clampStep(2, 3), 2)
    assert.equal(clampStep(3, 3), null)
  })
  it('recognises workflow files', () => {
    assert.equal(isWorkflowFile('/r/.flint/workflows/a.md'), true)
    assert.equal(isWorkflowFile('/r/.flint/workflows/sub/a.md'), false)
    assert.equal(isWorkflowFile('/r/docs/a.md'), false)
    assert.equal(workflowFileRoot('/r/x/.flint/workflows/a.md'), '/r/x')
    assert.equal(workflowFileRoot('/r/a.md'), '')
    assert.equal(workflowForPath([wf()], '/r/.flint/workflows/checked.md')?.name, 'checked')
    assert.equal(workflowForPath([wf()], '/other.md'), undefined)
  })
})

describe('a step without text', () => {
  it('says what it does instead', () => {
    assert.match(noTextNote('script'), /runs the command/)
    assert.match(noTextNote('gate'), /until you approve/)
    assert.match(noTextNote('agent'), /task is what the agent gets/)
  })
})

describe('runTooltip', () => {
  it('shows the title once when the task is the same text', () => {
    assert.equal(runTooltip({ title: 'Create hello.txt as the step says.', workflow: 'w', description: 'Create hello.txt as the step says. ' }), 'Create hello.txt as the step says.')
  })
  it('adds the task when it says more, and falls back to the workflow without a title', () => {
    assert.equal(runTooltip({ title: 'Add login', workflow: 'ship', description: 'Add a login form\nwith tests' }), 'Add login\n\nAdd a login form\nwith tests')
    assert.equal(runTooltip({ title: '', workflow: 'ship', description: '' }), 'ship')
  })
})

describe('workflowHistory', () => {
  const runs = [
    run({ id: 'new', state: 'running', created: 30 }),
    run({ id: 'old', state: 'done', created: 10 }),
    run({ id: 'mid', state: 'failed', created: 20 }),
    run({ id: 'other', workflow: 'review', state: 'done', created: 5 }),
    run({ id: 'elsewhere', dir: '/x', state: 'done', created: 6 }),
  ]
  it('keeps the runs of one workflow in one folder, oldest first', () => {
    assert.deepEqual(workflowHistory(runs, '/w', 'ship').map((h) => [h.id, h.state]), [['old', 'ok'], ['mid', 'failed'], ['new', 'running']])
  })
  it('shows a run that waits for the user as a warning and a stopped one as skipped', () => {
    const waiting = [run({ id: 'a', state: 'waiting', needsUser: true }), run({ id: 'b', state: 'stopped', created: 1 }), run({ id: 'c', state: 'paused', created: 2 })]
    assert.deepEqual(workflowHistory(waiting, '/w', 'ship').map((h) => h.state), ['warn', 'skipped', 'warn'])
  })
  it('names each run by its title and state', () => {
    assert.equal(workflowHistory(runs, '/w', 'ship')[0]?.title, 'Add login: Done')
  })
  it('keeps only the newest runs', () => {
    assert.deepEqual(workflowHistory(runs, '/w', 'ship', 2).map((h) => h.id), ['mid', 'new'])
    assert.deepEqual(workflowHistory(runs, '/w', 'ship', 0), [])
  })
  it('is empty for a workflow that never ran', () => {
    assert.deepEqual(workflowHistory(runs, '/w', 'never'), [])
  })
})
