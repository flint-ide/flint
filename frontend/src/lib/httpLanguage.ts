import type { StreamParser, StringStream } from '@codemirror/language'

// Highlighting for .http / .rest request files (IntelliJ HTTP Client format). The
// line structure mirrors the Go parser (internal/httpfile): "###" separators, then
// comments and "@var = value" lines, the request line, headers, a blank line, a body.

export const HTTP_METHODS = new Set(['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS', 'TRACE', 'CONNECT'])

type Phase = 'before' | 'request' | 'headers' | 'body'

export interface HttpState {
  phase: Phase
  /** Where in the current line we are (reset at every line start). */
  part: 'start' | 'value' | 'path' | 'url'
  /** Body lines seen so far (the first may be a "< file" reference). */
  bodyLines: number
}

const VARIABLE = /^\{\{[^}]*\}\}/

function variable(stream: StringStream): boolean {
  return !!stream.match(VARIABLE)
}

export const httpRequests: StreamParser<HttpState> = {
  name: 'http',
  startState: () => ({ phase: 'before', part: 'start', bodyLines: 0 }),
  blankLine(state) {
    if (state.phase === 'request' || state.phase === 'headers') state.phase = 'body'
  },
  token(stream, state) {
    if (stream.sol()) {
      state.part = 'start'
      if (stream.match(/^###.*/)) {
        state.phase = 'before'
        state.bodyLines = 0
        return 'heading'
      }
      if (state.phase === 'request') state.phase = 'headers'
      if (state.phase === 'body') state.bodyLines++
    }
    switch (state.phase) {
      case 'before':
        return beforeRequest(stream, state)
      case 'request':
        return requestLine(stream)
      case 'headers':
        return header(stream, state)
      case 'body':
        return body(stream, state)
    }
  },
  languageData: { commentTokens: { line: '#' } },
}

function comment(stream: StringStream): string {
  // "# @name x" and other directives: the marker stands out, like in IntelliJ.
  if (stream.match(/^(#+|\/\/)\s*(?=@)/)) return 'comment'
  if (stream.match(/^@[\w-]+/)) return 'keyword'
  stream.skipToEnd()
  return 'comment'
}

function beforeRequest(stream: StringStream, state: HttpState): string | null {
  if (state.part === 'value') return value(stream, 'string')
  if (state.part === 'path') return comment(stream) // the rest of a directive comment
  if (stream.eatSpace()) return null
  if (stream.match(/^(#|\/\/)/, false)) {
    state.part = 'path'
    return comment(stream)
  }
  if (stream.match(/^@[A-Za-z_][\w.-]*/)) return 'variableName.definition'
  if (stream.match(/^\s*=/)) {
    state.part = 'value'
    return 'operator'
  }
  // Anything else starts the request line.
  state.phase = 'request'
  return requestLine(stream)
}

function requestLine(stream: StringStream): string | null {
  if (stream.eatSpace()) return null
  if (variable(stream)) return 'atom'
  const word = stream.match(/^[A-Z]+(?=\s)/, false) as RegExpMatchArray | null
  if (word && HTTP_METHODS.has(word[0]) && stream.column() === stream.indentation()) {
    stream.match(word[0])
    return 'keyword'
  }
  if (stream.match(/^HTTP\/\d(\.\d)?(?=\s*$)/)) return 'typeName'
  if (stream.match(/^[^\s{]+/) || stream.next()) return 'url'
  return null
}

function header(stream: StringStream, state: HttpState): string | null {
  if (state.part === 'value') return value(stream, null)
  if (state.part === 'url') return requestLine(stream)
  if (stream.sol() && stream.match(/^\s+(?=[?&])/)) return null // indented query continuation
  if (state.part === 'start' && stream.match(/^[?&]/, false)) {
    state.part = 'url'
    return requestLine(stream)
  }
  if (stream.match(/^\s*(#|\/\/)/, false)) {
    stream.skipToEnd()
    return 'comment'
  }
  if (stream.match(/^[^:\s]+(?=\s*:)/)) return 'propertyName'
  if (stream.match(/^\s*:/)) {
    state.part = 'value'
    return 'punctuation'
  }
  stream.skipToEnd()
  return 'invalid'
}

/** A header or variable value: plain text with {{variables}}. */
function value(stream: StringStream, style: string | null): string | null {
  if (variable(stream)) return 'atom'
  if (stream.match(/^[^{]+/) || stream.next()) return style
  return null
}

function body(stream: StringStream, state: HttpState): string | null {
  if (state.part === 'path') {
    stream.skipToEnd()
    return 'string'
  }
  if (stream.sol() && state.bodyLines === 1 && stream.match(/^<@?(?=\s+\S)/)) {
    state.part = 'path'
    return 'operator'
  }
  if (variable(stream)) return 'atom'
  if (stream.match(/^"(?:[^"\\]|\\.)*"?/)) return 'string'
  if (stream.match(/^-?\d+(\.\d+)?([eE][+-]?\d+)?\b/)) return 'number'
  if (stream.match(/^(true|false|null)\b/)) return 'atom'
  if (stream.match(/^[^{"\d\-tfn]+/) || stream.next()) return null
  return null
}
