import type { StreamParser } from '@codemirror/language'

// Highlighting for go.mod / go.work / go.sum (no grammar ships with CodeMirror).

const DIRECTIVES = new Set(['module', 'go', 'toolchain', 'require', 'replace', 'exclude', 'retract', 'use', 'godebug', 'tool', 'ignore'])

interface State {
  inBlock: boolean
}

export const goMod: StreamParser<State> = {
  name: 'gomod',
  startState: () => ({ inBlock: false }),
  token(stream, state) {
    if (stream.eatSpace()) return null
    if (stream.match('//')) {
      stream.skipToEnd()
      return 'comment'
    }
    if (stream.match('(')) {
      state.inBlock = true
      return 'bracket'
    }
    if (stream.match(')')) {
      state.inBlock = false
      return 'bracket'
    }
    if (stream.match('=>')) return 'operator'
    if (stream.match(/^"(?:[^"\\]|\\.)*"/) || stream.match(/^`[^`]*`/)) return 'string'
    // Versions: v1.2.3, v0.0.0-2024…-abcdef, 1.22, h1:base64= (go.sum hashes).
    if (stream.match(/^v\d+\.\d+\.\d+[\w.+-]*/) || stream.match(/^\d+\.\d+(\.\d+)?\b/)) return 'number'
    if (stream.match(/^h1:[A-Za-z0-9+/=]+/)) return 'string'
    const word = stream.match(/^[^\s()"]+/) as RegExpMatchArray | null
    if (!word) {
      stream.next()
      return null
    }
    // A directive starts the line (outside of blocks); module paths are everything else.
    if (DIRECTIVES.has(word[0]) && stream.column() === 0) return 'keyword'
    return state.inBlock || stream.column() > 0 ? 'variableName.special' : 'variableName'
  },
  languageData: { commentTokens: { line: '//' } },
}
