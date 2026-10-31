import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { groupTools, lookedFor, statusLabel, summarize, toolTone, type ToolStatus } from './toolCheck'

const tool = (over: Partial<ToolStatus>): ToolStatus => ({
  id: 'x', name: 'X', group: 'core', feature: '', required: false, install: '', docsUrl: '',
  binaries: ['x'], state: 'ok', binary: 'x', version: '1.0.0', detail: '', ...over,
})

describe('tool check', () => {
  it('colors rows by state', () => {
    assert.equal(toolTone({ state: 'ok', required: true }), 'ok')
    assert.equal(toolTone({ state: 'error', required: false }), 'warn')
    assert.equal(toolTone({ state: 'missing', required: true }), 'bad')
    assert.equal(toolTone({ state: 'missing', required: false }), 'muted')
  })

  it('labels versions, alternatives and problems', () => {
    assert.equal(statusLabel(tool({ version: '2.43.0' })), '2.43.0')
    assert.equal(statusLabel(tool({ version: '' })), 'Installed')
    const ts = { binaries: ['vtsls', 'typescript-language-server'], binary: 'typescript-language-server' }
    assert.equal(statusLabel(tool({ ...ts, version: '4.3.3' })), 'typescript-language-server 4.3.3')
    assert.equal(statusLabel(tool({ ...ts, binary: 'vtsls', version: '' })), 'vtsls')
    assert.equal(statusLabel(tool({ state: 'missing', binary: '', version: '' })), 'Not installed')
    assert.equal(statusLabel(tool({ state: 'error', version: '' })), 'Not working')
  })

  it('lists alternatives', () => {
    assert.equal(lookedFor(['gopls']), 'gopls')
    assert.equal(lookedFor(['vtsls', 'typescript-language-server']), 'vtsls or typescript-language-server')
    assert.equal(lookedFor(['a', 'b', 'c']), 'a, b or c')
    assert.equal(lookedFor([]), '')
  })

  it('groups in display order with problems first', () => {
    const list = [
      tool({ id: 'kubectl', group: 'infra' }),
      tool({ id: 'rg', group: 'core', state: 'missing' }),
      tool({ id: 'gh', group: 'core', state: 'error' }),
      tool({ id: 'git', group: 'core', state: 'missing', required: true }),
      tool({ id: 'new', group: 'someday' }),
    ]
    const groups = groupTools(list)
    assert.deepEqual(groups.map((g) => g.id), ['core', 'infra', 'other'])
    assert.deepEqual(groups[0].tools.map((t) => t.id), ['git', 'gh', 'rg'])
    assert.equal(groups[2].title, 'Other')
    assert.deepEqual(list.map((t) => t.id), ['kubectl', 'rg', 'gh', 'git', 'new'], 'input left untouched')
  })

  it('summarizes what needs attention', () => {
    assert.deepEqual(summarize([tool({}), tool({})]), { installed: 2, total: 2, tone: 'ok', text: 'All 2 tools installed' })
    const s = summarize([
      tool({ name: 'Git', required: true, state: 'missing' }),
      tool({ name: 'Rust language server', state: 'error' }),
      tool({ name: 'ripgrep', state: 'missing' }),
      tool({}),
    ])
    assert.equal(s.text, '1 of 4 installed · Git required · Rust language server not working')
    assert.equal(s.tone, 'bad')
    assert.equal(summarize([tool({ state: 'error' })]).tone, 'warn')
  })
})
