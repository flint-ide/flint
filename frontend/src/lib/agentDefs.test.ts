import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { agentDescription, agentOption, agentLabel, engineLabel, engineOf, ignoredNote, savedEngine, trustedRoots, validAgentName, type AgentDefInfo } from './agentDefs'
import { forAgent, matchPrompts, type PromptInfo } from './prompts'

const defs: AgentDefInfo[] = [
  { name: 'reviewer', description: 'Reviews diffs', engine: 'claude', resolvedEngine: 'claude' },
  { name: 'helper', description: '', engine: '', resolvedEngine: 'codex' },
  { name: 'ghost', description: 'Nothing installed', engine: '', resolvedEngine: '' },
]

describe('agent definitions', () => {
  it('labels pinned, unpinned and unresolved agents', () => {
    assert.equal(agentLabel('reviewer', defs), 'reviewer · claude')
    assert.equal(agentLabel('helper', defs), 'helper · default (codex)')
    assert.equal(agentLabel('ghost', defs), 'ghost · default')
    assert.equal(engineLabel(defs[0]), 'claude')
  })

  it('leaves native and custom agents as they are', () => {
    assert.equal(agentLabel('claude', defs), 'claude')
    assert.equal(agentLabel('gemini', defs), 'gemini')
    assert.equal(agentDescription('gemini', defs), '')
    assert.equal(agentDescription('reviewer', defs), 'Reviews diffs')
  })

  it('resolves the engine behind a name', () => {
    assert.equal(engineOf('reviewer', defs), 'claude')
    assert.equal(engineOf('helper', defs), 'codex')
    assert.equal(engineOf('ghost', defs), '')
    assert.equal(engineOf('codex', defs), 'codex')
    assert.equal(engineOf('gemini', defs), 'gemini')
  })

  it("prefers the session's own engine over today's resolution", () => {
    assert.equal(engineOf('helper', defs, 'claude'), 'claude')
    assert.equal(engineOf('helper', defs, ''), 'codex')
  })

  it('reads the engine of a chat saved without one', () => {
    assert.equal(savedEngine({}), '')
    assert.equal(savedEngine({ engine: 42 }), '')
    assert.equal(savedEngine({ engine: 'codex' }), 'codex')
  })

  it('validates names like Go', () => {
    assert.ok(validAgentName('reviewer-2'))
    assert.ok(!validAgentName('Reviewer'))
    assert.ok(!validAgentName('2fast'))
    assert.ok(!validAgentName(''))
    assert.ok(!validAgentName('a'.repeat(33)))
  })

  it('lists trusted roots only', () => {
    assert.deepEqual(trustedRoots(['/a', '/b', '/c'], { '/a': true, '/c': false }), ['/a'])
  })

  it('words the ignored-folder note', () => {
    assert.match(ignoredNote({ dir: '/p/.flint/agents', count: 1 }), /1 definition not loaded/)
    assert.match(ignoredNote({ dir: '/p/.flint/agents', count: 3 }), /3 definitions not loaded/)
  })

  it('offers a prompt limited to an engine to a defined agent on it', () => {
    const p: PromptInfo = { name: 'x', description: '', agent: 'claude', args: [], source: 'personal', untrusted: false }
    assert.ok(forAgent(p, 'reviewer', 'claude'))
    assert.ok(!forAgent(p, 'reviewer', 'codex'))
    assert.ok(!forAgent(p, 'reviewer'))
    assert.ok(forAgent(p, 'claude'))
    assert.equal(matchPrompts([p], 'x', 'reviewer', 'claude').length, 1)
    assert.equal(matchPrompts([p], 'x', 'reviewer', 'codex').length, 0)
  })
})

describe('agent picker options', () => {
  it('shows the description and a marker next to it', () => {
    assert.deepEqual(agentOption('reviewer', defs, 'default'), { value: 'reviewer', label: 'reviewer · claude', hint: 'Reviews diffs · default' })
    assert.deepEqual(agentOption('claude', defs), { value: 'claude', label: 'claude' })
    assert.deepEqual(agentOption('claude', defs, 'not installed'), { value: 'claude', label: 'claude', hint: 'not installed' })
  })
})
