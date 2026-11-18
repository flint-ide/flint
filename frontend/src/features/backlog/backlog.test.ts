import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  agentText,
  allReposResolve,
  boardProblem,
  canClose,
  canOpenTask,
  coerceBoardData,
  delegateIssues,
  delegateStep,
  dragPayload,
  dropStatus,
  groupByStatus,
  locateQuestion,
  otherStatuses,
  parseDrop,
  pickSelected,
  problemPrompt,
  repoMarkers,
  reposText,
  statusLabel,
  taskPill,
  touchesBacklog,
  trustBacklogQuestion,
  trustQuestion,
  type ItemInfo,
  type RepoView,
  type TaskInfo,
} from './backlog'

const item = (over: Partial<ItemInfo> = {}): ItemInfo => ({
  id: 'a', title: 'A', status: 'todo', priority: 'normal', agent: '', repos: [], body: '', path: '/b/items/a.md', agentProblem: '', failed: false, tasks: [], ...over,
})
const repo = (over: Partial<RepoView> = {}): RepoView => ({
  name: 'api', url: 'git@h:a/api.git', folder: '/w/api', located: false, inWorkspace: true, trusted: true, problem: '', ...over,
})
const task = (over: Partial<TaskInfo> = {}): TaskInfo => ({
  id: 't', repo: 'api', title: '', agent: '', branch: '', worktree: '/w/t', state: 'working', error: '', pullRequest: '', merged: false, stale: false, gone: false, ...over,
})

describe('grouping', () => {
  it('groups by status and keeps the arrival order', () => {
    const g = groupByStatus([item({ id: 'a', status: 'done' }), item({ id: 'b', status: 'todo' }), item({ id: 'c', status: 'todo' }), item({ id: 'd', status: 'doing' })])
    assert.deepEqual(g.todo.map((i) => i.id), ['b', 'c'])
    assert.deepEqual(g.doing.map((i) => i.id), ['d'])
    assert.deepEqual(g.review, [])
    assert.deepEqual(g.done.map((i) => i.id), ['a'])
  })
  it('puts an unknown status with the to do items', () => {
    assert.equal(groupByStatus([item({ status: 'blocked' })]).todo.length, 1)
  })
  it('offers every other status', () => {
    assert.deepEqual(otherStatuses('doing'), ['todo', 'review', 'done'])
  })
  it('labels statuses', () => {
    assert.equal(statusLabel('todo'), 'To do')
    assert.equal(statusLabel('x'), 'x')
  })
})

describe('labels', () => {
  it('shows the item agent, else the default one', () => {
    const label = (n: string) => `${n} · claude`
    assert.equal(agentText(item({ agent: 'backend' }), 'main', label), 'backend · claude')
    assert.equal(agentText(item(), 'claude'), 'default: claude')
    assert.equal(agentText(item(), ''), 'default: no agent available')
  })
  it('lists repositories', () => {
    assert.equal(reposText(item({ repos: ['api', 'web'] })), 'api, web')
    assert.equal(reposText(item()), 'active project')
  })
})

describe('delegate preconditions', () => {
  it('has none for a ready item', () => {
    assert.deepEqual(delegateIssues(item({ repos: ['api'] }), [repo()], '/w'), [])
    assert.deepEqual(delegateIssues(item(), [], '/w'), [])
  })
  it('names the agent first, with nothing to offer', () => {
    const [i] = delegateIssues(item({ agentProblem: 'no agent', repos: ['api'] }), [repo({ folder: '' })], '/w')
    assert.deepEqual([i.fix, i.message], ['none', 'no agent'])
  })
  it('needs a project for an item without repositories', () => {
    assert.equal(delegateIssues(item(), [], '')[0].fix, 'project')
  })
  it('offers Locate for a repository without a checkout or with a problem', () => {
    assert.equal(delegateIssues(item({ repos: ['api'] }), [repo({ folder: '' })], '/w')[0].fix, 'locate')
    const [p] = delegateIssues(item({ repos: ['api'] }), [repo({ located: true, problem: 'no longer a checkout' })], '/w')
    assert.deepEqual([p.fix, p.message], ['locate', 'api: no longer a checkout'])
  })
  it('attaches a located folder outside the workspace', () => {
    const [i] = delegateIssues(item({ repos: ['api'] }), [repo({ located: true, inWorkspace: false })], '/w')
    assert.deepEqual([i.fix, i.folder], ['attach', '/w/api'])
  })
  it('asks to trust an untrusted checkout', () => {
    assert.equal(delegateIssues(item({ repos: ['api'] }), [repo({ trusted: false })], '/w')[0].fix, 'trust')
  })
  it('lists every repository that is not ready, in the item order', () => {
    const issues = delegateIssues(item({ repos: ['web', 'api', 'db'] }), [repo({ name: 'api', trusted: false }), repo({ name: 'web', folder: '' })], '/w')
    assert.deepEqual(issues.map((i) => [i.repo, i.fix]), [['web', 'locate'], ['api', 'trust'], ['db', 'none']])
  })
})

describe('delegate walk-through', () => {
  const ready = repo()
  const missing = repo({ name: 'web', url: 'https://h/a/web', folder: '' })
  const issuesFor = (repos: RepoView[]) => delegateIssues(item({ repos: repos.map((r) => r.name) }), repos, '/w')
  it('delegates when nothing is in the way', () => {
    assert.deepEqual(delegateStep(issuesFor([ready]), ''), { kind: 'delegate' })
  })
  it('fixes the first issue, carrying the URL to locate', () => {
    const step = delegateStep(issuesFor([missing, repo({ name: 'db', trusted: false })]), '')
    assert.equal(step.kind, 'fix')
    if (step.kind !== 'fix') return
    assert.deepEqual([step.issue.fix, step.issue.repo, step.issue.url], ['locate', 'web', 'https://h/a/web'])
    // The fix worked: the next issue is a new one.
    const next = delegateStep(issuesFor([ready, repo({ name: 'db', trusted: false })]), step.sig)
    assert.equal(next.kind === 'fix' && next.issue.fix, 'trust')
  })
  it('stops with the reason when a step made no progress', () => {
    const first = delegateStep(issuesFor([missing]), '')
    assert.equal(first.kind, 'fix')
    const again = delegateStep(issuesFor([missing]), first.kind === 'fix' ? first.sig : '')
    assert.deepEqual(again, { kind: 'stuck', message: "Delegate can't go on: No checkout of web was found: locate it." })
  })
  it('stops with the reason for an issue it cannot fix', () => {
    assert.deepEqual(delegateStep(delegateIssues(item({ agentProblem: 'no agent can start' }), [], '/w'), ''), {
      kind: 'stuck', message: "Delegate can't go on: no agent can start",
    })
    assert.equal(delegateStep(delegateIssues(item(), [], ''), '').kind, 'stuck')
  })
  it('says which repository to locate, and that trust is for the whole folder from now on', () => {
    assert.match(locateQuestion('web', 'https://h/a/web'), /"web"[\s\S]*https:\/\/h\/a\/web/)
    const q = trustQuestion('api', '/w/api')
    assert.match(q, /^Trust the folder \/w\/api\?/)
    assert.match(q, /whole folder from now on, not just for this Delegate/)
    assert.match(trustBacklogQuestion('Acme', '/plan'), /^Trust the folder \/plan\?[\s\S]*"Acme"/)
  })
})

describe('repositories group', () => {
  it('marks what keeps a repository from being used', () => {
    assert.deepEqual(repoMarkers(repo()), [])
    assert.deepEqual(repoMarkers(repo({ folder: '' })).map((m) => m.label), ['no checkout'])
    assert.deepEqual(repoMarkers(repo({ located: true, problem: 'gone' })).map((m) => [m.label, m.hint]), [['problem', 'gone']])
    assert.deepEqual(repoMarkers(repo({ located: true, inWorkspace: false, trusted: false })).map((m) => m.label), ['located', 'not in workspace', 'Restricted Mode'])
  })
  it('is collapsed when every repository resolves', () => {
    assert.equal(allReposResolve([repo(), repo({ name: 'web', located: true })]), true)
    assert.equal(allReposResolve([repo(), repo({ name: 'web', trusted: false })]), false)
    assert.equal(allReposResolve([]), true)
  })
})

describe('agent tasks', () => {
  it('shows the Agent Tasks states, merged and stale', () => {
    assert.deepEqual(taskPill(task({ state: 'waiting' })).label, 'waiting for you')
    assert.equal(taskPill(task({ state: 'finished', merged: true })).label, 'merged')
    assert.equal(taskPill(task({ stale: true })).cls, 'stale')
    assert.equal(taskPill(task({ state: 'failed', error: 'boom' })).hint, 'boom')
    assert.equal(taskPill(task({ gone: true, stale: true, state: 'finished' })).label, 'finished')
  })
  it('opens only live, started tasks', () => {
    assert.equal(canOpenTask(task()), true)
    assert.equal(canOpenTask(task({ gone: true })), false)
    assert.equal(canOpenTask(task({ stale: true })), false)
    assert.equal(canOpenTask(task({ state: 'queued' })), false)
  })
})

describe('selection', () => {
  const infos = [{ path: '/a', name: 'a', workspace: false, trusted: true, error: '' }, { path: '/b', name: 'b', workspace: true, trusted: true, error: '' }]
  it('keeps a known backlog, else the first', () => {
    assert.equal(pickSelected(infos, '/b'), '/b')
    assert.equal(pickSelected(infos, '/gone'), '/a')
    assert.equal(pickSelected([], '/a'), '')
  })
  it('closes remembered backlogs only', () => {
    assert.equal(canClose(infos[0]), true)
    assert.equal(canClose(infos[1]), false)
    assert.equal(canClose(undefined), false)
  })
})

describe('file changes', () => {
  it('reacts to the config and the items only', () => {
    assert.equal(touchesBacklog('/b', ['/b/items/a.md']), true)
    assert.equal(touchesBacklog('/b', ['/b/flint-backlog.json']), true)
    assert.equal(touchesBacklog('/b/', ['/b/items/x/y.md']), true)
    assert.equal(touchesBacklog('/b', ['/b/src/a.go', '/b/items2/a.md', '/other/items/a.md']), false)
    assert.equal(touchesBacklog('', ['/items/a.md']), false)
  })
})

describe('board', () => {
  it('validates restored tab data', () => {
    assert.deepEqual(coerceBoardData({ folder: '/b' }), { folder: '/b' })
    for (const bad of [null, undefined, 'x', 3, {}, { folder: 3 }, { folder: 'rel/b' }, { folder: '' }]) assert.equal(coerceBoardData(bad), null)
  })
  it('explains a backlog that is gone or unreadable', () => {
    const infos = [{ path: '/a', name: 'a', workspace: false, trusted: true, error: '' }, { path: '/c', name: 'c', workspace: false, trusted: true, error: 'no such file' }]
    assert.equal(boardProblem('/a', infos), null)
    assert.equal(boardProblem('/a', null), null)
    assert.match(boardProblem('/b', infos) ?? '', /not known any more/)
    assert.match(boardProblem('/c', infos) ?? '', /no such file/)
  })
  it('decides what a drop does', () => {
    assert.equal(dropStatus('todo', 'doing'), 'doing')
    assert.equal(dropStatus('todo', 'todo'), null)
    assert.equal(dropStatus('todo', 'blocked'), null)
  })
  it('reads only cards of its own backlog', () => {
    const raw = dragPayload('/b', item({ id: 'x', status: 'review' }))
    assert.deepEqual(parseDrop(raw, '/b'), { id: 'x', status: 'review' })
    assert.equal(parseDrop(raw, '/other'), null)
    for (const bad of ['', 'nope', '[]', 'null', '{"folder":"/b"}', '{"folder":"/b","id":"","status":"todo"}']) assert.equal(parseDrop(bad, '/b'), null)
  })
})

describe('problems', () => {
  it('asks the agent about the file and the reason', () => {
    const p = problemPrompt('Acme', '/b', { file: 'items/x.md', reason: 'title is required' })
    assert.match(p, /items\/x\.md: title is required/)
    assert.match(p, /"Acme" \(\/b\)/)
  })
})
