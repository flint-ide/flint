import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coercePromptSettings, lastFileTab, matchPrompts, slashQuery, sourceLabel, validPromptName, type PromptInfo } from './prompts'

const prompt = (p: Partial<PromptInfo>): PromptInfo => ({ name: 'x', description: '', agent: '', args: [], source: 'builtin', untrusted: false, ...p })

const LIST = [
  prompt({ name: 'review', description: 'Review the uncommitted changes' }),
  prompt({ name: 'test', description: 'Write tests' }),
  prompt({ name: 'pre-review', description: 'Before review' }),
  prompt({ name: 'claude-only', agent: 'claude', description: 'For Claude' }),
  prompt({ name: 'explain', description: 'Explain the code' }),
]

describe('prompt library', () => {
  it('opens the picker only for a single /word draft', () => {
    assert.equal(slashQuery('/'), '')
    assert.equal(slashQuery('/rev'), 'rev')
    assert.equal(slashQuery('/plugin:cmd'), 'plugin:cmd')
    assert.equal(slashQuery('/compact focus on tests'), null)
    assert.equal(slashQuery('/review\nmore'), null)
    assert.equal(slashQuery('text /review'), null)
    assert.equal(slashQuery(''), null)
  })

  it('ranks exact, prefix, substring, then description matches', () => {
    assert.deepEqual(matchPrompts(LIST, 'review', 'codex').map((p) => p.name), ['review', 'pre-review'])
    assert.deepEqual(matchPrompts(LIST, 'Re', 'codex').map((p) => p.name), ['review', 'pre-review'])
    assert.deepEqual(matchPrompts(LIST, 'tests', 'codex').map((p) => p.name), ['test'])
    assert.deepEqual(matchPrompts(LIST, 'code', 'codex').map((p) => p.name), ['explain'])
  })

  it('lists everything for an empty query and hides other agents’ prompts', () => {
    assert.deepEqual(matchPrompts(LIST, '', 'codex').map((p) => p.name), ['explain', 'pre-review', 'review', 'test'])
    assert.ok(matchPrompts(LIST, '', 'claude').some((p) => p.name === 'claude-only'))
    assert.ok(matchPrompts(LIST, '', '').some((p) => p.name === 'claude-only'))
  })

  it('finds nothing for an agent command, so it is sent unchanged', () => {
    assert.deepEqual(matchPrompts(LIST, 'compact', 'claude'), [])
  })

  it('labels where a prompt comes from', () => {
    assert.equal(sourceLabel(prompt({ source: 'project' })), 'project')
    assert.equal(sourceLabel(prompt({ source: 'project', untrusted: true })), 'untrusted repo')
    assert.equal(sourceLabel(prompt({ source: 'personal' })), 'personal')
    assert.equal(sourceLabel(prompt({ source: 'builtin' })), 'built-in')
  })

  it('defaults the settings like Go', () => {
    assert.deepEqual(coercePromptSettings(undefined), { builtins: true })
    assert.deepEqual(coercePromptSettings({ builtins: 'no' }), { builtins: true })
    assert.deepEqual(coercePromptSettings({ builtins: false }), { builtins: false })
  })

  it('validates names like Go', () => {
    assert.ok(validPromptName('fix-issue_2'))
    for (const bad of ['', '-x', 'a b', 'a.b', '../x', 'x'.repeat(65)]) assert.ok(!validPromptName(bad), bad)
  })

  it('finds the most recent file tab', () => {
    const tabs = [{ id: 'f1', kind: 'file' }, { id: 'chat:1', kind: 'chat' }, { id: 'f2', kind: 'file' }]
    assert.equal(lastFileTab(tabs, 'f2', ['f1'])?.id, 'f2')
    assert.equal(lastFileTab(tabs, 'chat:1', ['f2', 'f1'])?.id, 'f1')
    assert.equal(lastFileTab(tabs, 'chat:1', []), null)
  })
})
