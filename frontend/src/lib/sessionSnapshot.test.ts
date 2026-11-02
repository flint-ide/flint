import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { chatsNeedSave, inside, parseSnapshot, trimChat, type AutosavedChat } from './sessionSnapshot'
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

  it('keeps the selected run configuration, ignoring bad values', () => {
    assert.equal(parseSnapshot(JSON.stringify({ version: 1, active: 0, tabs: [], runConfig: 'server' }))?.runConfig, 'server')
    assert.equal(parseSnapshot(JSON.stringify({ version: 1, active: 0, tabs: [], runConfig: 7 }))?.runConfig, undefined)
  })

  it('keeps the selected run configuration of each workspace folder, ignoring bad entries', () => {
    const runConfigs = { '/w/api': 'server', '/w/web': 'detected:npm:dev', rel: 'x', '/w/docs': '', '/w/x': 3 }
    const snap = parseSnapshot(JSON.stringify({ version: 1, active: 0, tabs: [], runConfigs }))
    assert.deepEqual(snap?.runConfigs, { '/w/api': 'server', '/w/web': 'detected:npm:dev' })
    assert.equal('runConfigs' in (parseSnapshot(JSON.stringify({ version: 1, active: 0, tabs: [], runConfigs: ['/w'] })) ?? {}), false)
  })

  it('keeps history tabs and revision diffs', () => {
    const tabs = [
      { kind: 'history', path: '/p/a' },
      { kind: 'diff', path: '/p/a', basePath: '/p/a', repoRoot: '/p', title: 'Δ a', baseRev: 'x^', rev: 'x' },
    ]
    assert.deepEqual(parseSnapshot(JSON.stringify({ version: 1, active: 0, tabs }))?.tabs, tabs)
  })

  it('defaults a bad active index to none', () => {
    assert.equal(parseSnapshot(JSON.stringify({ version: 1, active: 'x', tabs: [] }))?.active, -1)
  })

  it('keeps attached folders, dropping anything that is not an absolute path', () => {
    const snap = parseSnapshot(JSON.stringify({ version: 1, active: -1, tabs: [], roots: ['/w/web', 3, 'rel', '/w/docs'] }))
    assert.deepEqual(snap?.roots, ['/w/web', '/w/docs'])
    // A one-folder session (written before workspaces existed) has no roots at all.
    assert.equal('roots' in (parseSnapshot(JSON.stringify({ version: 1, active: -1, tabs: [] })) ?? {}), false)
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

describe('trimChat cache', () => {
  it('returns the same trimmed copy for the same chat', () => {
    const chat: ChatState = { busy: false, plan: [], items: [{ type: 'assistant', text: 'hi' }] }
    assert.equal(trimChat(chat), trimChat(chat))
    assert.notEqual(trimChat({ ...chat }), trimChat(chat))
  })
})

describe('chatsNeedSave', () => {
  const session = (chat: Partial<ChatState> = {}, rest: Partial<AutosavedChat> = {}): AutosavedChat => ({
    chat: { busy: true, plan: [], items: [{ type: 'assistant', text: 'a' }], ...chat },
    draft: '',
    ...rest,
  })

  it('ignores text streaming into the last item', () => {
    const a = session()
    const b = session({ items: [{ type: 'assistant', text: 'ab' }] })
    assert.equal(chatsNeedSave({ s: a }, { s: b }), false)
    assert.equal(chatsNeedSave({ s: a }, { s: a }), false)
  })

  it('saves for opened or closed chats and structural changes', () => {
    const a = session()
    assert.equal(chatsNeedSave({}, { s: a }), true)
    assert.equal(chatsNeedSave({ s: a }, {}), true)
    assert.equal(chatsNeedSave({ s: a }, { t: a }), true)
    assert.equal(chatsNeedSave({ s: a }, { s: session({ busy: false }) }), true)
    assert.equal(chatsNeedSave({ s: a }, { s: session({ items: [...a.chat.items, { type: 'error', text: 'x' }] }) }), true)
    assert.equal(chatsNeedSave({ s: a }, { s: session({ totals: { inputTokens: 1, outputTokens: 0, costUsd: 0, contextTokens: 0, contextWindow: 0 } }) }), true)
    assert.equal(chatsNeedSave({ s: a }, { s: session({}, { draft: 'hi' }) }), true)
    assert.equal(chatsNeedSave({ s: a }, { s: session({}, { dormant: true }) }), true)
  })
})

describe('parseSnapshot: extension tabs', () => {
  it('keeps tab views with a key and drops malformed ones', () => {
    const raw = JSON.stringify({
      version: 1,
      active: 0,
      tabs: [
        { kind: 'contrib', view: 'flint.database.console', key: 'c1', title: 'Prod', data: { sql: 'select 1' } },
        { kind: 'contrib', view: 'flint.database.console', key: '', title: 'no key' },
        { kind: 'contrib', view: 'not an id', key: 'k', title: 'x' },
      ],
    })
    const snap = parseSnapshot(raw)
    assert.deepEqual(snap?.tabs, [{ kind: 'contrib', view: 'flint.database.console', key: 'c1', title: 'Prod', data: { sql: 'select 1' } }])
  })
})

