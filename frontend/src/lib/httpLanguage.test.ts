import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { StringStream } from '@codemirror/language'
import { httpRequests, type HttpState } from './httpLanguage'

/** Styled tokens per line of a whole document, the way CodeMirror feeds the parser. */
function tokenize(doc: string): [string, string | null][][] {
  const state: HttpState = httpRequests.startState!(2)
  return doc.split('\n').map((line) => {
    if (!line.trim()) {
      httpRequests.blankLine?.(state, 2)
      return []
    }
    const stream = new StringStream(line, 4, 2)
    const out: [string, string | null][] = []
    while (!stream.eol()) {
      const style = httpRequests.token(stream, state)
      const text = stream.current()
      if (text.trim()) out.push([text.trim(), style])
      stream.start = stream.pos
    }
    return out
  })
}

describe('.http highlighting', () => {
  it('marks separators, variables, the request line and headers', () => {
    const lines = tokenize('@host = http://localhost\n### Create\nPOST {{host}}/users HTTP/1.1\nAuthorization: Bearer {{token}}\n\n{"a": 1, "b": true}')
    assert.deepEqual(lines[0], [['@host', 'variableName.definition'], ['=', 'operator'], ['http://localhost', 'string']])
    assert.deepEqual(lines[1], [['### Create', 'heading']])
    assert.deepEqual(lines[2], [['POST', 'keyword'], ['{{host}}', 'atom'], ['/users', 'url'], ['HTTP/1.1', 'typeName']])
    assert.deepEqual(lines[3], [['Authorization', 'propertyName'], [':', 'punctuation'], ['Bearer', null], ['{{token}}', 'atom']])
    assert.deepEqual(lines[5], [['{', null], ['"a"', 'string'], [':', null], ['1', 'number'], [',', null], ['"b"', 'string'], [':', null], ['true', 'atom'], ['}', null]])
  })

  it('treats a line without a method as a GET URL', () => {
    assert.deepEqual(tokenize('https://example.test/ping')[0], [['https://example.test/ping', 'url']])
  })

  it('highlights comments and directives before the request', () => {
    const lines = tokenize('# plain\n// @name create\nGET /x')
    assert.deepEqual(lines[0], [['# plain', 'comment']])
    assert.deepEqual(lines[1], [['//', 'comment'], ['@name', 'keyword'], ['create', 'comment']])
    assert.deepEqual(lines[2][0], ['GET', 'keyword'])
  })

  it('continues long URLs and flags broken headers', () => {
    const lines = tokenize('GET http://x.test/a\n    ?page=1\n    &size={{n}}\nnot a header')
    assert.deepEqual(lines[1], [['?page=1', 'url']])
    assert.deepEqual(lines[2], [['&size=', 'url'], ['{{n}}', 'atom']])
    assert.equal(lines[3][0][1], 'invalid')
  })

  it('recognizes a body file reference but keeps XML inline', () => {
    const file = tokenize('POST http://x.test\n\n< ./payload.json')
    assert.deepEqual(file[2], [['<', 'operator'], ['./payload.json', 'string']])
    const xml = tokenize('POST http://x.test\n\n<root>{{id}}</root>')
    assert.ok(xml[2].every(([, style]) => style !== 'operator'))
    assert.ok(xml[2].some(([text, style]) => text === '{{id}}' && style === 'atom'))
  })

  it('starts over after ###', () => {
    const lines = tokenize('GET http://a.test\n\nbody\n###\nPUT http://b.test')
    assert.deepEqual(lines[4][0], ['PUT', 'keyword'])
  })
})
