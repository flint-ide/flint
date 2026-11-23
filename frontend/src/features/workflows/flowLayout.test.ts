import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { centredScroll, flowLayout, LINK_WIDTH, NODE_MIN, rowWidth, scrollEdges, stepForKey } from './flowLayout'

describe('flowLayout', () => {
  it('keeps one row without scrolling while every step fits at its narrowest', () => {
    for (let n = 2; n <= 8; n++) assert.deepEqual(flowLayout(rowWidth(n), n), { vertical: false, overflow: false })
    assert.deepEqual(flowLayout(5000, 3), { vertical: false, overflow: false })
  })

  it('never wraps: more steps than fit stay one row that scrolls', () => {
    // An 8-step run in a 900px window (about 560px of tab) and in a 1400px one: one row, scrolling.
    assert.deepEqual(flowLayout(560, 8), { vertical: false, overflow: true })
    assert.deepEqual(flowLayout(1060, 8), { vertical: false, overflow: true })
    assert.deepEqual(flowLayout(rowWidth(8) - 1, 8), { vertical: false, overflow: true })
  })

  it('the Run Workflow dialog (about 588px inside) keeps a row: two blocks fit', () => {
    assert.ok(588 >= rowWidth(2))
    assert.deepEqual(flowLayout(588, 2), { vertical: false, overflow: false })
    assert.deepEqual(flowLayout(588, 5), { vertical: false, overflow: true })
  })

  it('becomes a vertical stepper only when not even two blocks fit side by side', () => {
    assert.equal(rowWidth(2), 2 * NODE_MIN + LINK_WIDTH)
    assert.deepEqual(flowLayout(rowWidth(2) - 1, 2), { vertical: true, overflow: false })
    assert.deepEqual(flowLayout(300, 8), { vertical: true, overflow: false })
  })

  it('draws a row before it is measured, and a single step never stacks', () => {
    assert.deepEqual(flowLayout(0, 5), { vertical: false, overflow: false })
    assert.deepEqual(flowLayout(300, 1), { vertical: false, overflow: false })
    assert.deepEqual(flowLayout(120, 1), { vertical: false, overflow: true })
    assert.deepEqual(flowLayout(800, 0), { vertical: false, overflow: false })
  })
})

describe('stepForKey', () => {
  it('moves one step with the arrows in either layout, and jumps with Home and End', () => {
    assert.equal(stepForKey('ArrowRight', 2, 8), 3)
    assert.equal(stepForKey('ArrowDown', 2, 8), 3)
    assert.equal(stepForKey('ArrowLeft', 2, 8), 1)
    assert.equal(stepForKey('ArrowUp', 2, 8), 1)
    assert.equal(stepForKey('Home', 5, 8), 0)
    assert.equal(stepForKey('End', 1, 8), 7)
  })

  it('does not wrap around the ends and ignores other keys', () => {
    assert.equal(stepForKey('ArrowLeft', 0, 8), null)
    assert.equal(stepForKey('ArrowRight', 7, 8), null)
    assert.equal(stepForKey('Home', 0, 8), null)
    assert.equal(stepForKey('Enter', 3, 8), null)
    assert.equal(stepForKey('ArrowRight', 0, 0), null)
  })
})

describe('centredScroll', () => {
  it('centres the block and clamps to the scrollable range', () => {
    assert.equal(centredScroll(1000, 200, 600, 2000), 800)
    assert.equal(centredScroll(50, 200, 600, 2000), 0)
    assert.equal(centredScroll(1900, 200, 600, 2000), 1400)
    assert.equal(centredScroll(100, 200, 600, 500), 0)
  })
})

describe('scrollEdges', () => {
  it('tells which ends hide blocks', () => {
    assert.deepEqual(scrollEdges(0, 600, 2000), { before: false, after: true })
    assert.deepEqual(scrollEdges(700, 600, 2000), { before: true, after: true })
    assert.deepEqual(scrollEdges(1400, 600, 2000), { before: true, after: false })
    assert.deepEqual(scrollEdges(0, 600, 600), { before: false, after: false })
  })
})
