import { it as test } from 'node:test'
import assert from 'node:assert/strict'
import { Text } from '@codemirror/state'
import { editSummary, editsToChanges, filterSymbols, fromLsp, signatureParts, sortCodeActions, symbolKindName, toLsp } from './lspText'

const doc = Text.of(['func main() {', '\tprintln("hi")', '}'])
const range = (l1: number, c1: number, l2: number, c2: number) => ({ start: { line: l1, character: c1 }, end: { line: l2, character: c2 } })

test('positions round-trip and clamp', () => {
  assert.deepEqual(toLsp(doc, 15), { line: 1, character: 1 })
  assert.equal(fromLsp(doc, { line: 1, character: 1 }), 15)
  assert.equal(fromLsp(doc, { line: 0, character: 999 }), 13) // end of the line, not the next one
  assert.equal(fromLsp(doc, { line: 99, character: 0 }), doc.length)
  assert.equal(fromLsp(doc, { line: 0, character: -4 }), 0)
})

test('edits become changes against the current document', () => {
  const changes = editsToChanges(doc, [
    { range: range(1, 1, 1, 8), newText: 'print' },
    { range: range(0, 5, 0, 9), newText: 'start' },
  ])
  assert.deepEqual(changes, [
    { from: 15, to: 22, insert: 'print' },
    { from: 5, to: 9, insert: 'start' },
  ])
})

test('edits that change nothing are dropped and CRLF becomes LF', () => {
  const whole = range(0, 0, 3, 0)
  assert.deepEqual(editsToChanges(doc, [{ range: whole, newText: 'func main() {\r\n\tprintln("hi")\r\n}' }]), [])
  assert.deepEqual(editsToChanges(doc, [{ range: range(2, 1, 2, 1), newText: '\r\n' }]), [{ from: doc.length, to: doc.length, insert: '\n' }])
})

test('a reversed range does not produce a negative span', () => {
  assert.deepEqual(editsToChanges(doc, [{ range: range(1, 5, 1, 2), newText: 'x' }]), [{ from: 19, to: 19, insert: 'x' }])
})

test('edit summary counts distinct files', () => {
  assert.equal(editSummary([{ path: 'a', edits: [1] }]), '1 edit in 1 file')
  assert.equal(editSummary([{ path: 'a', edits: [1, 2] }, { path: 'b', edits: [1] }, { path: 'a', edits: [1] }]), '4 edits in 2 files')
})

test('symbol kinds have names', () => {
  assert.equal(symbolKindName(12), 'function')
  assert.equal(symbolKindName(999), 'symbol')
})

test('outline filter keeps document order', () => {
  const symbols = [
    { name: 'Server', container: '', depth: 0 },
    { name: 'serve', container: 'Server', depth: 1 },
    { name: 'main', container: '', depth: 0 },
    { name: 'parseArgs', container: '', depth: 0 },
  ]
  assert.deepEqual(filterSymbols(symbols, 'srv').map((s) => s.name), ['Server', 'serve'])
  assert.deepEqual(filterSymbols(symbols, 'PA').map((s) => s.name), ['parseArgs'])
  assert.equal(filterSymbols(symbols, '  ').length, 4)
  assert.equal(filterSymbols(symbols, 'zz').length, 0)
})

test('signature is split around the active parameter', () => {
  const params = [{ start: 2, end: 7 }, { start: 9, end: 17 }]
  assert.deepEqual(signatureParts('f(a int, b string)', params, 1), { before: 'f(a int, ', active: 'b string', after: ')' })
  assert.deepEqual(signatureParts('f(a int, b string)', params, 5), { before: 'f(a int, b string)', active: '', after: '' })
  assert.deepEqual(signatureParts('f()', [{ start: 0, end: 0 }], 0), { before: 'f()', active: '', after: '' })
  assert.deepEqual(signatureParts('f(a)', [{ start: 2, end: 40 }], 0), { before: 'f(a)', active: '', after: '' })
})

test('code actions: preferred, quick fixes, refactorings, source', () => {
  const actions = [
    { title: 'Organize', kind: 'source.organizeImports', preferred: false },
    { title: 'Extract', kind: 'refactor.extract', preferred: false },
    { title: 'Fix', kind: 'quickfix', preferred: false },
    { title: 'Other', kind: '', preferred: false },
    { title: 'Best', kind: 'quickfix', preferred: true },
  ]
  assert.deepEqual(sortCodeActions(actions).map((a) => a.title), ['Best', 'Fix', 'Extract', 'Other', 'Organize'])
})
