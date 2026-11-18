import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  agentDescription,
  agentOption,
  agentLabel,
  DEFAULT_TIERS,
  engineLabel,
  engineOf,
  ignoredNote,
  isTier,
  MODEL_TIERS,
  modelLabel,
  savedEngine,
  trustedRoots,
  validAgentName,
  type AgentDefInfo,
} from './agentDefs'
import { forAgent, matchPrompts, type PromptInfo } from './prompts'

const defs: AgentDefInfo[] = [
  { name: 'reviewer', description: 'Reviews diffs', engine: 'claude', resolvedEngine: 'claude', model: '', resolvedModel: '' },
  { name: 'helper', description: '', engine: '', resolvedEngine: 'codex', model: '', resolvedModel: '' },
  { name: 'ghost', description: 'Nothing installed', engine: '', resolvedEngine: '', model: '', resolvedModel: '' },
  { name: 'strong', description: '', engine: '', resolvedEngine: 'claude', model: 'strong', resolvedModel: 'opus' },
  { name: 'cx-strong', description: '', engine: 'codex', resolvedEngine: 'codex', model: 'strong', resolvedModel: '' },
  { name: 'named', description: '', engine: 'claude', resolvedEngine: 'claude', model: 'claude-opus-4-1', resolvedModel: 'claude-opus-4-1' },
]

describe('agent definitions', () => {
  it('labels pinned, unpinned and unresolved agents', () => {
    assert.equal(agentLabel('reviewer', defs), 'reviewer · claude')
    assert.equal(agentLabel('helper', defs), 'helper · default (codex)')
    assert.equal(agentLabel('ghost', defs), 'ghost · default')
    assert.equal(engineLabel(defs[0]), 'claude')
  })

  it('shows a tier with the model it means now', () => {
    assert.equal(modelLabel(defs[3]), 'strong (opus)')
    assert.equal(modelLabel(defs[4]), 'strong', 'an unmapped tier runs on the engine model')
    assert.equal(modelLabel(defs[5]), 'claude-opus-4-1', 'a model name is shown once')
    assert.equal(modelLabel(defs[0]), '')
    assert.equal(modelLabel({ model: 'opus', resolvedModel: '' }), 'opus')
    assert.equal(agentLabel('strong', defs), 'strong · default (claude) · strong (opus)')
    assert.equal(agentLabel('cx-strong', defs), 'cx-strong · codex · strong')
    assert.equal(agentLabel('named', defs), 'named · claude · claude-opus-4-1')
  })

  it('knows the tier names like Go', () => {
    assert.deepEqual([...MODEL_TIERS], ['fast', 'balanced', 'strong'])
    assert.ok(isTier('strong'))
    assert.ok(!isTier('Strong'))
    assert.ok(!isTier('opus'))
    assert.deepEqual(DEFAULT_TIERS.claude, { fast: 'haiku', balanced: 'sonnet', strong: 'opus' })
    assert.equal(DEFAULT_TIERS.codex, undefined)
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
