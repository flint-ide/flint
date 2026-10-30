import { describe, expect, it } from 'vitest'
import { inside, parseSnapshot, trimChat } from './sessionSnapshot'
import type { ChatState } from './agentReducer'

describe('inside', () => {
  it('matches the root and paths under it, not siblings with the same prefix', () => {
    expect(inside('/p/app', '/p/app')).toBe(true)
    expect(inside('/p/app/src/main.go', '/p/app')).toBe(true)
    expect(inside('/p/app/x', '/p/app/')).toBe(true)
    expect(inside('/p/application/x', '/p/app')).toBe(false)
    expect(inside('/elsewhere', '/p/app')).toBe(false)
  })
})

describe('parseSnapshot', () => {
  it('accepts what Flint wrote and drops unknown tabs', () => {
    const snap = parseSnapshot(JSON.stringify({ version: 1, active: 1, tabs: [{ kind: 'file', path: '/p/a' }, { kind: 'weird' }, null] }))
    expect(snap).toEqual({ version: 1, active: 1, tabs: [{ kind: 'file', path: '/p/a' }] })
  })

  it('rejects empty, broken and foreign documents', () => {
    expect(parseSnapshot('')).toBeNull()
    expect(parseSnapshot('{')).toBeNull()
    expect(parseSnapshot('null')).toBeNull()
    expect(parseSnapshot(JSON.stringify({ version: 99, active: 0, tabs: [] }))).toBeNull()
    expect(parseSnapshot(JSON.stringify({ version: 1, active: 0, tabs: {} }))).toBeNull()
  })

  it('defaults a bad active index to none', () => {
    expect(parseSnapshot(JSON.stringify({ version: 1, active: 'x', tabs: [] }))?.active).toBe(-1)
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
    expect(out.busy).toBe(false)
    expect(out.items).toHaveLength(200)
    expect(out.items[0]).toEqual({ type: 'assistant', text: 'm51' })
    const tool = out.items[199]
    if (tool.type !== 'tool') throw new Error('expected tool item')
    expect(tool.call.output!.length).toBeLessThan(8_010)
    expect(tool.call.diffs!.map((d) => d.path)).toEqual(['/p/small'])
    expect(chat.items).toHaveLength(251) // input untouched
  })
})
