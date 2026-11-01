import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  coerceAgentTasks,
  DEFAULT_AGENT_TASKS,
  failurePrompt,
  formatChanges,
  mergeTaskLists,
  pruneChanges,
  removeQuestions,
  stateInfo,
  taskActions,
  tasksTouchedBy,
  validBranchPrefix,
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
    assert.deepEqual(removeQuestions({ stale: false, changes: 0, unmerged: 0 }), { discard: null, branch: null })
    const q = removeQuestions({ stale: false, changes: 2, unmerged: 3 })
    assert.match(q.discard ?? '', /2 uncommitted/)
    assert.match(q.branch ?? '', /3 commit/)
    assert.match(removeQuestions({ stale: true, changes: 0, unmerged: -1 }).branch ?? '', /could not tell/)
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
    assert.deepEqual(coerceAgentTasks({ worktreeDir: 'rel', branchPrefix: 'me/' }), { worktreeDir: '', branchPrefix: 'me/' })
  })
})
