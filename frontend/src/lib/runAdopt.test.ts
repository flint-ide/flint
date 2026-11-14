import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { parseRunAdoption } from './runAdopt'

describe('runs started for an agent', () => {
  it('reads a test run', () => {
    const a = parseRunAdoption({ id: 'agent-1', kind: 'tests', title: 'TestA', root: '/p', request: { lang: 'go', dirs: ['/p/pkg'], tests: ['TestA'] } })
    assert.deepEqual(a, { kind: 'tests', id: 'agent-1', title: 'TestA', root: '/p', request: { lang: 'go', dirs: ['/p/pkg'], tests: ['TestA'] } })
    const file = parseRunAdoption({ id: 'x', kind: 'tests', title: 'a.test.ts', root: '/p', request: { lang: 'js', dirs: ['/p'], file: '/p/a.test.ts', tests: [] } })
    assert.deepEqual(file?.kind === 'tests' && file.request, { lang: 'js', dirs: ['/p'], file: '/p/a.test.ts' })
  })
  it('reads a run configuration', () => {
    const a = parseRunAdoption({ id: 'agent-2', kind: 'config', title: 'dev', root: '/p', config: { id: 'dev', name: 'dev', type: 'npm', script: 'dev' } })
    assert.equal(a?.kind, 'config')
    assert.equal(a?.kind === 'config' && a.config.id, 'dev')
  })
  it('refuses anything else', () => {
    for (const bad of [
      null,
      'run',
      { id: '', kind: 'tests', title: 't', root: '/p', request: { lang: 'go', dirs: ['/p'] } },
      { id: 'a', kind: 'tests', title: 't', root: '/p' },
      { id: 'a', kind: 'tests', title: 't', root: '/p', request: { lang: 'ruby', dirs: ['/p'] } },
      { id: 'a', kind: 'tests', title: 't', root: '/p', request: { lang: 'go', dirs: [] } },
      { id: 'a', kind: 'tests', title: 't', root: '/p', request: { lang: 'go', dirs: [1] } },
      { id: 'a', kind: 'config', title: 't', root: '/p', config: { id: 'x' } },
      { id: 'a', kind: 'debug', title: 't', root: '/p' },
      { id: 'a', kind: 'config', title: 't', config: { id: 'x', name: 'x', type: 'go' } },
    ]) {
      assert.equal(parseRunAdoption(bad), null, JSON.stringify(bad))
    }
  })
})
