import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { assertSubset } from '../testing/assertSubset'
import {
  addUserMessage,
  answerPermission,
  applyEvent,
  applyEvents,
  editedFiles,
  emptyChat,
  settleItems,
  type AgentEvent,
  type ChatState,
} from './agentReducer'

const ev = (e: Partial<AgentEvent>): AgentEvent => ({ sessionId: 's', kind: '', ...e })
const run = (events: Partial<AgentEvent>[], start: ChatState = emptyChat) =>
  events.reduce((s, e) => applyEvent(s, ev(e)), start)

describe('agent reducer', () => {
  it('merges streamed chunks into one assistant message', () => {
    const s = run([
      { kind: 'message_chunk', text: 'Hel' },
      { kind: 'message_chunk', text: 'lo' },
      { kind: 'thought_chunk', text: 'hmm' },
      { kind: 'message_chunk', text: '!' },
    ])
    assert.deepEqual(s.items, [
      { type: 'assistant', text: 'Hello' },
      { type: 'thought', text: 'hmm' },
      { type: 'assistant', text: '!' },
    ])
  })

  it('merges tool call updates by id without dropping earlier fields', () => {
    const s = run([
      { kind: 'tool_call', toolCall: { id: 't1', title: 'Run: go test', kind: 'execute', status: 'pending', input: 'go test' } },
      { kind: 'tool_call_update', toolCall: { id: 't1', status: 'completed', output: 'ok', title: '' } },
    ])
    assert.equal((s.items).length, 1)
    assert.deepEqual(s.items[0], {
      type: 'tool',
      call: { id: 't1', title: 'Run: go test', kind: 'execute', status: 'completed', input: 'go test', output: 'ok' },
    })
  })

  it('tracks busy state, plan, usage and the turn end', () => {
    let s = addUserMessage(emptyChat, 'fix it', [{ path: '/p/a.go' }])
    assert.equal(s.busy, true)
    s = run(
      [
        { kind: 'plan', plan: [{ content: 'read', status: 'completed' }] },
        { kind: 'usage', usage: { inputTokens: 10, outputTokens: 5, costUsd: 0.01 } },
        { kind: 'turn_end', stopReason: 'end_turn' },
      ],
      s,
    )
    assert.equal(s.busy, false)
    assert.equal((s.plan).length, 1)
    assert.deepEqual(s.items.at(-1), { type: 'end', stopReason: 'end_turn', usage: { inputTokens: 10, outputTokens: 5, costUsd: 0.01 } })
  })

  it('accumulates usage across turns and keeps the latest context reading', () => {
    const s = run([
      { kind: 'usage', usage: { inputTokens: 100, outputTokens: 10, costUsd: 0.01, contextTokens: 90, contextWindow: 1000 } },
      { kind: 'turn_end', stopReason: 'end_turn' },
      { kind: 'usage', usage: { inputTokens: 250, outputTokens: 20, costUsd: 0.02, contextTokens: 240, contextWindow: 1000 } },
      { kind: 'turn_end', stopReason: 'end_turn' },
      // a turn without a usage report must not blank the totals or repeat the last turn's usage
      { kind: 'turn_end', stopReason: 'end_turn' },
    ])
    assert.deepEqual(s.totals, { inputTokens: 350, outputTokens: 30, costUsd: 0.03, contextTokens: 240, contextWindow: 1000 })
    assert.deepEqual(s.items.at(-1), { type: 'end', stopReason: 'end_turn', usage: undefined })
  })

  it('treats missing usage fields as zero and leaves the previous state untouched', () => {
    const before = run([{ kind: 'usage', usage: { inputTokens: 5, outputTokens: 1, costUsd: 0 } }])
    const after = applyEvent(before, ev({ kind: 'usage', usage: { inputTokens: 5, outputTokens: 1, costUsd: 0 } }))
    assert.equal(before.totals?.inputTokens, 5)
    assert.deepEqual(after.totals, { inputTokens: 10, outputTokens: 2, costUsd: 0, contextTokens: 0, contextWindow: 0 })
  })

  it('handles permission requests: answer, settle, and cancel', () => {
    const permission = { id: 'p1', toolCall: { id: 't', kind: 'execute' }, options: [{ id: 'y', name: 'Allow', kind: 'allow_once' }] }
    let s = run([{ kind: 'permission_request', permission }])
    s = answerPermission(s, 'p1', 'Allow')
    assertSubset(s.items[0], { type: 'permission', answer: 'Allow', settled: true })

    let c = run([{ kind: 'permission_request', permission: { ...permission, id: 'p2' } }, { kind: 'turn_end', stopReason: 'cancelled' }])
    assertSubset(c.items[0], { settled: true, answer: null })
    c = run([{ kind: 'permission_settled', requestId: 'zzz' }], c)
    assert.equal((c.items).length, 2)
  })

  it('lists edited files from diffs and edit calls, skipping failures', () => {
    const s = run([
      { kind: 'tool_call', toolCall: { id: '1', kind: 'edit', paths: ['/p/a.go'], status: 'completed' } },
      { kind: 'tool_call', toolCall: { id: '2', kind: 'edit', diffs: [{ path: '/p/b.go', oldText: '', newText: 'x' }] } },
      { kind: 'tool_call', toolCall: { id: '3', kind: 'edit', paths: ['/p/c.go'], status: 'failed' } },
      { kind: 'tool_call', toolCall: { id: '4', kind: 'read', paths: ['/p/d.go'] } },
    ])
    assert.deepEqual(editedFiles(s), ['/p/a.go', '/p/b.go'])
  })

  it('ignores unknown events without allocating', () => {
    const s = run([{ kind: 'message_chunk', text: 'x' }])
    assert.equal(applyEvent(s, ev({ kind: 'something_new' })), s)
  })

  it('appends chunks without touching earlier items', () => {
    const s = run([{ kind: 'tool_call', toolCall: { id: 't1' } }, { kind: 'message_chunk', text: 'a' }])
    const next = applyEvent(s, ev({ kind: 'message_chunk', text: 'b' }))
    assert.equal(next.items[0], s.items[0])
    assert.deepEqual(next.items[1], { type: 'assistant', text: 'ab' })
    assert.deepEqual(s.items[1], { type: 'assistant', text: 'a' }) // input untouched
  })

  it('updates the latest tool call with a repeated id', () => {
    const s = run([
      { kind: 'tool_call', toolCall: { id: 't1', title: 'first' } },
      { kind: 'tool_call', toolCall: { id: 't2', title: 'second' } },
      { kind: 'tool_call_update', toolCall: { id: 't1', status: 'completed' } },
    ])
    assert.deepEqual(s.items.map((it) => (it.type === 'tool' ? it.call.status : null)), ['completed', undefined])
  })

  it('applies a batch like the same events one by one', () => {
    const events: Partial<AgentEvent>[] = [
      { kind: 'message_chunk', text: 'He' },
      { kind: 'message_chunk', text: 'llo' },
      { kind: 'thought_chunk', text: 'hm' },
      { kind: 'thought_chunk', text: 'm' },
      { kind: 'tool_call', toolCall: { id: 't1', status: 'pending' } },
      { kind: 'message_chunk', text: 'done' },
      { kind: 'message_chunk' },
      { kind: 'usage', usage: { inputTokens: 1, outputTokens: 2, costUsd: 0 } },
      { kind: 'turn_end', stopReason: 'end_turn' },
    ]
    const start = addUserMessage(emptyChat, 'hi', [])
    assert.deepEqual(applyEvents(start, events.map(ev)), run(events, start))
    assert.equal(applyEvents(start, []), start)
  })

  it('stops tool calls left running by a cancelled or failed turn, not by a finished one', () => {
    const running: Partial<AgentEvent>[] = [
      { kind: 'tool_call', toolCall: { id: '1', status: 'pending' } },
      { kind: 'tool_call', toolCall: { id: '2', status: 'in_progress' } },
      { kind: 'tool_call', toolCall: { id: '3' } },
      { kind: 'tool_call', toolCall: { id: '4', status: 'completed' } },
      { kind: 'tool_call', toolCall: { id: '5', status: 'failed' } },
    ]
    const statuses = (s: ChatState) => s.items.flatMap((it) => (it.type === 'tool' ? [it.call.status] : []))
    const cancelled = run([...running, { kind: 'turn_end', stopReason: 'cancelled' }])
    assert.deepEqual(statuses(cancelled), ['cancelled', 'cancelled', 'cancelled', 'completed', 'failed'])
    const finished = run([...running, { kind: 'turn_end', stopReason: 'end_turn' }])
    assert.deepEqual(statuses(finished), ['pending', 'in_progress', undefined, 'completed', 'failed'])
  })

  it('settles nothing (and allocates nothing) when nothing is open', () => {
    const items = run([{ kind: 'tool_call', toolCall: { id: '1', status: 'completed' } }, { kind: 'message_chunk', text: 'x' }]).items
    assert.equal(settleItems(items), items)
  })
})
