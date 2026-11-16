import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceResume, resumeCommand } from './terminalResume'

const id = '0f8fad5b-d9cb-469f-a165-70867728950e'

describe('resuming an agent in a reopened terminal', () => {
  it('resumes the very conversation of Claude Code', () => {
    assert.equal(resumeCommand({ agent: 'claude', session: id }), `claude --resume ${id}`)
  })

  it('continues the latest conversation when the id is unknown', () => {
    assert.equal(resumeCommand({ agent: 'claude', session: '' }), 'claude --continue')
    assert.equal(resumeCommand({ agent: 'claude' }), 'claude --continue')
    assert.equal(resumeCommand({ agent: 'codex', session: '' }), 'codex resume --last')
    assert.equal(resumeCommand({ agent: 'codex', session: id }), 'codex resume --last')
  })

  it('types nothing for anything else', () => {
    for (const v of [null, undefined, '', 'claude', [], {}, { agent: '' }, { agent: 'npm' }, { agent: 'claude; rm -rf ~', session: id }, { agent: 3 }]) {
      assert.equal(resumeCommand(v), '', JSON.stringify(v))
      assert.equal(coerceResume(v), null, JSON.stringify(v))
    }
  })

  it('never lets a tampered id into the command', () => {
    for (const session of [`${id}; rm -rf ~`, `${id}\nrm -rf ~`, '$(reboot)', '--dangerously-skip-permissions', id.toUpperCase(), 7, null]) {
      assert.equal(resumeCommand({ agent: 'claude', session }), 'claude --continue', JSON.stringify(session))
    }
  })

  it('keeps only the agent and a valid id', () => {
    assert.deepEqual(coerceResume({ agent: 'claude', session: id, extra: 'x' }), { agent: 'claude', session: id })
    assert.deepEqual(coerceResume({ agent: 'codex', session: 'whatever' }), { agent: 'codex', session: '' })
  })
})
