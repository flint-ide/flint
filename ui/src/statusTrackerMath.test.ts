import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { rovingIndex, trackerSlots, trackerSummary, type TrackerItem } from './statusTrackerMath'

const item = (id: string, state: TrackerItem['state'] = 'ok'): TrackerItem => ({ id, state })

describe('trackerSlots', () => {
  it('pads the left with placeholders up to max', () => {
    const slots = trackerSlots([item('a'), item('b', 'failed')], 4)
    assert.deepEqual(
      slots.map((s) => s.state),
      ['none', 'none', 'ok', 'failed'],
    )
    assert.deepEqual(
      slots.map((s) => s.placeholder),
      [true, true, false, false],
    )
  })
  it('gives placeholders unique ids', () => {
    const ids = trackerSlots([], 3).map((s) => s.id)
    assert.equal(new Set(ids).size, 3)
  })
  it('keeps the newest items when there are more than max', () => {
    const slots = trackerSlots([item('a'), item('b'), item('c')], 2)
    assert.deepEqual(
      slots.map((s) => s.id),
      ['b', 'c'],
    )
  })
  it('returns exactly max slots for an exact fit', () => {
    assert.equal(trackerSlots([item('a'), item('b')], 2).length, 2)
  })
  it('returns nothing for a max of zero or less', () => {
    assert.deepEqual(trackerSlots([item('a')], 0), [])
    assert.deepEqual(trackerSlots([item('a')], -3), [])
  })
  it('rounds a fractional max down', () => {
    assert.equal(trackerSlots([], 2.9).length, 2)
  })
  it('carries the title through', () => {
    const [slot] = trackerSlots([{ id: 'a', state: 'ok', title: 'Run #1' }], 1)
    assert.equal(slot.title, 'Run #1')
  })
  it('does not mutate its input', () => {
    const items = Object.freeze([item('a')])
    assert.equal(trackerSlots(items, 3).length, 3)
  })
})

describe('trackerSummary', () => {
  it('counts each state, in a fixed order, skipping zeros', () => {
    const items = [
      ...Array.from({ length: 27 }, (_, i) => item(`ok${i}`)),
      item('f1', 'failed'),
      item('f2', 'failed'),
      item('r', 'running'),
    ]
    assert.equal(trackerSummary(items, 30), 'Last 30 runs: 27 passed, 2 failed, 1 running')
  })
  it('names warnings and skipped runs', () => {
    assert.equal(trackerSummary([item('a', 'warn'), item('b', 'skipped')], 30), 'Last 2 runs: 1 with warnings, 1 skipped')
  })
  it('says no runs for an empty list', () => {
    assert.equal(trackerSummary([], 30), 'No runs')
  })
  it('uses the singular for one run', () => {
    assert.equal(trackerSummary([item('a')], 30), 'Last 1 run: 1 passed')
  })
  it('counts only the runs that are shown', () => {
    assert.equal(trackerSummary([item('a', 'failed'), item('b'), item('c')], 2), 'Last 2 runs: 2 passed')
  })
  it('leaves runs without a result out of the counts', () => {
    assert.equal(trackerSummary([item('a', 'none'), item('b')], 5), 'Last 2 runs: 1 passed')
  })
})

describe('odd max values', () => {
  const three = [item('a'), item('b', 'failed'), item('c')]
  it('shows nothing for a max of 0, negative or NaN', () => {
    for (const max of [0, -1, Number.NaN]) {
      assert.deepEqual(trackerSlots(three, max), [])
      assert.equal(trackerSummary(three, max), 'No runs')
    }
  })
  it('shows every item for an infinite max, without padding', () => {
    assert.deepEqual(
      trackerSlots(three, Number.POSITIVE_INFINITY).map((s) => s.id),
      ['a', 'b', 'c'],
    )
    assert.equal(trackerSummary(three, Number.POSITIVE_INFINITY), 'Last 3 runs: 2 passed, 1 failed')
  })
})

describe('rovingIndex', () => {
  it('moves with the arrows and clamps at the ends', () => {
    assert.equal(rovingIndex(1, 'ArrowRight', 3), 2)
    assert.equal(rovingIndex(2, 'ArrowRight', 3), 2)
    assert.equal(rovingIndex(1, 'ArrowLeft', 3), 0)
    assert.equal(rovingIndex(0, 'ArrowLeft', 3), 0)
  })
  it('jumps with Home and End', () => {
    assert.equal(rovingIndex(1, 'Home', 5), 0)
    assert.equal(rovingIndex(1, 'End', 5), 4)
  })
  it('ignores other keys and empty lists', () => {
    assert.equal(rovingIndex(1, 'a', 3), null)
    assert.equal(rovingIndex(0, 'ArrowRight', 0), null)
  })
})
