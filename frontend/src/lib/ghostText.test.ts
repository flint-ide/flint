import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { Text } from '@codemirror/state'
import { acceptGhost, advanceGhost, cleanSuggestion, completionContext, shouldSuggest, type TriggerInput } from './ghostText'

const typing = (lineText: string, column = lineText.length, over: Partial<TriggerInput> = {}): TriggerInput => ({
  ranges: [{ from: 10, to: 10 }],
  userEvent: 'input.type',
  lineText,
  column,
  ...over,
})

describe('shouldSuggest', () => {
  it('asks after typing at the end of a line', () => {
    assert.equal(shouldSuggest(typing('  return ')), true)
    assert.equal(shouldSuggest(typing('', 0, { userEvent: 'input' })), true) // Enter
  })
  it('asks before closing characters', () => {
    assert.equal(shouldSuggest(typing('foo(bar)', 7)), true)
    assert.equal(shouldSuggest(typing('x = "ab";', 7)), true)
  })
  it('does not ask in the middle of a word', () => {
    assert.equal(shouldSuggest(typing('function', 4)), false)
    assert.equal(shouldSuggest(typing('a + b', 2)), false)
  })
  it('does not ask for pastes, undo or deletions', () => {
    for (const userEvent of ['input.paste', 'undo', 'delete.backward', 'input.complete', undefined]) {
      assert.equal(shouldSuggest(typing('x', 1, { userEvent })), false, String(userEvent))
    }
  })
  it('does not ask with a selection or several cursors', () => {
    assert.equal(shouldSuggest(typing('x', 1, { ranges: [{ from: 1, to: 4 }] })), false)
    assert.equal(shouldSuggest(typing('x', 1, { ranges: [{ from: 1, to: 1 }, { from: 5, to: 5 }] })), false)
  })
})

describe('completionContext', () => {
  const doc = Text.of(['a', 'b', 'cX', 'd', 'e'])
  const pos = doc.line(3).from + 1 // between c and X
  it('sends the lines around the cursor, split at the cursor', () => {
    assert.deepEqual(completionContext(doc, pos, 1), { prefix: 'b\nc', suffix: 'X\nd' })
    assert.deepEqual(completionContext(doc, pos, 10), { prefix: 'a\nb\nc', suffix: 'X\nd\ne' })
  })
  it('keeps the characters nearest the cursor when cutting', () => {
    assert.deepEqual(completionContext(doc, pos, 10, 3), { prefix: 'b\nc', suffix: 'X\nd' })
  })
})

describe('cleanSuggestion', () => {
  it('drops blank suggestions', () => {
    assert.equal(cleanSuggestion('  \n', ''), null)
  })
  it('drops what already follows the cursor', () => {
    assert.equal(cleanSuggestion('a, b)', ')'), 'a, b')
    assert.equal(cleanSuggestion(')', ')'), null)
  })
  it('keeps multi-line suggestions as they are', () => {
    assert.equal(cleanSuggestion('\n\treturn x\n}', '}'), '\n\treturn x\n}')
  })
})

describe('advanceGhost', () => {
  const g = { pos: 5, text: 'return x' }
  it('keeps the rest while the user types the suggestion', () => {
    assert.deepEqual(advanceGhost(g, 5, 're'), { pos: 7, text: 'turn x' })
  })
  it('ends when the suggestion is typed out', () => {
    assert.equal(advanceGhost(g, 5, 'return x'), null)
  })
  it('dismisses on other text or elsewhere', () => {
    assert.equal(advanceGhost(g, 5, 'x'), null)
    assert.equal(advanceGhost(g, 6, 'r'), null)
    assert.equal(advanceGhost(g, 5, ''), null)
  })
})

describe('acceptGhost', () => {
  it('inserts the text and moves the cursor after it', () => {
    assert.deepEqual(acceptGhost({ pos: 3, text: 'a\nb' }), { from: 3, insert: 'a\nb', cursor: 6 })
  })
  it('uses the document line break; a break is one position', () => {
    assert.deepEqual(acceptGhost({ pos: 3, text: 'a\nb' }, '\r\n'), { from: 3, insert: 'a\r\nb', cursor: 6 })
  })
})
