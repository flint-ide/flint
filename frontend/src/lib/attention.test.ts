import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  approvalKey,
  badgeCount,
  cameBack,
  chatSignals,
  emptyAttention,
  inAppNotice,
  needClosed,
  needOpened,
  needsOfTabClosed,
  notificationStatusLabel,
  terminalSignals,
  turnAfter,
  wentToBackground,
  workFinished,
  type Attention,
  type ChatSignal,
  type Need,
} from './attention'
import type { AgentEvent } from './agentReducer'

const on = { notifyInBackground: true, notifyOnApproval: true }
const need = (key: string, over: Partial<Need> = {}): Omit<Need, 'announced'> => ({
  key, kind: 'approval', title: 'claude is waiting for your approval', body: 'app: Run npm test', tab: `chat:${key}`, inApp: 'x', ...over,
})
const done = { title: 'Fix the bug', body: 'The agent finished its turn', tab: 'chat:s1' }

describe('needs', () => {
  it('announces a request that arrives in the background, once', () => {
    const first = needOpened(emptyAttention, need('a'), true, on)
    assert.equal(first.notify.length, 1)
    assert.equal(first.notify[0].title, 'claude is waiting for your approval')
    const again = needOpened(first.state, need('a'), true, on)
    assert.equal(again.notify.length, 0)
    assert.equal(again.state, first.state)
  })

  it('remembers a request that arrives in front and announces it when Flint goes to the background', () => {
    const front = needOpened(emptyAttention, need('a'), false, on)
    assert.deepEqual(front.notify, [])
    const away = wentToBackground(front.state, on)
    assert.equal(away.notify.length, 1)
    // Once: a second blur says nothing.
    assert.deepEqual(wentToBackground(away.state, on).notify, [])
  })

  it('does not announce what was answered before the user left', () => {
    const front = needOpened(emptyAttention, need('a'), false, on).state
    assert.deepEqual(wentToBackground(needClosed(front, 'a'), on).notify, [])
  })

  it('leaves Flint\'s own questions to Go but counts them', () => {
    const q = needOpened(emptyAttention, need('q1', { kind: 'question', tab: '', inApp: '' }), true, on)
    assert.deepEqual(q.notify, [])
    assert.deepEqual(wentToBackground(q.state, on).notify, [])
    assert.equal(badgeCount(q.state, on), 1)
  })

  it('follows the settings', () => {
    for (const prefs of [{ notifyInBackground: false, notifyOnApproval: true }, { notifyInBackground: true, notifyOnApproval: false }]) {
      const s = needOpened(emptyAttention, need('a'), true, prefs)
      assert.deepEqual(s.notify, [], JSON.stringify(prefs))
      assert.deepEqual(wentToBackground(s.state, prefs).notify, [])
      assert.equal(badgeCount(s.state, prefs), 0)
    }
  })

  it('closes every need of a tab whose turn ended', () => {
    let a = needOpened(emptyAttention, need('r1', { tab: 'chat:s1' }), true, on).state
    a = needOpened(a, need('r2', { tab: 'chat:s1' }), true, on).state
    a = needOpened(a, need('r3', { tab: 'chat:s2' }), true, on).state
    assert.deepEqual(needsOfTabClosed(a, 'chat:s1').needs.map((n) => n.key), ['r3'])
  })
})

describe('the badge', () => {
  it('counts what waits plus finished work not seen; coming back clears only the finished part', () => {
    let a: Attention = needOpened(emptyAttention, need('a'), true, on).state
    a = needOpened(a, need('b'), false, on).state // arrived in front: still waiting, still counted
    a = workFinished(a, done, true, on).state
    a = workFinished(a, done, true, on).state
    assert.equal(badgeCount(a, on), 4)
    a = cameBack(a)
    assert.equal(badgeCount(a, on), 2)
    assert.equal(badgeCount(needClosed(a, 'a'), on), 1)
  })

  it('does not count or announce work that finished while the user was in front', () => {
    const s = workFinished(emptyAttention, done, false, on)
    assert.deepEqual(s.notify, [])
    assert.equal(badgeCount(s.state, on), 0)
    const off = workFinished(emptyAttention, done, true, { notifyInBackground: false, notifyOnApproval: true })
    assert.deepEqual(off.notify, [])
  })
})

describe('the in-app notice', () => {
  const a = needOpened(needOpened(emptyAttention, need('a', { tab: 'chat:a' }), false, on).state, need('b', { tab: 'chat:b' }), false, on).state
  it('shows the newest request that is not in the active tab', () => {
    assert.equal(inAppNotice(a, null, new Set())?.key, 'b')
    assert.equal(inAppNotice(a, 'chat:b', new Set())?.key, 'a')
    assert.equal(inAppNotice(a, 'chat:b', new Set(['a'])), null)
  })
  it('has none for a question', () => {
    const q = needOpened(emptyAttention, need('q', { kind: 'question', inApp: '' }), false, on).state
    assert.equal(inAppNotice(q, null, new Set()), null)
  })
})

describe('chat events', () => {
  const chat = { agent: 'claude', cwd: '/src/app', title: 'Fix the bug' }
  const request: AgentEvent = { sessionId: 's1', kind: 'permission_request', permission: { id: 'r1', toolCall: { id: 't', title: 'Run npm test' }, options: [] } }
  const play = (events: AgentEvent[], info = chat): ChatSignal[] => {
    let inTurn = false
    const out: ChatSignal[] = []
    for (const e of events) {
      out.push(...chatSignals('s1', e, inTurn, info))
      inTurn = turnAfter(inTurn, e)
    }
    return out
  }

  it('asks for a turn whoever started it (a prompt Go sent opens it with turn_start)', () => {
    const got = play([{ sessionId: 's1', kind: 'user_message', text: 'task', origin: { sessionId: '', agent: 'Flint', role: 'task' } }, { sessionId: 's1', kind: 'turn_start' }, request])
    assert.equal(got.length, 1)
    assert.equal(got[0].kind, 'need')
    if (got[0].kind === 'need') {
      assert.equal(got[0].need.key, approvalKey('s1', 'r1'))
      assert.equal(got[0].need.tab, 'chat:s1')
      assert.equal(got[0].need.inApp, 'claude is waiting for your approval in app')
    }
  })

  it('ignores the history a resumed chat replays', () => {
    const replay = play([{ sessionId: 's1', kind: 'user_message', text: 'old' }, request, { sessionId: 's1', kind: 'turn_end', stopReason: 'end_turn' }])
    assert.deepEqual(replay.filter((s) => s.kind === 'need' || s.kind === 'finished'), [])
  })

  it('settles the request when it is answered and the turn when it ends', () => {
    const got = play([{ sessionId: 's1', kind: 'turn_start' }, request, { sessionId: 's1', kind: 'permission_settled', requestId: 'r1' }, { sessionId: 's1', kind: 'turn_end', stopReason: 'end_turn' }])
    assert.deepEqual(got.map((s) => s.kind), ['need', 'closed', 'ended', 'finished'])
    const fin = got[3]
    assert.ok(fin.kind === 'finished' && fin.notice.body === 'The agent finished its turn' && fin.notice.title === 'Fix the bug')
  })

  it('says when a turn failed, and nothing for a cancelled one', () => {
    const failed = play([{ sessionId: 's1', kind: 'turn_start' }, { sessionId: 's1', kind: 'turn_end', stopReason: 'error' }])
    assert.ok(failed[1].kind === 'finished' && failed[1].notice.body === 'The agent stopped with an error')
    const cancelled = play([{ sessionId: 's1', kind: 'turn_start' }, { sessionId: 's1', kind: 'turn_end', stopReason: 'cancelled' }])
    assert.deepEqual(cancelled.map((s) => s.kind), ['ended'])
  })

  it('lets a child agent ask, but leaves its turn ends to its parent', () => {
    const child = { ...chat, parent: 'p1' }
    const got = play([{ sessionId: 's1', kind: 'turn_start' }, request, { sessionId: 's1', kind: 'turn_end', stopReason: 'end_turn' }], child)
    assert.deepEqual(got.map((s) => s.kind), ['need', 'ended'])
  })
})

describe('terminal agents', () => {
  const t = { tab: 'term:t1', title: 'zsh', cwd: '/src/app', command: 'claude --resume' }
  it('waits while the agent waits and finishes once', () => {
    const waiting = terminalSignals(t, 'working', 'waiting')
    assert.equal(waiting.length, 1)
    assert.ok(waiting[0].kind === 'need' && waiting[0].need.title === 'claude is waiting for you' && waiting[0].need.tab === 'term:t1')
    assert.deepEqual(terminalSignals(t, 'waiting', 'working').map((s) => s.kind), ['closed'])
    const fin = terminalSignals(t, 'waiting', 'done')
    assert.deepEqual(fin.map((s) => s.kind), ['closed', 'finished'])
    assert.deepEqual(terminalSignals(t, 'done', 'done'), [])
    assert.deepEqual(terminalSignals(t, undefined, 'done'), [])
  })
})

describe('notificationStatusLabel', () => {
  it('names every state Go reports', () => {
    assert.equal(notificationStatusLabel('allowed'), 'Allowed')
    assert.equal(notificationStatusLabel('denied'), 'Not allowed by the system')
    assert.equal(notificationStatusLabel('failed'), 'Allowed, but the last one was not delivered')
    assert.equal(notificationStatusLabel('unavailable'), 'Not available')
    assert.equal(notificationStatusLabel('?'), 'Unknown')
  })
})
