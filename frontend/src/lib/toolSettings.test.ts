import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceSettings, DEFAULT_SETTINGS } from './settings'
import {
  clampRefresh, coerceAgents, coerceServers, DEFAULT_REFRESH, EMPTY_AGENT, formatArgs, isDefaultAgent, isDefaultServer,
  parseArgs, refreshMs, withAgent, withServer,
} from './toolSettings'

describe('refresh intervals', () => {
  it('clamps into the same bounds as Go', () => {
    assert.equal(clampRefresh('kubernetesSeconds', 1), 2)
    assert.equal(clampRefresh('kubernetesSeconds', 10_000), 600)
    assert.equal(clampRefresh('awsSeconds', 4), 5)
    assert.equal(clampRefresh('awsSeconds', 7.6), 8)
  })

  it('uses the default for missing or non-numeric values', () => {
    assert.equal(clampRefresh('awsSeconds', undefined), DEFAULT_REFRESH.awsSeconds)
    assert.equal(clampRefresh('kubernetesSeconds', '9'), DEFAULT_REFRESH.kubernetesSeconds)
    assert.equal(clampRefresh('kubernetesSeconds', Number.NaN), DEFAULT_REFRESH.kubernetesSeconds)
    assert.equal(clampRefresh('kubernetesSeconds', 0), DEFAULT_REFRESH.kubernetesSeconds)
  })

  it('converts to milliseconds', () => {
    assert.equal(refreshMs({ kubernetesSeconds: 5, awsSeconds: 30 }, 'kubernetesSeconds'), 5000)
    assert.equal(refreshMs({ kubernetesSeconds: 0, awsSeconds: 99_999 }, 'awsSeconds'), 3_600_000)
  })
})

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
    const s = coerceSettings({ theme: 'light' })
    assert.deepEqual(s.agents, {})
    assert.deepEqual(s.languageServers, {})
    assert.deepEqual(s.refresh, DEFAULT_SETTINGS.refresh)
  })
})
