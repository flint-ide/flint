import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  agentText,
  allReposResolve,
  applyCloneEvent,
  autoStartNotice,
  autoStartToast,
  cloneNotice,
  coerceItemEvent,
  reviewText,
  boardProblem,
  canClone,
  canClose,
  canOpenTask,
  cloneFailure,
  clonedText,
  cloneFromInfo,
  cloneTitle,
  coerceAutoStart,
  coerceBoardData,
  coerceCloneEvent,
  contextLine,
  dependencyLines,
  dependencyMarker,
  delegateIssues,
  delegateStep,
  dragPayload,
  dropStatus,
  goalProblem,
  groupByStatus,
  groupTasks,
  isRestrictedError,
  itemTitleOf,
  joinList,
  locateQuestion,
  MAX_GOAL,
  noCheckoutQuestion,
  notReadyQuestion,
  otherStatuses,
  parseDrop,
  pickPlanAgent,
  pickSelected,
  problemPrompt,
  startedText,
  titlesOf,
  togetherMarker,
  togetherNote,
  waitingToast,
  repoMarkers,
  reposText,
  statusLabel,
  taskPill,
  touchesProject,
  trustProjectQuestion,
  trustQuestion,
  type ItemInfo,
  type RepoView,
  type TaskInfo,
} from './projects'

const item = (over: Partial<ItemInfo> = {}): ItemInfo => ({
  id: 'a', title: 'A', status: 'todo', priority: 'normal', agent: '', repos: [], body: '', path: '/b/items/a.md', agentProblem: '', failed: false, tasks: [], together: false, after: [], waitingFor: [], ready: true, dependencyProblem: '', waiting: null, ...over,
})
const repo = (over: Partial<RepoView> = {}): RepoView => ({
  name: 'api', url: 'git@h:a/api.git', folder: '/w/api', located: false, inWorkspace: true, trusted: true, problem: '', ...over,
})
const task = (over: Partial<TaskInfo> = {}): TaskInfo => ({
  id: 't', repo: 'api', title: '', agent: '', branch: '', worktree: '/w/t', state: 'working', error: '', pullRequest: '', merged: false, stale: false, gone: false, joint: '', lead: false, ...over,
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
    assert.equal(reposText(item()), 'active folder')
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
  it('needs an active folder for an item without repositories', () => {
    assert.equal(delegateIssues(item(), [], '')[0].fix, 'folder')
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
    assert.match(trustProjectQuestion('Acme', '/plan'), /^Trust the folder \/plan\?[\s\S]*"Acme"/)
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
  const infos = [{ path: '/a', name: 'a', workspace: false, trusted: true, error: '', oldFormat: false }, { path: '/b', name: 'b', workspace: true, trusted: true, error: '', oldFormat: false }]
  it('keeps a known project, else the first', () => {
    assert.equal(pickSelected(infos, '/b'), '/b')
    assert.equal(pickSelected(infos, '/gone'), '/a')
    assert.equal(pickSelected([], '/a'), '')
  })
  it('closes remembered projects only', () => {
    assert.equal(canClose(infos[0]), true)
    assert.equal(canClose(infos[1]), false)
    assert.equal(canClose(undefined), false)
  })
})

describe('file changes', () => {
  it('reacts to the config and the items only', () => {
    assert.equal(touchesProject('/b', ['/b/items/a.md']), true)
    assert.equal(touchesProject('/b', ['/b/flint-project.json']), true)
    assert.equal(touchesProject('/b', ['/b/flint-backlog.json']), true)
    assert.equal(touchesProject('/b/', ['/b/items/x/y.md']), true)
    assert.equal(touchesProject('/b', ['/b/src/a.go', '/b/items2/a.md', '/other/items/a.md']), false)
    assert.equal(touchesProject('', ['/items/a.md']), false)
  })
})

describe('board', () => {
  it('validates restored tab data', () => {
    assert.deepEqual(coerceBoardData({ folder: '/b' }), { folder: '/b' })
    for (const bad of [null, undefined, 'x', 3, {}, { folder: 3 }, { folder: 'rel/b' }, { folder: '' }]) assert.equal(coerceBoardData(bad), null)
  })
  it('explains a project that is gone or unreadable', () => {
    const infos = [{ path: '/a', name: 'a', workspace: false, trusted: true, error: '', oldFormat: false }, { path: '/c', name: 'c', workspace: false, trusted: true, error: 'no such file', oldFormat: false }]
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
  it('reads only cards of its own project', () => {
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
    assert.match(p, /format of a project item/)
  })

  it('asks for a context file Flint can send', () => {
    const p = problemPrompt('Acme', '/b', { file: 'PROJECT.md', reason: 'contains invisible characters (U+200B)' })
    assert.match(p, /PROJECT\.md: contains invisible characters/)
    assert.match(p, /at most 16,000 characters/)
    assert.ok(!p.includes('project item'))
  })

  it('asks about a flint-backlog.json left over next to flint-project.json without deleting it', () => {
    const p = problemPrompt('Acme', '/b', { file: 'flint-backlog.json', reason: 'left over from the old format' })
    assert.match(p, /Compare the two files/)
    assert.match(p, /don't delete it yourself/)
    assert.ok(!p.includes('project item'))
  })
})

describe('project context', () => {
  const cases: [string, Parameters<typeof contextLine>[0], boolean, string, 'open' | 'add' | null, boolean][] = [
    ['a usable file', { path: '/p/PROJECT.md', exists: true, problem: '' }, true, 'PROJECT.md', 'open', false],
    ['a file with a problem', { path: '/p/PROJECT.md', exists: true, problem: 'not UTF-8 text' }, true, 'PROJECT.md (not sent)', 'open', true],
    ['none', { path: '/p/PROJECT.md', exists: false, problem: '' }, true, 'none', 'add', false],
    ['none, untrusted', { path: '/p/PROJECT.md', exists: false, problem: '' }, false, 'none', null, false],
    ['an untrusted file is opened', { path: '/p/PROJECT.md', exists: true, problem: '' }, false, 'PROJECT.md', 'open', false],
    ['no data from an older Go', undefined, true, 'none', 'add', false],
  ]
  for (const [name, c, trusted, text, action, problem] of cases) {
    it(name, () => {
      const line = contextLine(c, trusted)
      assert.equal(line.text, text)
      assert.equal(line.action, action)
      assert.equal(line.problem, problem)
      assert.ok(line.hint.length > 0)
    })
  }
  it('says why a file is not sent', () => {
    assert.match(contextLine({ path: '/p/PROJECT.md', exists: true, problem: 'not UTF-8 text' }, true).hint, /not UTF-8 text/)
  })
})

describe('dependencies', () => {
  const items = [
    item({ id: 'schema', title: 'Design the schema', status: 'doing' }),
    item({ id: 'api', title: 'Build the API', status: 'todo', after: ['schema'], waitingFor: ['schema'], ready: false }),
    item({ id: 'ui', title: 'Build the UI', status: 'todo', after: ['api', 'ghost'], waitingFor: ['api', 'ghost'], ready: false }),
  ]
  it('looks titles up and falls back to the id', () => {
    assert.equal(itemTitleOf('schema', items), 'Design the schema')
    assert.equal(itemTitleOf('ghost', items), 'ghost')
    assert.deepEqual(titlesOf(['api', 'ghost'], items), ['Build the API', 'ghost'])
    assert.equal(joinList([]), '')
    assert.equal(joinList(['a']), 'a')
    assert.equal(joinList(['a', 'b']), 'a and b')
    assert.equal(joinList(['a', 'b', 'c']), 'a, b and c')
  })
  it('marks an item that is not ready, waiting, or has a dependency problem', () => {
    assert.equal(dependencyMarker(items[0], items), null)
    assert.deepEqual(dependencyMarker(items[1], items), { label: 'not ready', cls: 'warn', hint: 'Waits for Design the schema: Delegate offers to start it when it is done' })
    const waiting = item({ ...items[1], waiting: { since: 't', reason: '' } })
    assert.equal(dependencyMarker(waiting, items)?.label, 'waiting to start')
    const bad = item({ id: 'x', after: ['x'], ready: false, dependencyProblem: 'it names itself' })
    assert.deepEqual(dependencyMarker(bad, items), { label: 'dependency problem', cls: 'error', hint: 'it names itself' })
    // A done item does not care what it waited for.
    assert.equal(dependencyMarker(item({ ...items[1], status: 'done' }), items), null)
  })
  it('says what an item waits for', () => {
    assert.deepEqual(dependencyLines(items[0], items), [])
    assert.deepEqual(dependencyLines(items[1], items), [{ kind: 'after', text: 'waits for Design the schema', cls: 'warn' }])
    assert.equal(dependencyLines(items[2], items)[0].text, 'waits for Build the API and ghost')
    const ready = item({ id: 'r', after: ['schema'], waitingFor: [], ready: true })
    assert.deepEqual(dependencyLines(ready, items), [{ kind: 'after', text: 'after Design the schema (done)', cls: 'muted' }])
  })
  it('shows the waiting start with its reason, and the problem', () => {
    const w = item({ ...items[2], waiting: { since: 't', reason: 'api has no checkout' } })
    assert.deepEqual(dependencyLines(w, items).map((l) => [l.kind, l.text]), [
      ['after', 'waits for Build the API and ghost'],
      ['waiting', 'starts when Build the API and ghost are done'],
      ['reason', 'Last start failed: api has no checkout'],
    ])
    assert.equal(dependencyLines(item({ ...items[1], waiting: { since: 't', reason: '' } }), items)[1].text, 'starts when Design the schema is done')
    const ready = item({ after: ['schema'], waitingFor: [], ready: true, waiting: { since: 't', reason: '' } })
    assert.match(dependencyLines(ready, items)[1].text, /^ready: starts as soon as/)
    // With a problem the after line says nothing more (waitingFor is empty then, which would read as done).
    assert.deepEqual(dependencyLines(item({ after: ['a', 'b'], dependencyProblem: 'cycle: a, b' }), items), [{ kind: 'problem', text: 'cycle: a, b', cls: 'error' }])
  })
  it('asks to start an item when its dependencies are done', () => {
    const q = notReadyQuestion(items[1], ['schema'], items)
    assert.match(q, /^Start "Build the API" when Design the schema is done\?/)
    assert.match(q, /Nothing starts now/)
    assert.ok(!/one agent/.test(q))
    assert.match(notReadyQuestion(items[2], ['api', 'schema'], items).split('\n')[0], /when Build the API and Design the schema are done\?$/)
    const together = item({ title: 'Rename', together: true, repos: ['api', 'web'] })
    assert.match(notReadyQuestion(together, ['schema'], items), /\nIt runs as one agent across the repositories api, web\.\n/)
    assert.equal(waitingToast('Build the API', ['schema'], items), '"Build the API" starts when Design the schema is done')
  })
})

describe('one agent across repositories', () => {
  it('marks a together item and names the repositories in the walk-through', () => {
    assert.equal(togetherMarker(item()), null)
    const t = item({ together: true, repos: ['api', 'web'] })
    assert.equal(togetherMarker(t)?.label, 'one agent')
    assert.match(togetherMarker(t)?.hint ?? '', /api, web/)
    assert.match(togetherNote(t), /one agent across the repositories api, web/)
    assert.equal(togetherNote(item({ repos: ['api', 'web'] })), '')
    assert.match(noCheckoutQuestion('api', 'u', togetherNote(t)), /one agent across/)
    assert.match(locateQuestion('api', 'u', 'NOTE'), /NOTE$/)
  })
  it('words the start', () => {
    assert.equal(startedText({ together: false }, 2), 'Started 2 agent tasks')
    assert.equal(startedText({ together: false }, 1), 'Started 1 agent task')
    assert.equal(startedText({ together: true }, 2), 'Started one agent across 2 repositories')
  })
  it('groups the tasks of one joint group, lead first, and opens the lead', () => {
    const tasks = [
      task({ id: 'solo', repo: 'x' }),
      task({ id: 'm', joint: 'j', repo: 'web' }),
      task({ id: 'l', joint: 'j', lead: true, repo: 'api' }),
      task({ id: 'alone', joint: 'k', lead: true }),
    ]
    assert.deepEqual(groupTasks(tasks).map((r) => [r.joint, r.tasks.map((t) => t.id).join(',')]), [['', 'solo'], ['j', 'l,m'], ['', 'alone']])
    assert.equal(canOpenTask(tasks[1], tasks), true)
    // The lead is gone or queued: a member has no chat to open either.
    assert.equal(canOpenTask(tasks[1], [tasks[1], { ...tasks[2], gone: true }]), false)
    assert.equal(canOpenTask(tasks[1], [tasks[1], { ...tasks[2], state: 'queued' }]), false)
    assert.equal(canOpenTask(tasks[1], [tasks[1]]), false)
    assert.equal(canOpenTask(tasks[0]), true)
  })
})

describe('automatic starts', () => {
  it('reads the event and tells how it went', () => {
    const ok = coerceAutoStart({ project: '/b', id: 'a', title: 'Build', tasks: 2, error: '', warning: '' })
    assert.deepEqual(autoStartToast(ok!), { message: 'Started Build (2 agent tasks)', kind: 'info' })
    assert.equal(autoStartToast({ ...ok!, tasks: 1 }).message, 'Started Build (1 agent task)')
    assert.deepEqual(autoStartToast({ ...ok!, tasks: 0, error: 'api has no checkout' }), { message: 'Couldn\'t start "Build": api has no checkout. It keeps waiting.', kind: 'error' })
    assert.equal(autoStartToast({ ...ok!, warning: 'the status could not be written' }).kind, 'error')
    assert.equal(coerceAutoStart({ project: '/b', id: 'a' })?.title, 'a')
    for (const bad of [null, 'x', {}, { project: 1, id: 'a' }]) assert.equal(coerceAutoStart(bad), null)
  })
})

describe('cloning', () => {
  const ev = (over = {}) => coerceCloneEvent({ project: '/b', repo: 'api', destination: '/p/api', progress: '', done: false, error: '', cancelled: false, ...over })!
  it('offers Clone… for a repository with no checkout only', () => {
    assert.equal(canClone(repo({ folder: '' })), true)
    assert.equal(canClone(repo({ folder: '/w/api' })), false)
  })
  it('follows the progress events and clears at the end', () => {
    const start = cloneFromInfo({ repo: 'api', destination: '/p/api', progress: 'Cloning into…' })
    assert.deepEqual(start, { repo: 'api', destination: '/p/api', progress: 'Cloning into…' })
    assert.equal(cloneFromInfo(null), null)
    const next = applyCloneEvent(start, ev({ progress: 'Receiving objects: 40%' }))
    assert.equal(next?.progress, 'Receiving objects: 40%')
    // An event without a line keeps the last one; the first event of a clone starts from nothing.
    assert.equal(applyCloneEvent(next, ev())?.progress, 'Receiving objects: 40%')
    assert.deepEqual(applyCloneEvent(null, ev({ progress: 'x' })), { repo: 'api', destination: '/p/api', progress: 'x' })
    assert.equal(applyCloneEvent(next, ev({ done: true })), null)
    assert.equal(applyCloneEvent(next, ev({ done: true, cancelled: true })), null)
  })
  it('titles a running clone', () => {
    assert.equal(cloneTitle({ repo: 'api', destination: '/p/api', progress: '' }), 'Cloning api into /p/api')
    assert.equal(cloneTitle({ repo: 'api', destination: '', progress: '' }), 'Cloning api…')
  })
  it('reads untrusted events', () => {
    for (const bad of [null, 3, {}, { project: '/b' }, { repo: 'api' }]) assert.equal(coerceCloneEvent(bad), null)
    assert.equal(coerceCloneEvent({ project: '/b', repo: 'api', done: 'yes', progress: 4 })?.done, false)
  })
  it('says whether a clone is trusted', () => {
    assert.equal(clonedText('api', { folder: '/c/api', trusted: true }), 'Cloned api into /c/api: it is trusted, because it is inside a trusted folder')
    assert.equal(clonedText('api', { folder: '/s/api', trusted: false }), 'Cloned api into /s/api: it is in Restricted Mode until you trust it')
  })
  it('does not show a cancel as an error', () => {
    assert.deepEqual(cloneFailure('api', 'the clone was cancelled'), { message: 'The clone of api was cancelled', kind: 'info' })
    assert.deepEqual(cloneFailure('api', 'fatal: repository not found'), { message: 'fatal: repository not found', kind: 'error' })
  })
})

describe('planning with an agent', () => {
  it('checks the goal like Go: 1 to 2,000 characters', () => {
    assert.match(goalProblem('') ?? '', /Say what the goal is/)
    assert.match(goalProblem('  \n ') ?? '', /Say what the goal is/)
    assert.equal(goalProblem('Add rate limiting'), null)
    assert.equal(goalProblem('x'.repeat(MAX_GOAL)), null)
    assert.match(goalProblem('x'.repeat(MAX_GOAL + 1)) ?? '', /At most 2000/)
    // Characters, not UTF-16 units.
    assert.equal(goalProblem('😀'.repeat(MAX_GOAL)), null)
  })
  it('recognises the Restricted Mode refusal', () => {
    assert.equal(isRestrictedError("the project's folder is in Restricted Mode: trust it first"), true)
    assert.equal(isRestrictedError('say what the goal is'), false)
  })
  it('starts with the default agent when it is installed', () => {
    assert.equal(pickPlanAgent(['claude', 'codex'], 'codex', 'claude'), 'codex')
    assert.equal(pickPlanAgent(['claude', 'codex'], 'gone', 'codex'), 'codex')
    assert.equal(pickPlanAgent(['claude', 'codex'], '', ''), 'claude')
    assert.equal(pickPlanAgent([], 'claude', 'claude'), '')
  })
})

describe('project notifications', () => {
  it('tell how an automatic start went', () => {
    const a = { project: '/p', id: 'w', title: 'Waiter', tasks: 2, error: '', warning: '' }
    assert.deepEqual(autoStartNotice(a), { title: 'Started "Waiter"', body: '2 agent tasks started' })
    const declined = "the start was declined in Flint's confirmation (the project's folder is in Restricted Mode): Delegate it by hand, or cancel the start"
    assert.deepEqual(autoStartNotice({ ...a, tasks: 0, error: declined }), { title: 'Couldn\'t start "Waiter"', body: `${declined}. It keeps waiting.` })
  })
  it('say when an item is ready for review', () => {
    const e = coerceItemEvent({ project: '/p', id: 'x', title: 'Add login' })
    assert.ok(e)
    assert.equal(reviewText(e), '"Add login" is ready for review')
    assert.equal(coerceItemEvent({ project: 1 }), null)
    assert.equal(coerceItemEvent({ project: '/p', id: 'x' })?.title, 'x')
  })
  it('tell how a clone ended, but not a cancelled one', () => {
    const ev = { project: '/p', repo: 'api', destination: '/w/api', progress: '', done: true, error: '', cancelled: false }
    assert.deepEqual(cloneNotice(ev), { title: 'Cloned api', body: 'Into /w/api' })
    assert.deepEqual(cloneNotice({ ...ev, error: 'exit status 128' }), { title: "Couldn't clone api", body: 'exit status 128' })
    assert.equal(cloneNotice({ ...ev, cancelled: true }), null)
    assert.equal(cloneNotice({ ...ev, done: false }), null)
  })
})
