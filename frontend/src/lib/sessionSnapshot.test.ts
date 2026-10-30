import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { inside, parseSnapshot, trimChat } from './sessionSnapshot'
import type { ChatState } from './agentReducer'

describe('inside', () => {
  it('matches the root and paths under it, not siblings with the same prefix', () => {
    assert.equal(inside('/p/app', '/p/app'), true)
    assert.equal(inside('/p/app/src/main.go', '/p/app'), true)
    assert.equal(inside('/p/app/x', '/p/app/'), true)
    assert.equal(inside('/p/application/x', '/p/app'), false)
    assert.equal(inside('/elsewhere', '/p/app'), false)
  })
})

describe('parseSnapshot', () => {
  it('accepts what Flint wrote and drops unknown tabs', () => {
    const snap = parseSnapshot(JSON.stringify({ version: 1, active: 1, tabs: [{ kind: 'file', path: '/p/a' }, { kind: 'weird' }, null] }))
    assert.deepEqual(snap, { version: 1, active: 1, tabs: [{ kind: 'file', path: '/p/a' }] })
  })

  it('rejects empty, broken and foreign documents', () => {
    assert.equal(parseSnapshot(''), null)
    assert.equal(parseSnapshot('{'), null)
    assert.equal(parseSnapshot('null'), null)
    assert.equal(parseSnapshot(JSON.stringify({ version: 99, active: 0, tabs: [] })), null)
    assert.equal(parseSnapshot(JSON.stringify({ version: 1, active: 0, tabs: {} })), null)
  })

  it('defaults a bad active index to none', () => {
    assert.equal(parseSnapshot(JSON.stringify({ version: 1, active: 'x', tabs: [] }))?.active, -1)
  })
})

describe('trimChat', () => {
  it('keeps recent items, cuts long outputs and drops big diffs', () => {
    const long = 'x'.repeat(25_000)
    const chat: ChatState = {
      busy: true,
      plan: [],
      items: [
        ...Array.from({ length: 250 }, (_, i) => ({ type: 'assistant' as const, text: `m${i}` })),
        {
          type: 'tool',
          call: {
            id: 't1',
            output: long,
            diffs: [
              { path: '/p/small', oldText: 'a', newText: 'b' },
              { path: '/p/big', oldText: long, newText: long },
            ],
          },
        },
      ],
    }
    const out = trimChat(chat)
    assert.equal(out.busy, false)
    assert.equal(out.items.length, 200)
    assert.deepEqual(out.items[0], { type: 'assistant', text: 'm51' })
    const tool = out.items[199]
    if (tool.type !== 'tool') throw new Error('expected tool item')
    assert.ok(tool.call.output!.length < 8_010)
    assert.deepEqual(tool.call.diffs!.map((d) => d.path), ['/p/small'])
    assert.equal(chat.items.length, 251) // input untouched
  })
})
