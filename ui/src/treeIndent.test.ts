import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { classNames, treeIndent, twistyGlyph } from './treeIndent'

describe('treeIndent', () => {
  it('matches the file tree: 8 px plus 14 per level', () => {
    assert.equal(treeIndent(), 8)
    assert.equal(treeIndent(0), 8)
    assert.equal(treeIndent(1), 22)
    assert.equal(treeIndent(2), 36)
  })
  it('lets an explicit indent win, never below 0', () => {
    assert.equal(treeIndent(3, 54), 54)
    assert.equal(treeIndent(1, -4), 0)
  })
  it('clamps odd depths', () => {
    assert.equal(treeIndent(-2), 8)
    assert.equal(treeIndent(1.7), 22)
    assert.equal(treeIndent(Number.NaN), 8)
    assert.equal(treeIndent(2, Number.NaN), 36)
  })
})

describe('twistyGlyph', () => {
  it('draws the open and closed triangles and nothing for a leaf', () => {
    assert.equal(twistyGlyph('open'), '▾')
    assert.equal(twistyGlyph('closed'), '▸')
    assert.equal(twistyGlyph('none'), '')
  })
})

describe('classNames', () => {
  it('joins only the parts that are set', () => {
    assert.equal(classNames('a', false, undefined, 'b', null, ''), 'a b')
    assert.equal(classNames(), '')
  })
})
