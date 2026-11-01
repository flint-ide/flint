import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { ChangeSet, Text } from '@codemirror/state'
import {
  MAX_SELECTION_CHARS,
  buildEditRequest,
  canApply,
  editContext,
  editRange,
  fitReplacement,
  languageOf,
  mapPending,
  type Pending,
} from './inlineEdit'

const doc = (s: string) => Text.of(s.split('\n'))
const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n')

describe('editRange', () => {
  const d = doc('a\n  bb\nccc')
  it('uses the selection, in document order', () => {
    assert.deepEqual(editRange(d, { from: 6, to: 3 }), { from: 3, to: 6 })
  })
  it('takes the cursor line without its line break when nothing is selected', () => {
    assert.deepEqual(editRange(d, { from: 4, to: 4 }), { from: 2, to: 6 })
    assert.equal(d.sliceString(2, 6), '  bb')
  })
})

describe('editContext', () => {
  const d = doc(lines(100))
  const fifty = d.line(50)
  it('sends the given number of lines on each side', () => {
    const { before, after } = editContext(d, { from: fifty.from, to: fifty.to }, 2)
    assert.equal(before, 'line 48\nline 49\n')
    assert.equal(after, '\nline 51\nline 52')
  })
  it('stops at the document edges', () => {
    const first = d.line(1)
    const { before, after } = editContext(d, { from: first.from, to: first.to }, 3)
    assert.equal(before, '')
    assert.equal(after, '\nline 2\nline 3\nline 4')
  })
  it('sends nothing with zero lines', () => {
    assert.deepEqual(editContext(d, { from: fifty.from, to: fifty.to }, 0), { before: '', after: '' })
  })
  it('cuts long context at a line boundary, keeping the nearest lines', () => {
    const { before, after } = editContext(d, { from: fifty.from, to: fifty.to }, 40, 20)
    assert.equal(before, 'line 48\nline 49\n')
    assert.equal(after, '\nline 51\nline 52\n')
    assert.ok(before.length <= 20 && after.length <= 20)
  })
})

describe('buildEditRequest', () => {
  const d = doc('package x\n\nfunc f() {\n\tdo()\n}')
  const line = d.line(4)
  const pending: Pending = { from: line.from, to: line.to, original: '\tdo()' }
  it('carries the path, language, instruction, selection and context', () => {
    const req = buildEditRequest('/p/x.go', d, pending, '  add error handling ', 1)
    assert.deepEqual(req, {
      path: '/p/x.go', language: 'go', instruction: 'add error handling',
      before: 'func f() {\n', selection: '\tdo()', after: '\n}',
    })
  })
  it('refuses an empty instruction and an oversized selection', () => {
    assert.ok('error' in buildEditRequest('/p/x.go', d, pending, '  ', 1))
    const big = { ...pending, original: 'x'.repeat(MAX_SELECTION_CHARS + 1) }
    assert.ok('error' in buildEditRequest('/p/x.go', d, big, 'shorten', 1))
  })
  it('names the language by extension or file name', () => {
    assert.equal(languageOf('/a/b/main.TS'), 'ts')
    assert.equal(languageOf('/a/Dockerfile'), 'Dockerfile')
    assert.equal(languageOf('/a/.env'), '.env')
  })
})

describe('apply only if unchanged', () => {
  const text = 'one\ntwo\nthree'
  const d = doc(text)
  const p: Pending = { from: 4, to: 7, original: 'two' }

  it('applies when nothing changed', () => {
    assert.equal(canApply(d, p), true)
  })

  it('follows edits elsewhere and still applies', () => {
    const changes = ChangeSet.of([{ from: 0, insert: '// head\n' }, { from: 13, insert: '!' }], d.length)
    const next = changes.apply(d)
    const mapped = mapPending(p, changes)
    assert.deepEqual([mapped.from, mapped.to], [12, 15])
    assert.equal(canApply(next, mapped), true)
  })

  it('keeps text typed right at the edges outside the range', () => {
    const changes = ChangeSet.of([{ from: 4, insert: '>' }, { from: 7, insert: '<' }], d.length)
    const next = changes.apply(d)
    const mapped = mapPending(p, changes)
    assert.equal(next.sliceString(mapped.from, mapped.to), 'two')
    assert.equal(canApply(next, mapped), true)
  })

  it('refuses when the range itself was edited', () => {
    const changes = ChangeSet.of([{ from: 5, to: 6, insert: 'W' }], d.length)
    assert.equal(canApply(changes.apply(d), mapPending(p, changes)), false)
  })

  it('refuses when the range was deleted', () => {
    const changes = ChangeSet.of([{ from: 3, to: 8 }], d.length)
    assert.equal(canApply(changes.apply(d), mapPending(p, changes)), false)
  })

  it('keeps the same object when nothing moved', () => {
    const changes = ChangeSet.of([{ from: 13, insert: '!' }], d.length)
    assert.equal(mapPending(p, changes), p)
  })
})

describe('fitReplacement', () => {
  it('keeps the trailing line break of whole-line selections', () => {
    assert.equal(fitReplacement('\tdo()\n', '\tif err := do(); err != nil {\n\t\treturn err\n\t}'), '\tif err := do(); err != nil {\n\t\treturn err\n\t}\n')
  })
  it('indents an answer that came back without the original indentation', () => {
    assert.equal(fitReplacement('\tdo()', 'if err := do(); err != nil {\n\treturn err\n\n}'), '\tif err := do(); err != nil {\n\t\treturn err\n\n\t}')
    assert.equal(fitReplacement('\tdo()', '\tdo2()'), '\tdo2()')
    assert.equal(fitReplacement('do()', 'do2()'), 'do2()')
  })
  it('drops a trailing line break the original line did not have', () => {
    assert.equal(fitReplacement('x', 'y\n'), 'y')
  })
  it('normalizes CRLF', () => {
    assert.equal(fitReplacement('a\nb', 'c\r\nd'), 'c\nd')
  })
  it('leaves an empty answer empty', () => {
    assert.equal(fitReplacement('x\n', ''), '')
  })
})
