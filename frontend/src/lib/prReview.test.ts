import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  checksSummary,
  countChecks,
  foldsClearOf,
  isCommentable,
  lineOnOtherSide,
  placeThreads,
  rollup,
  stateBadge,
  threadCounts,
  timeAgo,
  type LineHunk,
} from './prReview'

const hunk = (fromA: number, toA: number, fromB: number, toB: number): LineHunk => ({ fromA, toA, fromB, toB })
const thread = (id: string, line: number, side = 'RIGHT', fileLevel = false) => ({ id, line, side, fileLevel })

describe('check rollup', () => {
  const counts = (...states: string[]) => countChecks(states.map((state) => ({ state })))

  it('fails on any failure, even with checks still running', () => {
    assert.equal(rollup(counts('success', 'pending', 'failure')), 'failure')
  })

  it('is pending while something runs', () => {
    assert.equal(rollup(counts('success', 'pending', 'skipped')), 'pending')
  })

  it('passes with skipped and neutral checks', () => {
    assert.equal(rollup(counts('success', 'skipped', 'neutral')), 'success')
    assert.equal(rollup(counts('skipped')), 'success')
  })

  it('is none without checks', () => {
    assert.equal(rollup(counts()), 'none')
  })

  it('counts unknown states as neutral', () => {
    assert.deepEqual(counts('success', 'weird'), { success: 1, failure: 0, pending: 0, skipped: 0, neutral: 1 })
  })

  it('summarizes failing first', () => {
    assert.equal(checksSummary(counts('success', 'failure', 'success', 'pending')), '1 failing, 1 pending, 2 successful')
    assert.equal(checksSummary(counts()), 'No checks')
  })
})

describe('stateBadge', () => {
  it('prefers merged / closed / draft over the review decision', () => {
    assert.deepEqual(stateBadge({ state: 'MERGED', draft: false, reviewDecision: 'APPROVED' }), { label: 'Merged', tone: 'merged' })
    assert.deepEqual(stateBadge({ state: 'OPEN', draft: true, reviewDecision: 'APPROVED' }), { label: 'Draft', tone: 'muted' })
    assert.deepEqual(stateBadge({ state: 'OPEN', draft: false, reviewDecision: 'CHANGES_REQUESTED' }), { label: 'Changes requested', tone: 'bad' })
    assert.equal(stateBadge({ state: 'OPEN', draft: false, reviewDecision: '' }), null)
  })
})

describe('lineOnOtherSide', () => {
  // Side b inserted 3 lines at 2..4 and replaced line 10 (a) with 2 lines (b 13..14).
  const hunks = [hunk(2, 2, 2, 5), hunk(10, 11, 13, 15)]

  it('maps unchanged lines across inserted ones', () => {
    assert.equal(lineOnOtherSide(hunks, 'a', 0), 0)
    assert.equal(lineOnOtherSide(hunks, 'a', 5), 8)
    assert.equal(lineOnOtherSide(hunks, 'b', 8), 5)
    assert.equal(lineOnOtherSide(hunks, 'a', 20), 24) // past the last hunk: +3 +1
  })

  it('puts lines inserted on one side level with the line above on the other', () => {
    // Side a has no lines in the first hunk: its row is above line 2 (= after line 1).
    assert.equal(lineOnOtherSide(hunks, 'b', 3), 1)
  })

  it('maps a replacement row by row, then to its last line', () => {
    assert.equal(lineOnOtherSide(hunks, 'b', 13), 10)
    assert.equal(lineOnOtherSide(hunks, 'b', 14), 10)
    assert.equal(lineOnOtherSide(hunks, 'a', 10), 13)
  })

  it('uses -1 for an insertion at the very top', () => {
    assert.equal(lineOnOtherSide([hunk(0, 0, 0, 2)], 'b', 1), -1)
  })
})

describe('placeThreads', () => {
  const hunks = [hunk(2, 2, 2, 5)]
  const lines = { a: 10, b: 13 }

  it('converts GitHub lines (1-based, LEFT/RIGHT) to anchors with mirrors', () => {
    const { anchors, detached } = placeThreads([thread('t1', 7), thread('t2', 1, 'LEFT')], hunks, lines)
    assert.deepEqual(detached, [])
    assert.deepEqual(anchors, [
      { key: 'a:0', side: 'a', line: 0, mirror: 0, threadIds: ['t2'], draft: false },
      { key: 'b:6', side: 'b', line: 6, mirror: 3, threadIds: ['t1'], draft: false },
    ])
  })

  it('groups threads on the same line, in order', () => {
    const { anchors } = placeThreads([thread('t1', 4), thread('t2', 4)], hunks, lines)
    assert.equal(anchors.length, 1)
    assert.deepEqual(anchors[0].threadIds, ['t1', 't2'])
  })

  it('detaches outdated, file-level and out-of-range threads', () => {
    const { anchors, detached } = placeThreads([thread('old', 0), thread('file', 1, 'RIGHT', true), thread('far', 99)], hunks, lines)
    assert.deepEqual(anchors, [])
    assert.deepEqual(detached, ['old', 'file', 'far'])
  })

  it('adds the new-comment box to an existing anchor or a new one', () => {
    const shared = placeThreads([thread('t1', 4)], hunks, lines, { side: 'b', line: 3 })
    assert.deepEqual(shared.anchors.map((x) => [x.key, x.draft, x.threadIds]), [['b:3', true, ['t1']]])
    const alone = placeThreads([], hunks, lines, { side: 'a', line: 9 })
    assert.deepEqual(alone.anchors, [{ key: 'a:9', side: 'a', line: 9, mirror: 12, threadIds: [], draft: true }])
  })

  it('never mirrors past the end of the other side', () => {
    const { anchors } = placeThreads([thread('t', 3, 'LEFT')], [hunk(1, 3, 1, 1)], { a: 3, b: 1 })
    assert.equal(anchors[0].mirror, 0)
  })
})

describe('foldsClearOf', () => {
  it('keeps folds away from threads and their spacers', () => {
    const folds = [
      { a: 0, b: 0, lines: 5 },
      { a: 10, b: 13, lines: 6 },
      { a: 20, b: 23, lines: 6 },
    ]
    const { anchors } = placeThreads([thread('t1', 15)], [hunk(6, 6, 6, 9)], { a: 40, b: 43 })
    // Line 15 (1-based) on b is 14, inside the second fold; its mirror a:11 too.
    assert.deepEqual(foldsClearOf(folds, anchors), [folds[0], folds[2]])
  })
})

describe('isCommentable', () => {
  it('accepts lines inside the diff hunks only', () => {
    const ranges = [{ from: 1, to: 7 }, { from: 20, to: 26 }]
    assert.equal(isCommentable(ranges, 7), true)
    assert.equal(isCommentable(ranges, 8), false)
    assert.equal(isCommentable(ranges, 20), true)
    assert.equal(isCommentable(null, 1), false)
  })
})

describe('threadCounts', () => {
  it('counts open and resolved threads per file', () => {
    assert.deepEqual(
      threadCounts([
        { path: 'a.go', resolved: false },
        { path: 'a.go', resolved: true },
        { path: 'b.go', resolved: false },
      ]),
      { 'a.go': { open: 1, resolved: 1 }, 'b.go': { open: 1, resolved: 0 } },
    )
  })
})

describe('timeAgo', () => {
  const now = Date.parse('2026-10-30T12:00:00Z')
  it('prints short relative times', () => {
    assert.equal(timeAgo('2026-10-30T11:59:30Z', now), 'just now')
    assert.equal(timeAgo('2026-10-30T11:55:00Z', now), '5m ago')
    assert.equal(timeAgo('2026-10-30T09:00:00Z', now), '3h ago')
    assert.equal(timeAgo('2026-10-28T12:00:00Z', now), '2d ago')
    assert.equal(timeAgo('2026-01-02T12:00:00Z', now), '2026-01-02')
    assert.equal(timeAgo('nope', now), '')
  })
})
