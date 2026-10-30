import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { StringStream } from '@codemirror/language'
import { goMod } from './gomod'

function tokens(line: string, inBlock = false): [string, string | null][] {
  const state = { inBlock }
  const stream = new StringStream(line, 4, 2)
  const out: [string, string | null][] = []
  while (!stream.eol()) {
    const style = goMod.token(stream, state)
    const text = stream.current()
    if (text.trim()) out.push([text, style])
    stream.start = stream.pos
  }
  return out
}

describe('go.mod highlighting', () => {
  it('marks directives, versions and comments', () => {
    assert.deepEqual(tokens('go 1.22'), [['go', 'keyword'], ['1.22', 'number']])
    assert.deepEqual(tokens('module github.com/me/x'), [['module', 'keyword'], ['github.com/me/x', 'variableName.special']])
    assert.deepEqual(tokens('\tgithub.com/a/b v1.2.3 // indirect', true), [
      ['github.com/a/b', 'variableName.special'],
      ['v1.2.3', 'number'],
      ['// indirect', 'comment'],
    ])
  })

  it('handles replace arrows, pseudo-versions and go.sum hashes', () => {
    const repl = tokens('replace a => ../a')
    assert.deepEqual(repl[0], ['replace', 'keyword'])
    assert.ok(repl.some((t) => t[0] === '=>' && t[1] === 'operator'))
    assert.deepEqual(tokens('x v0.0.0-20240101000000-abcdef123456')[1], ['v0.0.0-20240101000000-abcdef123456', 'number'])
    assert.deepEqual(tokens('x v1.0.0 h1:AbC+/=')[2], ['h1:AbC+/=', 'string'])
  })
})
