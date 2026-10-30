import { describe, expect, it } from 'vitest'
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
    expect(tokens('go 1.22')).toEqual([['go', 'keyword'], ['1.22', 'number']])
    expect(tokens('module github.com/me/x')).toEqual([['module', 'keyword'], ['github.com/me/x', 'variableName.special']])
    expect(tokens('\tgithub.com/a/b v1.2.3 // indirect', true)).toEqual([
      ['github.com/a/b', 'variableName.special'],
      ['v1.2.3', 'number'],
      ['// indirect', 'comment'],
    ])
  })

  it('handles replace arrows, pseudo-versions and go.sum hashes', () => {
    const repl = tokens('replace a => ../a')
    expect(repl[0]).toEqual(['replace', 'keyword'])
    expect(repl).toContainEqual(['=>', 'operator'])
    expect(tokens('x v0.0.0-20240101000000-abcdef123456')[1]).toEqual(['v0.0.0-20240101000000-abcdef123456', 'number'])
    expect(tokens('x v1.0.0 h1:AbC+/=')[2]).toEqual(['h1:AbC+/=', 'string'])
  })
})
