import { describe, expect, it } from 'vitest'
import { addUserMessage, answerPermission, applyEvent, editedFiles, emptyChat, type AgentEvent, type ChatState } from './agentReducer'

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
    expect(s.items).toEqual([
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
    expect(s.items).toHaveLength(1)
    expect(s.items[0]).toEqual({
      type: 'tool',
      call: { id: 't1', title: 'Run: go test', kind: 'execute', status: 'completed', input: 'go test', output: 'ok' },
    })
  })

  it('tracks busy state, plan, usage and the turn end', () => {
    let s = addUserMessage(emptyChat, 'fix it', [{ path: '/p/a.go' }])
    expect(s.busy).toBe(true)
    s = run(
      [
        { kind: 'plan', plan: [{ content: 'read', status: 'completed' }] },
        { kind: 'usage', usage: { inputTokens: 10, outputTokens: 5, costUsd: 0.01 } },
        { kind: 'turn_end', stopReason: 'end_turn' },
      ],
      s,
    )
    expect(s.busy).toBe(false)
    expect(s.plan).toHaveLength(1)
    expect(s.items.at(-1)).toEqual({ type: 'end', stopReason: 'end_turn', usage: { inputTokens: 10, outputTokens: 5, costUsd: 0.01 } })
  })

  it('handles permission requests: answer, settle, and cancel', () => {
    const permission = { id: 'p1', toolCall: { id: 't', kind: 'execute' }, options: [{ id: 'y', name: 'Allow', kind: 'allow_once' }] }
    let s = run([{ kind: 'permission_request', permission }])
    s = answerPermission(s, 'p1', 'Allow')
    expect(s.items[0]).toMatchObject({ type: 'permission', answer: 'Allow', settled: true })

    let c = run([{ kind: 'permission_request', permission: { ...permission, id: 'p2' } }, { kind: 'turn_end', stopReason: 'cancelled' }])
    expect(c.items[0]).toMatchObject({ settled: true, answer: null })
    c = run([{ kind: 'permission_settled', requestId: 'zzz' }], c)
    expect(c.items).toHaveLength(2)
  })

  it('lists edited files from diffs and edit calls, skipping failures', () => {
    const s = run([
      { kind: 'tool_call', toolCall: { id: '1', kind: 'edit', paths: ['/p/a.go'], status: 'completed' } },
      { kind: 'tool_call', toolCall: { id: '2', kind: 'edit', diffs: [{ path: '/p/b.go', oldText: '', newText: 'x' }] } },
      { kind: 'tool_call', toolCall: { id: '3', kind: 'edit', paths: ['/p/c.go'], status: 'failed' } },
      { kind: 'tool_call', toolCall: { id: '4', kind: 'read', paths: ['/p/d.go'] } },
    ])
    expect(editedFiles(s)).toEqual(['/p/a.go', '/p/b.go'])
  })

  it('ignores unknown events without allocating', () => {
    const s = run([{ kind: 'message_chunk', text: 'x' }])
    expect(applyEvent(s, ev({ kind: 'something_new' }))).toBe(s)
  })
})
