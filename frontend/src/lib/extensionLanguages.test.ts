import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { EditorState } from '@codemirror/state'
import { ensureSyntaxTree, language } from '@codemirror/language'
import { commentTokens, compileGrammar, extensionLanguageFor, setExtensionLanguages, type LanguageSpec } from './extensionLanguages'
import { languageFor } from './language'

const ruby: LanguageSpec = {
  id: 'ruby',
  label: 'Ruby',
  extensions: ['.rb', '.RAKE'],
  filenames: ['Gemfile'],
  lineComment: '#',
  blockComment: ['=begin', '=end'],
  grammar: {
    states: {
      start: [
        { regex: '#.*', token: 'comment' },
        { regex: '"', token: 'string', push: 'string' },
      ],
      string: [{ regex: '"', token: 'string', pop: true }, { regex: '[^"]+', token: 'string' }],
    },
  },
}

describe('extension languages', () => {
  afterEach(() => setExtensionLanguages([]))

  it('compiles a grammar into regular expressions', () => {
    const states = compileGrammar(ruby.grammar)
    assert.ok(states)
    assert.ok(states.start[0].regex instanceof RegExp)
    assert.equal(states.string[0].pop, true)
    assert.equal(states.start[1].push, 'string')
  })

  it('refuses grammars that would break the editor', () => {
    assert.equal(compileGrammar(null), null)
    assert.equal(compileGrammar({ states: { other: [{ regex: 'x' }] } }), null)
    assert.equal(compileGrammar({ states: { start: [{ regex: '(' }] } }), null)
    assert.equal(compileGrammar({ states: { start: [{ regex: 'a*' }] } }), null) // matches nothing: no progress
    assert.equal(compileGrammar({ states: { start: [{ regex: 'x' }], languageData: [] } }), null)
  })

  it('turns comment syntax into language data', () => {
    assert.deepEqual(commentTokens(ruby), { line: '#', block: { open: '=begin', close: '=end' } })
    assert.deepEqual(commentTokens({ id: 'x', label: 'X' }), {})
  })

  it('highlights a file with the grammar and knows its comments', async () => {
    setExtensionLanguages([ruby])
    const ext = await languageFor('/p/app.rb')
    const state = EditorState.create({ doc: '# note\nputs "hi"\n', extensions: [ext] })
    const tree = ensureSyntaxTree(state, state.doc.length, 5000)
    assert.ok(tree && tree.length === state.doc.length, 'parsed the whole document')
    const tokens: string[] = []
    tree.iterate({ enter: (n) => void (n.name !== 'Document' && tokens.push(n.name)) })
    assert.ok(tokens.includes('comment'), `tokens: ${tokens.join(',')}`)
    assert.ok(tokens.includes('string'), `tokens: ${tokens.join(',')}`)
    assert.deepEqual(state.languageDataAt('commentTokens', 1)[0], { line: '#', block: { open: '=begin', close: '=end' } })
  })

  it('leaves Flint’s own languages alone', async () => {
    setExtensionLanguages([{ id: 'evil', label: 'Evil', extensions: ['.go'], grammar: { states: { start: [{ regex: '.', token: 'error' }] } } }])
    const state = EditorState.create({ doc: 'package main\n', extensions: [await languageFor('/p/main.go')] })
    assert.notEqual(state.facet(language)?.name, 'evil')
  })

  it('finds a language by file name, then extension', () => {
    const other: LanguageSpec = { id: 'other', label: 'Other', extensions: ['.rb'] }
    setExtensionLanguages([ruby, other])
    assert.equal(extensionLanguageFor('/p/app.rb')?.id, 'ruby')
    assert.equal(extensionLanguageFor('/p/Tasks.rake')?.id, 'ruby')
    assert.equal(extensionLanguageFor('/p/Gemfile')?.id, 'ruby')
    assert.equal(extensionLanguageFor('/p/main.go'), undefined)
    assert.equal(extensionLanguageFor('/p/.rb'), undefined)
  })
})
