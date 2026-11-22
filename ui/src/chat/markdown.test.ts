import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { codeText, fenceParts, inlineTokens, parseLine } from './markdown'

describe('fenceParts', () => {
  it('puts code at odd indexes and drops the fence lines', () => {
    assert.deepEqual(fenceParts('a\n```ts\nx = 1\n```\nb'), ['a\n', 'x = 1\n', 'b'])
  })
  it('keeps an unclosed fence as code to the end (a streaming reply)', () => {
    const parts = fenceParts('look:\n```\npartial')
    assert.equal(parts.length, 2)
    assert.equal(parts[1], 'partial')
  })
  it('returns the text alone when there is no fence', () => {
    assert.deepEqual(fenceParts('plain'), ['plain'])
  })
})

describe('codeText', () => {
  it('drops one trailing newline only', () => {
    assert.equal(codeText('a\n\n'), 'a\n')
    assert.equal(codeText('a'), 'a')
  })
})

describe('parseLine', () => {
  it('turns blank lines into gaps except at the edges', () => {
    assert.deepEqual(parseLine('  ', false), { kind: 'gap' })
    assert.equal(parseLine('', true), null)
  })
  it('reads headings up to four hashes', () => {
    assert.deepEqual(parseLine('## Title', false), { kind: 'heading', text: 'Title' })
    assert.deepEqual(parseLine('##### five', false), { kind: 'text', text: '##### five' })
  })
  it('reads dash, star and numbered bullets', () => {
    assert.deepEqual(parseLine('- item', false), { kind: 'bullet', marker: '•', text: 'item' })
    assert.deepEqual(parseLine('  * item', false), { kind: 'bullet', marker: '•', text: 'item' })
    assert.deepEqual(parseLine('3. third', false), { kind: 'bullet', marker: '3.', text: 'third' })
  })
  it('keeps anything else as text', () => {
    assert.deepEqual(parseLine('hello', true), { kind: 'text', text: 'hello' })
  })
  it('stays linear on long runs of spaces (no ReDoS)', () => {
    const spaces = ' '.repeat(50_000)
    const started = Date.now()
    assert.equal(parseLine(`* ${spaces}x`, false)?.kind, 'bullet')
    assert.equal(parseLine(`# ${spaces}`, false)?.kind, 'heading')
    assert.deepEqual(parseLine(`*${spaces}`, false), { kind: 'bullet', marker: '•', text: '' })
    assert.ok(Date.now() - started < 200)
  })
})

describe('inlineTokens', () => {
  it('splits code and bold spans', () => {
    assert.deepEqual(inlineTokens('run `npm test` **now**'), [
      { kind: 'text', text: 'run ' },
      { kind: 'code', text: 'npm test' },
      { kind: 'text', text: ' ' },
      { kind: 'bold', text: 'now' },
      { kind: 'text', text: '' },
    ])
  })
  it('leaves unmatched markers as text', () => {
    assert.deepEqual(inlineTokens('a ` b ** c'), [{ kind: 'text', text: 'a ` b ** c' }])
  })
  it('never produces markup from HTML in the text', () => {
    assert.deepEqual(inlineTokens('<b>x</b>'), [{ kind: 'text', text: '<b>x</b>' }])
  })
})
