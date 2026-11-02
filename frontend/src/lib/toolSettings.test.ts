import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceSettings } from './settings'
import { coerceAgents, coerceServers, EMPTY_AGENT, formatArgs, isDefaultAgent, isDefaultServer, parseArgs, withAgent, withServer } from './toolSettings'

describe('extra arguments', () => {
  it('takes one argument per line, so values may contain spaces', () => {
    assert.deepEqual(parseArgs('--add-dir\n /Users/me/my notes \n\n--verbose\n'), ['--add-dir', '/Users/me/my notes', '--verbose'])
    assert.deepEqual(parseArgs(''), [])
  })

  it('round-trips through the textarea format', () => {
    const args = ['-c', 'model_reasoning_effort=high']
    assert.deepEqual(parseArgs(formatArgs(args)), args)
  })
})

describe('agent and language-server entries', () => {
  it('changes one entry without touching the input', () => {
    const agents = { codex: { path: '', model: 'o3', args: [] } }
    const next = withAgent(agents, 'claude', { model: 'opus' })
    assert.deepEqual(next.claude, { ...EMPTY_AGENT, model: 'opus' })
    assert.equal(next.codex, agents.codex)
    assert.equal('claude' in agents, false)
    const servers = withServer({}, 'rust', { disabled: true })
    assert.deepEqual(servers.rust, { disabled: true, command: '', args: [] })
  })

  it('knows when an entry is all defaults', () => {
    assert.equal(isDefaultAgent(undefined), true)
    assert.equal(isDefaultAgent({ path: '', model: '', args: [] }), true)
    assert.equal(isDefaultAgent({ path: '', model: '', args: ['--verbose'] }), false)
    assert.equal(isDefaultServer({ disabled: true, command: '', args: [] }), false)
  })

  it('coerces untrusted JSON', () => {
    assert.deepEqual(coerceAgents({ claude: { path: 1, model: 'opus', args: ['a', 2] }, codex: 'x' }), {
      claude: { path: '', model: 'opus', args: ['a'] },
    })
    assert.deepEqual(coerceServers({ go: { disabled: 'yes', command: 'gopls' } }), { go: { disabled: false, command: 'gopls', args: [] } })
    assert.deepEqual(coerceAgents(null), {})
  })

  it('fills the new keys of settings saved by an older version', () => {
    const s = coerceSettings({ theme: 'light', refresh: { kubernetesSeconds: 5 } })
    assert.deepEqual(s.agents, {})
    assert.deepEqual(s.languageServers, {})
    assert.equal('refresh' in s, false, 'the refresh intervals went with the Kubernetes and AWS panels')
  })
})
