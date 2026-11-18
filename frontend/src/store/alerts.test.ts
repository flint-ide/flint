import { beforeEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  alertDone,
  alertReceived,
  chatSignalled,
  frontMoved,
  questionChanged,
  setAlertsOutput,
  tabClosed,
  terminalAgentChanged,
  useAlerts,
} from './alerts'
import { useSettings } from './settings'
import { emptyAttention, type Notice } from '../lib/attention'
import { createChatAnnouncer } from '../lib/chatAnnouncer'
import { createEventHold } from '../lib/eventHold'
import { applyEvent, emptyChat, type AgentEvent, type ChatState } from '../lib/agentReducer'

// The wiring of store/alerts.ts without a DOM: Go's front, question and alert
// events in, notifications, the badge and toasts out (a fake output).
let sent: Notice[] = []
let badges: number[] = []
let toasts: string[] = []
let now = 0
let seq = 0

beforeEach(() => {
  sent = []
  badges = []
  toasts = []
  now = 0
  seq = 0
  useAlerts.setState({ attention: emptyAttention, dismissed: new Set() })
  useSettings.setState((s) => ({ settings: { ...s.settings, notifyInBackground: true, notifyOnApproval: true } }))
  setAlertsOutput({ notify: (n) => sent.push(n), badge: (n) => badges.push(n), toast: (m) => toasts.push(m), now: () => now })
})

const away = () => frontMoved({ front: false, seq: ++seq })
const back = () => frontMoved({ front: true, seq: ++seq })
const approval = (id: string): AgentEvent => ({
  sessionId: 's1', kind: 'permission_request', permission: { id, toolCall: { id: 't', title: 'Run npm test' }, options: [] },
})
const chat = { agent: 'claude', cwd: '/src/app', title: 'Fix the bug' }

describe('alerts wiring', () => {
  it('follows Go on whether Flint is in front: work finishing in front is not announced, behind it is', () => {
    assert.equal(alertDone('Tests', 'passed', ''), false)
    away()
    assert.equal(alertDone('Tests', 'failed', ''), true)
    assert.deepEqual(badges, [1])
    back()
    assert.deepEqual(badges, [1, 0])
  })

  it('a question of Flint\'s is counted on the badge; with it open Flint stays in front, so a chat request is not announced', () => {
    questionChanged({ id: 'q1', title: 'Allow this agent action?', open: true })
    const announcer = createChatAnnouncer()
    for (const e of [{ sessionId: 's1', kind: 'turn_start' }, approval('r1')]) chatSignalled(announcer.event('s1', e, () => chat))
    assert.equal(sent.length, 0)
    assert.deepEqual(badges, [1, 2])
    questionChanged({ id: 'q1', title: 'Allow this agent action?', open: false })
    assert.deepEqual(badges, [1, 2, 1])
    // Leaving Flint announces the request, once; the closed question says nothing.
    away()
    away()
    assert.deepEqual(sent.map((n) => n.title), ['claude is waiting for your approval'])
  })

  it('ignores a front event older than the last one', () => {
    frontMoved({ front: false, seq: 5 })
    frontMoved({ front: true, seq: 4 })
    assert.equal(useAlerts.getState().attention.front, false)
  })

  it('shows an alert of Go\'s as a toast in front; behind, a notification and the toast when the user is back', () => {
    alertReceived({ title: 'An agent task did not start', body: 'x: boom', tab: '', kind: 'error' })
    assert.deepEqual(toasts, ['An agent task did not start: x: boom'])
    away()
    alertReceived({ title: 'An agent task did not start', body: 'y: boom', tab: '', kind: 'error' })
    assert.deepEqual(sent.map((n) => n.body), ['y: boom'])
    assert.equal(toasts.length, 1)
    back()
    assert.deepEqual(toasts, ['An agent task did not start: x: boom', 'An agent task did not start: y: boom'])
  })

  it('a terminal that is not running an agent CLI never notifies; one that is, at most once a minute', () => {
    away()
    for (let i = 0; i < 20; i++) {
      now += 1000
      terminalAgentChanged({ tab: 'term:1', cwd: '/r', command: 'npm test' }, i % 2 ? 'waiting' : 'done', i % 2 ? 'done' : 'waiting')
      terminalAgentChanged({ tab: 'term:2', cwd: '/r', command: 'claude' }, i % 2 ? 'waiting' : 'done', i % 2 ? 'done' : 'waiting')
    }
    assert.deepEqual(sent.map((n) => n.title), ['claude is waiting for you', 'claude finished its turn'])
    tabClosed('term:2')
    assert.deepEqual(useAlerts.getState().attention.terminals, {})
  })

  // A chat Go starts for a task: chat:opened is held while the task's folder
  // opens (store/agentChat.ts adopting), its first events wait in the hold,
  // then go through the announcer, the model and the reducer in order.
  it('takes a Go-started turn end to end, through the adoption hold, the reducer and the model', () => {
    away()
    const hold = createEventHold<AgentEvent>(100)
    const announcer = createChatAnnouncer()
    let adopted = false
    let state: ChatState = emptyChat
    const deliver = (e: AgentEvent) => {
      if (hold.offer('s1', e)) return
      chatSignalled(announcer.event('s1', e, () => (adopted ? chat : null)))
      state = applyEvent(state, e)
    }
    hold.begin('s1')
    deliver({ sessionId: 's1', kind: 'turn_start' })
    deliver({ sessionId: 's1', kind: 'message_chunk', text: 'Looking' })
    deliver(approval('r1'))
    assert.equal(sent.length, 0, 'nothing before the chat is adopted')
    adopted = true
    for (const e of hold.end('s1')) deliver(e)
    assert.equal(state.busy, true)
    assert.deepEqual(sent.map((n) => [n.title, n.tab]), [['claude is waiting for your approval', 'chat:s1']])
    deliver({ sessionId: 's1', kind: 'permission_settled', requestId: 'r1' })
    // The agent fails before it got further (Go's Manager ends the turn after the error).
    deliver({ sessionId: 's1', kind: 'error', text: 'claude: the process exited' })
    deliver({ sessionId: 's1', kind: 'turn_end', stopReason: 'error' })
    assert.equal(state.busy, false)
    assert.deepEqual(sent.map((n) => n.body), ['app: Run npm test', 'The agent stopped with an error'])
    assert.deepEqual(announcer.inTurn(), [])
    assert.equal(useAlerts.getState().attention.needs.length, 0)
    assert.equal(useAlerts.getState().attention.finished, 1)
  })
})

describe('chat announcer', () => {
  it('keeps nothing for a chat that is not here, and forgets a closed one', () => {
    const a = createChatAnnouncer()
    assert.deepEqual(a.event('x', { sessionId: 'x', kind: 'turn_start' }, () => null), [])
    assert.deepEqual(a.inTurn(), [])
    a.event('s1', { sessionId: 's1', kind: 'turn_start' }, () => chat)
    assert.deepEqual(a.inTurn(), ['s1'])
    a.event('s1', { sessionId: 's1', kind: 'turn_start' }, () => null) // closed by Go, still sending
    assert.deepEqual(a.inTurn(), [])
    a.event('s1', { sessionId: 's1', kind: 'turn_start' }, () => chat)
    a.forget('s1')
    assert.deepEqual(a.inTurn(), [])
  })

  it('does no work for streamed tokens', () => {
    const a = createChatAnnouncer()
    let asked = 0
    const info = () => {
      asked++
      return chat
    }
    a.event('s1', { sessionId: 's1', kind: 'turn_start' }, info)
    for (let i = 0; i < 50; i++) a.event('s1', { sessionId: 's1', kind: 'message_chunk', text: 'x' }, info)
    a.event('s1', { sessionId: 's1', kind: 'tool_call_update' }, info)
    assert.equal(asked, 1)
  })
})
