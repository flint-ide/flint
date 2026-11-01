import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { editorContext, MAX_FILES, MAX_SELECTION_CHARS, sameContext, selectionInfo, type DocLike } from './editorContext'
import { coerceMcpServer, DEFAULT_MCP_SERVER, sharesEditor } from './mcpSettings'
import { coerceSettings } from './settings'

/** A document of lines joined by \n. */
function doc(text: string): DocLike {
  const starts = [0]
  for (let i = 0; i < text.length; i++) if (text[i] === '\n') starts.push(i + 1)
  return {
    lineAt(pos) {
      let n = 0
      while (n + 1 < starts.length && starts[n + 1] <= pos) n++
      return { number: n + 1, from: starts[n] }
    },
    sliceString: (from, to) => text.slice(from, to),
  }
}

describe('selectionInfo', () => {
  const d = doc('one\ntwo\nthree')
  it('gives 1-based lines and columns and the text', () => {
    assert.deepEqual(selectionInfo(d, 5, 10), { startLine: 2, startColumn: 2, endLine: 3, endColumn: 3, text: 'wo\nth' })
  })
  it('treats a backwards selection like a forward one', () => {
    assert.deepEqual(selectionInfo(d, 10, 5), selectionInfo(d, 5, 10))
  })
  it('reports the cursor with empty text', () => {
    assert.deepEqual(selectionInfo(d, 4, 4), { startLine: 2, startColumn: 1, endLine: 2, endColumn: 1, text: '' })
  })
  it('bounds the text', () => {
    const big = doc('x'.repeat(MAX_SELECTION_CHARS * 2))
    assert.equal(selectionInfo(big, 0, MAX_SELECTION_CHARS * 2).text.length, MAX_SELECTION_CHARS)
  })
})

describe('editorContext', () => {
  const tabs = [
    { id: 'a', kind: 'file', path: '/p/a.go', dirty: true },
    { id: 't', kind: 'terminal', dirty: false },
    { id: 'b', kind: 'file', path: '/p/b.go', dirty: false },
  ]
  const sel = { startLine: 1, startColumn: 1, endLine: 1, endColumn: 1, text: '' }

  it('lists file tabs and the active file with its selection', () => {
    const ctx = editorContext(tabs, 'b', (id) => (id === 'b' ? sel : null))
    assert.deepEqual(ctx, { files: [{ path: '/p/a.go', dirty: true }, { path: '/p/b.go', dirty: false }], active: '/p/b.go', selection: sel })
  })
  it('has no active file when another kind of tab is focused', () => {
    const onTerminal = editorContext(tabs, 't', () => sel)
    assert.equal(onTerminal.active, '')
    assert.equal(onTerminal.selection, null)
    assert.equal(onTerminal.files.length, 2)
    assert.equal(editorContext(tabs, null, () => sel).active, '')
  })
  it('bounds the file list', () => {
    const many = Array.from({ length: MAX_FILES + 3 }, (_, i) => ({ id: String(i), kind: 'file', path: `/p/${i}`, dirty: false }))
    assert.equal(editorContext(many, null, () => null).files.length, MAX_FILES)
  })
  it('compares snapshots by value', () => {
    const one = editorContext(tabs, 'a', () => sel)
    assert.equal(sameContext(null, one), false)
    assert.equal(sameContext(one, editorContext(tabs, 'a', () => ({ ...sel }))), true)
    assert.equal(sameContext(one, editorContext(tabs, 'a', () => ({ ...sel, startLine: 2 }))), false)
  })
})

describe('MCP server settings', () => {
  it('defaults to on, with terminal output and cluster status opt-in', () => {
    assert.deepEqual(coerceSettings({}).mcpServer, DEFAULT_MCP_SERVER)
    assert.equal(sharesEditor(DEFAULT_MCP_SERVER), true)
    assert.deepEqual(DEFAULT_MCP_SERVER.tools, { diagnostics: true, editor: true, terminals: false, tests: true, kubernetes: false })
  })
  it('keeps valid values and replaces invalid ones', () => {
    const m = coerceMcpServer({ enabled: false, tools: { kubernetes: false, editor: 'no' }, terminalLines: 99999 })
    assert.equal(m.enabled, false)
    assert.equal(m.tools.kubernetes, false)
    assert.equal(m.tools.editor, true)
    assert.equal(m.terminalLines, 2000)
    assert.equal(coerceMcpServer({ terminalLines: 'x' }).terminalLines, 200)
    assert.equal(sharesEditor(m), false)
    assert.equal(sharesEditor({ ...DEFAULT_MCP_SERVER, tools: { ...DEFAULT_MCP_SERVER.tools, editor: false } }), false)
  })
})
