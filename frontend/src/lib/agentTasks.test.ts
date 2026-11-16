import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  attemptLabels,
  changeCell,
  coerceAgentTasks,
  DEFAULT_AGENT_TASKS,
  failurePrompt,
  formatChanges,
  formatTaskUsage,
  groupMates,
  groupRows,
  groupSummary,
  MAX_GROUP,
  mergeTaskLists,
  planAgents,
  planNote,
  planProblem,
  pruneChanges,
  removeOthersQuestion,
  removeQuestions,
  stateInfo,
  taskActions,
  tasksTouchedBy,
  validBranchPrefix,
  validMaxParallel,
  validWorktreeDir,
  type TaskView,
} from './agentTasks'

const task = (over: Partial<TaskView> = {}): TaskView => ({
  id: 'a1',
  title: 'Add a button',
  prompt: 'Add a button',
  agent: 'claude',
  branch: 'agent/add-a-button',
  base: 'main',
  worktree: '/src/p.worktrees/agent-add-a-button',
  checkout: '/src/p',
  createdAt: 1,
  pullRequest: '',
  state: 'idle',
  error: '',
  stale: false,
  ahead: 0,
  merged: false,
  group: '',
  tokens: 0,
  costUsd: 0,
  ...over,
})

describe('stateInfo', () => {
  it('labels every state and falls back for unknown ones', () => {
    assert.equal(stateInfo('waiting').label, 'waiting for you')
    assert.equal(stateInfo('working').label, 'working')
    assert.equal(stateInfo('bogus').label, 'idle')
  })
})

describe('formatChanges', () => {
  it('counts files and lines', () => {
    assert.equal(formatChanges({ count: 3, added: 12, deleted: 4 }), '3 files +12 −4')
    assert.equal(formatChanges({ count: 1, added: 0, deleted: 0 }), '1 file +0 −0')
    assert.equal(formatChanges({ count: 0, added: 0, deleted: 0 }), 'no changes')
    assert.equal(formatChanges(undefined), '')
  })
})

describe('taskActions', () => {
  it('offers only removal for a stale task', () => {
    const a = taskActions(task({ stale: true, ahead: 2 }))
    assert.deepEqual([a.open, a.diff, a.merge, a.pr, a.remove], [false, false, false, false, true])
  })

  it('needs commits before merging or publishing', () => {
    assert.equal(taskActions(task()).merge, false)
    const a = taskActions(task({ ahead: 1 }))
    assert.equal(a.merge && a.pr, true)
  })

  it('does not merge or publish while the agent works', () => {
    const a = taskActions(task({ ahead: 1, state: 'working' }))
    assert.equal(a.merge || a.pr, false)
  })

  it('offers the agent hand-off only for failures', () => {
    assert.equal(taskActions(task({ state: 'failed' })).askAgent, true)
    assert.equal(taskActions(task({ state: 'finished' })).askAgent, false)
  })

  it('offers no second pull request', () => {
    assert.equal(taskActions(task({ ahead: 1, pullRequest: 'https://x/pull/1' })).pr, false)
  })
})

describe('failurePrompt', () => {
  it('quotes the error and asks to continue', () => {
    const p = failurePrompt(task({ state: 'failed', error: 'rate limited' }))
    assert.match(p, /rate limited/)
    assert.match(p, /Add a button/)
  })
})

describe('tasksTouchedBy', () => {
  it('maps changed paths to the tasks whose worktree holds them', () => {
    const tasks = [task(), task({ id: 'b2', worktree: '/src/p.worktrees/b' })]
    assert.deepEqual(tasksTouchedBy(['/src/p.worktrees/b/x.go', '/elsewhere/y'], tasks), ['b2'])
    assert.deepEqual(tasksTouchedBy(['/src/p.worktrees/bb/x.go'], tasks), [])
  })
})

describe('mergeTaskLists', () => {
  it('dedupes tasks listed from several folders of one repository, newest first', () => {
    const got = mergeTaskLists([[task({ id: 'a', createdAt: 1 }), task({ id: 'b', createdAt: 3 })], [task({ id: 'a', createdAt: 1 })]])
    assert.deepEqual(
      got.map((t) => t.id),
      ['b', 'a'],
    )
  })
})

describe('pruneChanges', () => {
  it('keeps the changes of tasks that still exist', () => {
    const changes = { a: { count: 1, added: 1, deleted: 0 }, gone: { count: 2, added: 0, deleted: 0 } }
    assert.deepEqual(Object.keys(pruneChanges(changes, [task({ id: 'a' })])), ['a'])
    assert.equal(pruneChanges(changes, [task({ id: 'a' }), task({ id: 'gone' })]), changes)
  })
})

describe('removeQuestions', () => {
  it('asks about lost work only when there is some', () => {
    assert.deepEqual(removeQuestions({ stale: false, changes: 0, ignored: 0, unmerged: 0 }), { discard: null, branch: null })
    const q = removeQuestions({ stale: false, changes: 2, ignored: 0, unmerged: 3 })
    assert.match(q.discard ?? '', /2 uncommitted/)
    assert.match(q.branch ?? '', /3 commit/)
    assert.match(removeQuestions({ stale: true, changes: 0, ignored: 0, unmerged: -1 }).branch ?? '', /could not tell/)
  })

  it('counts ignored files (.env) as lost work', () => {
    const q = removeQuestions({ stale: false, changes: 0, ignored: 1, unmerged: 0 })
    assert.match(q.discard ?? '', /1 ignored/)
  })
})

describe('agent task settings', () => {
  it('validates like Go', () => {
    assert.equal(validBranchPrefix('agent/'), true)
    assert.equal(validBranchPrefix(''), true)
    assert.equal(validBranchPrefix('-x'), false)
    assert.equal(validBranchPrefix('a..b'), false)
    assert.equal(validBranchPrefix('a b'), false)
    assert.equal(validWorktreeDir(''), true)
    assert.equal(validWorktreeDir('~/wt'), true)
    assert.equal(validWorktreeDir('/abs'), true)
    assert.equal(validWorktreeDir('rel/dir'), false)
  })

  it('coerces untrusted JSON', () => {
    assert.deepEqual(coerceAgentTasks(undefined), DEFAULT_AGENT_TASKS)
    assert.deepEqual(coerceAgentTasks({ worktreeDir: 'rel', branchPrefix: 'me/' }), { worktreeDir: '', branchPrefix: 'me/', maxParallel: 3 })
  })

  it('keeps a valid limit of working tasks, 0 included, and defaults the rest', () => {
    assert.equal(DEFAULT_AGENT_TASKS.maxParallel, 3)
    assert.equal(coerceAgentTasks({ maxParallel: 0 }).maxParallel, 0)
    assert.equal(coerceAgentTasks({ maxParallel: 8 }).maxParallel, 8)
    for (const bad of [-1, 33, 1.5, '2', null, NaN]) assert.equal(coerceAgentTasks({ maxParallel: bad }).maxParallel, 3, String(bad))
    assert.equal(validMaxParallel(32), true)
    assert.equal(validMaxParallel(33), false)
  })
})

describe('queued tasks', () => {
  it('have their own state', () => {
    assert.equal(stateInfo('queued').state, 'queued')
    assert.equal(stateInfo('queued').label, 'queued')
  })

  it('can be started now or removed, not opened or merged', () => {
    const a = taskActions(task({ state: 'queued' }))
    assert.deepEqual([a.startNow, a.open, a.diff, a.merge, a.pr, a.remove], [true, false, false, false, false, true])
    assert.equal(taskActions(task()).startNow, false)
    assert.equal(taskActions(task({ state: 'queued', stale: true })).startNow, false)
  })
})

describe('formatTaskUsage', () => {
  it('shows tokens and cost once there are any', () => {
    assert.equal(formatTaskUsage(task()), '')
    assert.equal(formatTaskUsage(task({ tokens: 12_300 })), '12k tokens')
    assert.equal(formatTaskUsage(task({ tokens: 1234, costUsd: 2.5 })), '1.2k tokens · $2.50')
  })
})

describe('planAgents', () => {
  it('is one task per agent and attempt, first attempts first', () => {
    assert.deepEqual(planAgents(['claude'], 1), ['claude'])
    assert.deepEqual(planAgents(['claude', 'codex'], 1), ['claude', 'codex'])
    assert.deepEqual(planAgents(['claude', 'codex'], 2), ['claude', 'codex', 'claude', 'codex'])
    assert.deepEqual(planAgents(['claude'], 3), ['claude', 'claude', 'claude'])
  })

  it('drops repeats and empty choices and bad counts', () => {
    assert.deepEqual(planAgents(['claude', '', 'claude'], 1), ['claude'])
    assert.deepEqual(planAgents(['claude'], 0), ['claude'])
    assert.deepEqual(planAgents(['claude'], NaN), ['claude'])
    assert.deepEqual(planAgents([], 2), [])
  })

  it('is bounded like Go', () => {
    assert.equal(planProblem([]), 'Choose an agent.')
    assert.equal(planProblem(planAgents(['a', 'b'], 3)), null)
    assert.equal(planAgents(['a', 'b'], 3).length, MAX_GROUP)
    assert.match(planProblem(planAgents(['a', 'b', 'c'], 3)) ?? '', /at most 6/)
  })

  it('says what Start will do', () => {
    assert.equal(planNote(['claude'], 3), '')
    assert.match(planNote(['claude', 'codex'], 3), /^Starts 2 tasks from the same commit/)
    assert.equal(/queue/.test(planNote(['claude', 'codex'], 3)), false)
    assert.match(planNote(['a', 'b', 'c', 'd'], 3), /At most 3 work at the same time; the rest wait in the queue/)
    assert.equal(/queue/.test(planNote(['a', 'b', 'c', 'd'], 0)), false)
  })
})

describe('groups', () => {
  // As mergeTaskLists gives them: newest first.
  const tasks = [
    task({ id: 'solo2', createdAt: 9 }),
    task({ id: 'g1a', group: 'g1', agent: 'claude', createdAt: 5 }),
    task({ id: 'g1b', group: 'g1', agent: 'codex', createdAt: 5 }),
    task({ id: 'g1c', group: 'g1', agent: 'claude', createdAt: 5 }),
    task({ id: 'solo1', createdAt: 3 }),
    task({ id: 'left', group: 'g2', createdAt: 1 }),
  ]

  it('shows the tasks of a group together, in the order they were created', () => {
    const rows = groupRows(tasks)
    assert.deepEqual(
      rows.map((r) => [r.group, r.tasks.map((t) => t.id).join(',')]),
      [['', 'solo2'], ['g1', 'g1a,g1b,g1c'], ['', 'solo1'], ['', 'left']],
    )
  })

  it('gathers a group whose tasks are apart in the list, oldest first', () => {
    const rows = groupRows([task({ id: 'b', group: 'g', createdAt: 7 }), task({ id: 'x', createdAt: 6 }), task({ id: 'a', group: 'g', createdAt: 5 })])
    assert.deepEqual(rows.map((r) => r.tasks.map((t) => t.id).join(',')), ['a,b', 'x'])
  })

  it('treats a group with one task left as a plain task', () => {
    assert.deepEqual(groupRows([task({ id: 'left', group: 'g2' })]), [{ group: '', tasks: [task({ id: 'left', group: 'g2' })] }])
    assert.deepEqual(groupRows([]), [])
  })

  it('finds the other tasks of a group', () => {
    assert.deepEqual(groupMates(tasks[1], tasks).map((t) => t.id), ['g1b', 'g1c'])
    assert.deepEqual(groupMates(tasks[0], tasks), [])
    assert.deepEqual(groupMates(tasks[5], tasks), [])
  })

  it('names tasks by agent, numbering attempts', () => {
    assert.deepEqual(attemptLabels(tasks.slice(1, 4)), { g1a: 'claude #1', g1b: 'codex', g1c: 'claude #2' })
  })

  it('summarises the states of a group', () => {
    const states = [task({ state: 'working' }), task({ state: 'queued' }), task({ state: 'working' }), task({ state: 'waiting' }), task({ stale: true, state: 'finished' })]
    assert.equal(groupSummary(states), '2 working · 1 waiting for you · 1 queued · 1 stale')
    assert.equal(groupSummary([]), '')
  })

  it('asks before removing the tasks that were not kept', () => {
    const q = removeOthersQuestion(task({ branch: 'agent/x-claude' }), [task({ branch: 'agent/x-codex' })])
    assert.match(q, /Kept "agent\/x-claude"\. Remove the other task of this comparison \(agent\/x-codex\)\?/)
    assert.match(q, /asks again before deleting uncommitted work or an unmerged branch/)
    assert.match(removeOthersQuestion(task(), [task({ branch: 'b1' }), task({ branch: 'b2' })]), /the other 2 tasks of this comparison \(b1, b2\)/)
  })
})

describe('changeCell', () => {
  it('shows what a task did to a file', () => {
    const c = { origPath: '', added: 3, deleted: 1, binary: false, untracked: false }
    assert.equal(changeCell(c), '+3 −1')
    assert.equal(changeCell({ ...c, binary: true }), 'bin')
    assert.equal(changeCell(null), '—')
    assert.equal(changeCell(undefined), '—')
  })
})
