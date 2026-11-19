import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  agentRows,
  attentionCount,
  chatState,
  chatTitle,
  firstNeedingAttention,
  nextUnseen,
  stateHint,
  stateLabel,
  terminalAgentKey,
  terminalBadge,
  type AgentChatInput,
  type TerminalAgentInput,
} from './agentList'
import type { ChatItem, ChatState } from './agentReducer'
import type { TaskView } from './agentTasks'

const chat = (items: ChatItem[], busy = false): ChatState => ({ items, plan: [], busy })
const user = (text: string): ChatItem => ({ type: 'user', text, attachments: [] })
const end = (stopReason: string): ChatItem => ({ type: 'end', stopReason })
const ask = (settled: boolean): ChatItem => ({
  type: 'permission',
  request: { id: 'r1', toolCall: { id: 't1', title: 'Run ls' }, options: [] },
  answer: null,
  settled,
})

describe('chatState', () => {
  const cases: [string, ChatState, boolean, string][] = [
    ['nothing said yet', chat([]), false, 'idle'],
    ['a turn running', chat([user('hi')], true), false, 'working'],
    ['an unanswered request', chat([user('hi'), ask(false)], true), false, 'waiting'],
    ['an answered request', chat([user('hi'), ask(true)], true), false, 'working'],
    ['a finished turn', chat([user('hi'), end('end_turn')]), false, 'finished'],
    ['a cancelled turn', chat([user('hi'), end('cancelled')]), false, 'idle'],
    ['a failed turn', chat([user('hi'), end('error')]), false, 'failed'],
    ['an unknown stop reason', chat([user('hi'), end('max_tokens')]), false, 'failed'],
    ['a new turn after a finished one', chat([user('a'), end('end_turn'), user('b')], true), false, 'working'],
    ['a restored chat', chat([user('hi'), end('end_turn')]), true, 'idle'],
  ]
  for (const [name, c, dormant, want] of cases) {
    it(name, () => assert.equal(chatState(c, dormant), want))
  }
})

describe('chatTitle', () => {
  it('uses the first message, one line, shortened', () => {
    assert.equal(chatTitle(chat([user('  Fix the\nbug  ')])), 'Fix the bug')
    assert.equal(chatTitle(chat([user('x'.repeat(100))])), `${'x'.repeat(59)}…`)
  })
  it('names a chat without messages', () => assert.equal(chatTitle(chat([])), 'New chat'))
})

describe('nextUnseen', () => {
  const cases: [string, string[], string[], string | null, string[], string[]][] = [
    ['a turn ended in a background tab', [], ['a'], 'b', ['a', 'b'], ['a']],
    ['a turn ended in the open tab', [], ['a'], 'a', ['a', 'b'], []],
    ['viewing a tab clears it', ['a'], [], 'a', ['a'], []],
    ['a closed chat is forgotten', ['a'], [], null, [], []],
    ['no duplicates', ['a'], ['a'], null, ['a'], ['a']],
  ]
  for (const [name, unseen, ended, active, live, want] of cases) {
    it(name, () => assert.deepEqual(nextUnseen(unseen, ended, active, live), want))
  }
})

const session = (id: string, over: Partial<AgentChatInput> = {}): AgentChatInput => ({
  id, agent: 'claude', cwd: '/src/p', chat: chat([]), dormant: false, ...over,
})
const task = (over: Partial<TaskView> = {}): TaskView => ({
  id: 'k1', title: 'Add a button', prompt: 'Add a button', agent: 'codex', branch: 'agent/b', base: 'main',
  worktree: '/src/p.wt/b', checkout: '/src/p', createdAt: 1, pullRequest: '', state: 'idle', error: '', stale: false,
  ahead: 0, merged: false, group: '', joint: '', lead: false, flintStarts: false, session: '', engine: '', tokens: 0, costUsd: 0, ...over,
})

describe('agentRows', () => {
  it('orders what needs the user first, then by session order', () => {
    const rows = agentRows({
      sessions: [
        session('idle'),
        session('work', { chat: chat([user('go')], true) }),
        session('done', { chat: chat([user('go'), end('end_turn')]) }),
        session('seen', { chat: chat([user('go'), end('end_turn')]) }),
        session('fail', { chat: chat([user('go'), end('error')]) }),
        session('ask', { chat: chat([user('go'), ask(false)], true) }),
      ],
      tasks: [task({ id: 'q', state: 'queued', worktree: '/nowhere' })],
      unseen: ['done'],
    })
    assert.deepEqual(rows.map((r) => `${r.key}:${r.state}`), [
      'chat:ask:waiting', 'chat:fail:failed', 'chat:done:finished-unseen', 'chat:work:working', 'task:q:queued',
      'chat:idle:idle', 'chat:seen:idle',
    ])
  })

  it('takes the state of a chat in a task worktree from the task', () => {
    const rows = agentRows({
      sessions: [session('c', { cwd: '/src/p.wt/b', chat: chat([user('go')], true) })],
      tasks: [task({ state: 'waiting' })],
      unseen: [],
    })
    assert.equal(rows.length, 1)
    assert.deepEqual([rows[0].kind, rows[0].state, rows[0].title, rows[0].taskId], ['task', 'waiting', 'Add a button', 'k1'])
  })

  it('lists a task without a chat only while it is queued', () => {
    const rows = agentRows({
      sessions: [],
      tasks: [task({ state: 'queued' }), task({ id: 'k2', state: 'finished' }), task({ id: 'k3', state: 'queued', stale: true })],
      unseen: [],
    })
    assert.deepEqual(rows.map((r) => r.key), ['task:k1'])
  })

  it('shows a task chat that finished unseen', () => {
    const rows = agentRows({
      sessions: [session('c', { cwd: '/src/p.wt/b', chat: chat([user('go'), end('end_turn')]) })],
      tasks: [task({ state: 'finished' })],
      unseen: ['c'],
    })
    assert.equal(rows[0].state, 'finished-unseen')
  })

  it('names the project folder and the agent', () => {
    const [r] = agentRows({ sessions: [session('c', { agent: 'codex', cwd: '/src/my-app/' })], tasks: [], unseen: [] })
    assert.deepEqual([r.agent, r.root, r.project], ['codex', '/src/my-app/', 'my-app'])
  })
})

describe('attentionCount and labels', () => {
  it('counts waiting, failed and finished-unseen', () => {
    const rows = agentRows({
      sessions: [
        session('a', { chat: chat([user('x'), ask(false)], true) }),
        session('b', { chat: chat([user('x')], true) }),
        session('c', { chat: chat([user('x'), end('end_turn')]) }),
      ],
      tasks: [],
      unseen: ['c'],
    })
    assert.equal(attentionCount(rows), 2)
  })
  it('labels every state', () => {
    assert.equal(stateLabel('waiting'), 'waiting for you')
    assert.equal(stateLabel('finished-unseen'), 'finished')
    assert.equal(stateLabel('queued'), 'queued')
  })
})

describe('firstNeedingAttention', () => {
  const rows = agentRows({
    sessions: [
      session('a', { chat: chat([user('x')], true) }),
      session('b', { chat: chat([user('x'), end('end_turn')]) }),
      session('c', { chat: chat([user('x'), ask(false)], true) }),
    ],
    tasks: [],
    unseen: ['b'],
  })
  it('is the first row in the panel order that needs the user', () => assert.equal(firstNeedingAttention(rows)?.sessionId, 'c'))
  it('is undefined when nobody needs the user', () =>
    assert.equal(firstNeedingAttention(agentRows({ sessions: [session('a', { chat: chat([user('x')], true) })], tasks: [], unseen: [] })), undefined))
  it('skips a row that has no chat to show', () => {
    const failedTask = agentRows({ sessions: [], tasks: [task({ state: 'queued' })], unseen: [] })
    assert.equal(firstNeedingAttention(failedTask), undefined)
  })
})

const term = (id: string, over: Partial<TerminalAgentInput> = {}): TerminalAgentInput => ({
  id, title: 'claude', cwd: '/src/p', command: 'claude --resume', state: 'working', ...over,
})

describe('terminal agents', () => {
  it('lists a terminal that reports a state, with the panel rules', () => {
    const rows = agentRows({
      sessions: [session('idle'), session('work', { chat: chat([user('go')], true) })],
      tasks: [],
      unseen: [terminalAgentKey('d')],
      terminals: [term('w', { state: 'waiting' }), term('d', { state: 'done' }), term('d2', { state: 'done' }), term('x', { state: 'working' })],
    })
    assert.deepEqual(rows.map((r) => `${r.key}:${r.state}`), [
      'term:w:waiting', 'term:d:finished-unseen', 'chat:work:working', 'term:x:working', 'chat:idle:idle', 'term:d2:idle',
    ])
    const w = rows[0]
    assert.deepEqual([w.kind, w.terminalId, w.agent, w.title, w.root, w.project, w.sessionId], ['terminal', 'w', 'claude', 'claude', '/src/p', 'p', undefined])
  })

  it('ignores terminals with no agent: no state, idle, exited', () => {
    const rows = agentRows({
      sessions: [],
      tasks: [],
      unseen: [],
      terminals: [term('none', { state: undefined }), term('left', { state: 'idle' }), term('dead', { state: 'working', exited: true })],
    })
    assert.deepEqual(rows, [])
  })

  it('names an agent without a command after the terminal', () => {
    const [r] = agentRows({ sessions: [], tasks: [], unseen: [], terminals: [term('a', { command: '' })] })
    assert.equal(r.agent, 'terminal')
  })

  it('counts a waiting terminal as needing the user, and can be the first to look at', () => {
    const rows = agentRows({ sessions: [session('a', { chat: chat([user('x')], true) })], tasks: [], unseen: [], terminals: [term('w', { state: 'waiting' })] })
    assert.equal(attentionCount(rows), 1)
    assert.equal(firstNeedingAttention(rows)?.terminalId, 'w')
  })

  it('badges a tab for working, waiting and done only', () => {
    assert.deepEqual(['working', 'waiting', 'done'].map((s) => terminalBadge(s as TerminalAgentInput['state'])?.look), ['working', 'waiting', 'finished'])
    assert.equal(terminalBadge('idle'), null)
    assert.equal(terminalBadge(undefined), null)
  })

  it('explains waiting differently for a terminal', () => {
    assert.match(stateHint('waiting', 'terminal'), /terminal/)
    assert.equal(stateHint('waiting', 'chat'), stateHint('waiting'))
  })
})

describe('hidden rows', () => {
  it('marks settled chats and keeps them listed', () => {
    const rows = agentRows({ sessions: [session('a'), session('b')], tasks: [], unseen: [], settled: ['b'] })
    assert.deepEqual(rows.map((r) => [r.sessionId, r.hidden]), [['a', false], ['b', true]])
  })
})
