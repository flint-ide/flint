import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { Text } from '@codemirror/state'
import { MAX_DECORATIONS, MAX_EDITS, coerceDecorations, decorationRange, editChanges, isLineLevel, positionOf, publishIsCurrent, type TextEdit } from './decorationModel'

// Lines: 1 "alpha" 0..5, 2 "" 6..6, 3 "gamma delta" 7..18
const doc = Text.of(['alpha', '', 'gamma delta'])

describe('coerceDecorations', () => {
  it('keeps known kinds with valid values and drops the rest', () => {
    const got = coerceDecorations([
      { kind: 'diagnostic', id: 'd1', line: 2, from: 1, to: 4, severity: 'warning', message: 'shadowed', actions: [{ id: 'fix', label: 'Fix' }, { id: '', label: 'x' }, 'nope'] },
      { kind: 'gutter', line: 3, icon: 'check', severity: 'ok', hover: 'Covered' },
      { kind: 'gutter', line: 4, icon: 'constructor', severity: 'loud' },
      { kind: 'inline', line: 5, text: 'ABC-1\n· open', command: { id: 'acme.open', args: { key: 'ABC-1' } } },
      { kind: 'highlight', line: 6, endLine: 2, from: -3, to: -9 },
      { kind: 'diagnostic', line: 1 }, // no message
      { kind: 'inline', line: 1, text: '  ' }, // no text
      { kind: 'script', line: 1 },
      { kind: 'gutter', line: 0 },
      { kind: 'gutter', line: 'two' },
      null,
      'gutter',
    ])
    assert.deepEqual(got, [
      { kind: 'diagnostic', id: 'd1', line: 2, endLine: 2, from: 1, to: 4, severity: 'warning', message: 'shadowed', actions: [{ id: 'fix', label: 'Fix' }] },
      { kind: 'gutter', line: 3, endLine: 3, from: 0, to: -1, severity: 'ok', message: 'Covered', icon: 'check' },
      { kind: 'gutter', line: 4, endLine: 4, from: 0, to: -1, severity: 'info' },
      { kind: 'inline', line: 5, endLine: 5, from: 0, to: -1, severity: 'info', text: 'ABC-1 · open', command: { id: 'acme.open', args: { key: 'ABC-1' } } },
      { kind: 'highlight', line: 6, endLine: 6, from: 0, to: -1, severity: 'info' },
    ])
    assert.deepEqual(coerceDecorations({ kind: 'gutter' }), [])
    assert.deepEqual(coerceDecorations(undefined), [])
  })

  it('bounds counts and lengths', () => {
    const many = Array.from({ length: MAX_DECORATIONS + 10 }, (_, i) => ({ kind: 'gutter', line: i + 1 }))
    assert.equal(coerceDecorations(many).length, MAX_DECORATIONS)
    const [d] = coerceDecorations([
      { kind: 'inline', line: 1, text: 'y'.repeat(500), message: 'm'.repeat(5000), actions: Array.from({ length: 30 }, (_, i) => ({ id: `a${i}`, label: 'L'.repeat(100) })) },
    ])
    assert.equal(d.text?.length, 200)
    assert.equal(d.message?.length, 2000)
    assert.equal(d.actions?.length, 10)
    assert.equal(d.actions?.[0].label.length, 60)
  })
})

describe('decorationRange', () => {
  const range = (line: number, from = 0, to = -1, endLine = line) => decorationRange(doc, { line, endLine, from, to })

  it('turns lines and columns into offsets', () => {
    assert.deepEqual(range(1, 1, 3), { from: 1, to: 3 })
    assert.deepEqual(range(3, 6, 11), { from: 13, to: 18 })
    assert.deepEqual(range(1), { from: 0, to: 5 }, 'no columns: the whole line')
    assert.deepEqual(range(1, 0, -1, 3), { from: 0, to: 18 }, 'whole lines 1 to 3')
    assert.deepEqual(range(1, 2, 3, 3), { from: 2, to: 10 }, 'a range over several lines')
    assert.deepEqual(range(2), { from: 6, to: 6 }, 'an empty line')
  })

  it('clamps what lies outside the document', () => {
    assert.deepEqual(range(1, 3, 99), { from: 3, to: 5 }, 'a column past the line end')
    assert.deepEqual(range(1, 99, 120), { from: 5, to: 5 })
    assert.deepEqual(range(9, 2, 4), { from: 9, to: 11 }, 'a line past the end lands on the last line')
    assert.deepEqual(range(2, 0, -1, 40), { from: 6, to: 18 }, 'an end line past the end')
    assert.deepEqual(range(3, 4, 2), { from: 11, to: 11 }, 'an end before the start is empty, not negative')
    assert.deepEqual(range(-4, -2, 1), { from: 0, to: 1 })
  })

  it('tells line-level decorations from ranges', () => {
    assert.equal(isLineLevel({ from: 0, to: -1 }), true)
    assert.equal(isLineLevel({ from: 0, to: 4 }), false)
    assert.equal(isLineLevel({ from: 2, to: -1 }), false)
  })

  it('names offsets as a line and columns', () => {
    assert.deepEqual(positionOf(doc, 13, 18), { line: 3, from: 6, endLine: 3, to: 11 })
    assert.deepEqual(positionOf(doc, 2, 10), { line: 1, from: 2, endLine: 3, to: 3 })
  })
})

describe('publishIsCurrent', () => {
  it('draws only what was made for the text in the editor', () => {
    assert.equal(publishIsCurrent(7, 7), true)
    assert.equal(publishIsCurrent(0, 7), true, 'no version given')
    assert.equal(publishIsCurrent(6, 7), false, 'stale')
    assert.equal(publishIsCurrent(8, 7), false, 'a version the editor never had')
  })
})

describe('editChanges', () => {
  const edit = (line: number, from: number, to: number, text: string, endLine = line): TextEdit => ({ line, from, endLine, to, text })

  it('turns edits into sorted changes', () => {
    assert.deepEqual(editChanges(doc, [edit(3, 6, 11, 'DELTA'), edit(1, 0, 0, '// '), edit(1, 5, 0, '\r\nbeta', 2)]), [
      { from: 0, to: 0, insert: '// ' },
      { from: 5, to: 6, insert: '\nbeta' },
      { from: 13, to: 18, insert: 'DELTA' },
    ])
    assert.deepEqual(editChanges(doc, []), [])
    // endLine 0 (left out) is the same line; two inserts at one place are fine.
    assert.deepEqual(editChanges(doc, [{ line: 2, from: 0, endLine: 0, to: 0, text: 'a' }, edit(2, 0, 0, 'b')]), [
      { from: 6, to: 6, insert: 'a' },
      { from: 6, to: 6, insert: 'b' },
    ])
  })

  it('refuses edits made for another text instead of clamping them', () => {
    for (const [name, edits, want] of [
      ['a line past the end', [edit(4, 0, 0, 'x')], 'outside'],
      ['line 0', [edit(0, 0, 0, 'x')], 'outside'],
      ['a column past the line end', [edit(1, 2, 9, 'x')], 'outside'],
      ['a start past the line end', [edit(2, 1, 1, 'x')], 'outside'],
      ['an end before the start', [edit(1, 4, 2, 'x')], 'outside'],
      ['an end line before the line', [edit(3, 0, 0, 'x', 1)], 'outside'],
      ['overlapping edits', [edit(1, 0, 3, 'x'), edit(1, 2, 4, 'y')], 'overlap'],
      ['a fraction', [edit(1.5, 0, 1, 'x')], 'no position'],
      ['no text', [{ line: 1, from: 0, endLine: 1, to: 1 } as unknown as TextEdit], 'no text'],
      ['too many', Array.from({ length: MAX_EDITS + 1 }, () => edit(1, 0, 0, 'x')), 'more than'],
    ] as const) {
      const got = editChanges(doc, edits)
      assert.equal(typeof got, 'string', name)
      assert.match(got as string, new RegExp(want), name)
    }
  })
})
