import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { terminalTitle } from './terminalTitle'

describe('terminal titles', () => {
  it('names an idle shell after its folder', () => {
    assert.equal(terminalTitle({ command: '', cwd: '/Users/me/projects/flint' }), 'flint')
    assert.equal(terminalTitle({ command: '', cwd: '/' }), '~')
  })

  it('names a busy terminal after its command', () => {
    assert.equal(terminalTitle({ command: 'npm run dev', cwd: '/p/web' }), 'npm run dev')
  })

  it('prefers the title the running program set', () => {
    assert.equal(terminalTitle({ command: 'claude', cwd: '/p', programTitle: '✳ Fix login' }), '✳ Fix login')
    assert.equal(terminalTitle({ command: 'claude', cwd: '/p', programTitle: '  ' }), 'claude')
  })

  it('ignores titles the shell sets at the prompt', () => {
    assert.equal(terminalTitle({ command: '', cwd: '/p/api', programTitle: 'me@mac: ~/p/api' }), 'api')
  })

  it('cuts long titles', () => {
    assert.equal(terminalTitle({ command: 'x'.repeat(50), cwd: '/p' }), `${'x'.repeat(39)}…`)
  })
})
