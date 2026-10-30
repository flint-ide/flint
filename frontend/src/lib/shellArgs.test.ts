import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { formatEnv, joinArgs, parseEnv, splitArgs } from './shellArgs'

describe('shellArgs', () => {
  it('splits like a shell', () => {
    assert.deepEqual(splitArgs(`-port 8080  --name "my app" 'it''s' a\\ b ""`), ['-port', '8080', '--name', 'my app', 'its', 'a b', ''])
    assert.deepEqual(splitArgs('   '), [])
  })

  it('round-trips through joinArgs', () => {
    const args = ['serve', 'my app', "it's", '', '--x=1']
    assert.equal(joinArgs(args), `serve 'my app' 'it'\\''s' '' --x=1`)
    assert.deepEqual(splitArgs(joinArgs(args)), args)
  })

  it('parses env lines', () => {
    assert.deepEqual(parseEnv('A=1\n# note\n\nB = two=2\nbad\n=x'), { A: '1', B: ' two=2' })
    assert.equal(formatEnv({ A: '1', B: '2' }), 'A=1\nB=2')
  })
})
