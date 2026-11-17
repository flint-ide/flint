import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { neighbourTab, settledToReveal } from './settledTabs'
import type { AgentRow, AgentRunState } from './agentList'

const row = (sessionId: string, state: AgentRunState): AgentRow => ({
  key: `chat:${sessionId}`, kind: 'chat', sessionId, agent: 'claude', title: 't', root: '/p', project: 'p', state, hidden: false,
})

describe('settledToReveal', () => {
  const rows = [row('a', 'waiting'), row('b', 'failed'), row('c', 'finished-unseen'), row('d', 'working'), row('e', 'idle'), row('f', 'queued')]
  it('reveals settled chats whose agent needs the user', () => assert.deepEqual(settledToReveal(['a', 'b', 'c'], rows), ['a', 'b', 'c']))
  it('keeps hidden the ones that work, are queued or idle', () => assert.deepEqual(settledToReveal(['d', 'e', 'f'], rows), []))
  it('reveals only settled chats', () => assert.deepEqual(settledToReveal(['b'], rows), ['b']))
  it('ignores settled chats that have no row (closed)', () => assert.deepEqual(settledToReveal(['zzz'], rows), []))
})

describe('neighbourTab', () => {
  const ids = ['t1', 't2', 't3', 't4']
  it('takes the next tab that is drawn', () => assert.equal(neighbourTab(ids, new Set(['t3']), 't2'), 't4'))
  it('falls back to the previous one at the end', () => assert.equal(neighbourTab(ids, new Set(), 't4'), 't3'))
  it('skips hidden tabs on both sides', () => assert.equal(neighbourTab(ids, new Set(['t3', 't4']), 't2'), 't1'))
  it('is null when nothing else is drawn', () => assert.equal(neighbourTab(['t1', 't2'], new Set(['t2']), 't1'), null))
  it('is null for an unknown tab', () => assert.equal(neighbourTab(ids, new Set(), 'nope'), null))
})
