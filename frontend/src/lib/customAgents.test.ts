import { it as test } from 'node:test'
import assert from 'node:assert/strict'
import {
  coerceCustomAgents,
  conflictHint,
  displayName,
  followDefault,
  idAvailable,
  PRESETS,
  suggestId,
  unusedPresets,
  withCustomAgent,
  withoutCustomAgent,
  type CustomAgent,
} from './customAgents'

const gemini: CustomAgent = { id: 'gemini', name: 'Gemini CLI', command: 'gemini', args: ['--experimental-acp'] }

test('coerceCustomAgents keeps well-formed entries only', () => {
  assert.deepEqual(coerceCustomAgents(null), [])
  assert.deepEqual(coerceCustomAgents({ id: 'x' }), [])
  assert.deepEqual(
    coerceCustomAgents([gemini, { id: 'nocommand' }, 'junk', { id: 'a', command: '/bin/a', args: ['x', 3], name: 7 }]),
    [gemini, { id: 'a', name: '', command: '/bin/a', args: ['x'] }],
  )
})

test('idAvailable mirrors the Go rules', () => {
  assert.equal(idAvailable('gemini', []), true)
  assert.equal(idAvailable('my-agent2', []), true)
  for (const bad of ['', 'Gemini', '2agent', 'my agent', 'claude', 'codex', 'a'.repeat(33)]) assert.equal(idAvailable(bad, []), false, bad)
  assert.equal(idAvailable('gemini', [gemini]), false)
  assert.equal(idAvailable('gemini', [gemini], 0), true, 'the entry being edited may keep its id')
})

test('suggestId derives a free id from a name or a path', () => {
  assert.equal(suggestId('Gemini CLI', []), 'gemini-cli')
  assert.equal(suggestId('/Users/me/.opencode/bin/opencode', []), 'opencode')
  assert.equal(suggestId('gemini', [gemini]), 'gemini-2')
  assert.equal(suggestId('Claude', []), 'claude-2')
  assert.equal(suggestId('123', []), 'agent')
})

test('list edits return new arrays', () => {
  const list = [gemini]
  const renamed = withCustomAgent(list, 0, { name: 'Gemini' })
  assert.equal(renamed[0].name, 'Gemini')
  assert.equal(list[0].name, 'Gemini CLI')
  assert.deepEqual(withoutCustomAgent(list, 0), [])
  assert.equal(list.length, 1)
})

test('followDefault keeps the default pointing at the right agent', () => {
  assert.equal(followDefault('gemini', 'gemini', 'gem'), 'gem')
  assert.equal(followDefault('gemini', 'gemini', ''), '')
  assert.equal(followDefault('claude', 'gemini', ''), 'claude')
})

test('presets: the ones not added yet; displayName falls back to the id', () => {
  assert.deepEqual(
    unusedPresets([gemini]).map((p) => p.id),
    PRESETS.filter((p) => p.id !== 'gemini').map((p) => p.id),
  )
  assert.equal(unusedPresets([{ id: 'oc', name: '', command: 'opencode', args: [] }]).some((p) => p.id === 'opencode'), false)
  assert.equal(displayName({ ...gemini, name: '' }), 'gemini')
  assert.equal(displayName(gemini), 'Gemini CLI')
})

test('conflictHint says when a native agent stands in', () => {
  assert.equal(conflictHint('claude', 'claude'), 'Start claude in a new terminal to resolve these conflicts')
  assert.match(conflictHint('gemini', 'codex'), /codex in a new terminal.*gemini has no terminal mode/)
})
