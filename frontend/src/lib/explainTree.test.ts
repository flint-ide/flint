import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  compactCount,
  formatMs,
  nodeLabel,
  parentPaths,
  planMetric,
  planTotal,
  shareOf,
  textParents,
  visiblePlanRows,
  visibleTextLines,
  type PlanNodeLike,
} from './explainTree'

const node = (nodeType: string, selfMs: number, selfCost: number, children: PlanNodeLike[] = [], extra: Partial<PlanNodeLike> = {}): PlanNodeLike => ({
  nodeType, selfMs, selfCost, analyzed: true, children, ...extra,
})

const plan = node('Hash Join', 2, 10, [
  node('Seq Scan', 6, 80, [], { relation: 'public.orders', alias: 'o' }),
  node('Hash', 0, 0, [node('Index Scan', 2, 10, [], { relation: 'users', alias: 'users', index: 'users_pkey' })]),
], { joinType: 'Left' })

describe('visiblePlanRows', () => {
  it('flattens depth-first with stable paths', () => {
    const rows = visiblePlanRows(plan, new Set())
    assert.deepEqual(rows.map((r) => [r.path, r.depth, r.node.nodeType]), [
      ['0', 0, 'Hash Join'],
      ['0.0', 1, 'Seq Scan'],
      ['0.1', 1, 'Hash'],
      ['0.1.0', 2, 'Index Scan'],
    ])
    assert.deepEqual(rows.map((r) => r.hasChildren), [true, false, true, false])
  })

  it('hides descendants of collapsed nodes', () => {
    assert.deepEqual(visiblePlanRows(plan, new Set(['0.1'])).map((r) => r.path), ['0', '0.0', '0.1'])
    assert.deepEqual(visiblePlanRows(plan, new Set(['0'])).map((r) => r.path), ['0'])
    assert.deepEqual(parentPaths(plan), ['0', '0.1'])
  })
})

describe('shares', () => {
  it('measures analyzed plans by self time, estimates by self cost', () => {
    assert.equal(planMetric(plan), 'time')
    assert.equal(planMetric({ ...plan, analyzed: false }), 'cost')
    const total = planTotal(plan, 'time')
    assert.equal(total, 10)
    assert.equal(shareOf(plan.children[0], total, 'time'), 0.6)
    const costTotal = planTotal(plan, 'cost')
    assert.equal(costTotal, 100)
    assert.equal(shareOf(plan.children[0], costTotal, 'cost'), 0.8)
    assert.equal(shareOf(plan, 0, 'time'), 0)
  })
})

describe('labels and numbers', () => {
  it('names nodes like psql does', () => {
    assert.equal(nodeLabel(plan), 'Hash Join (Left)')
    assert.equal(nodeLabel(plan.children[0]), 'Seq Scan on public.orders o')
    assert.equal(nodeLabel(plan.children[1].children[0]), 'Index Scan using users_pkey on users')
  })

  it('formats counts and times compactly', () => {
    assert.equal(compactCount(12), '12')
    assert.equal(compactCount(9999), '9999')
    assert.equal(compactCount(12345), '12.3k')
    assert.equal(compactCount(4_200_000), '4.2M')
    assert.equal(compactCount(0.5), '0.5')
    assert.equal(formatMs(0.0123), '0.012 ms')
    assert.equal(formatMs(4.5), '4.50 ms')
    assert.equal(formatMs(1240), '1.24 s')
  })
})

describe('text plans', () => {
  const lines = [{ depth: 0 }, { depth: 1 }, { depth: 2 }, { depth: 2 }, { depth: 1 }, { depth: 0 }]

  it('finds lines with nested lines', () => {
    assert.deepEqual(textParents(lines), [true, true, false, false, false, false])
  })

  it('hides lines nested under collapsed ones', () => {
    assert.deepEqual(visibleTextLines(lines, new Set()), [0, 1, 2, 3, 4, 5])
    assert.deepEqual(visibleTextLines(lines, new Set([1])), [0, 1, 4, 5])
    assert.deepEqual(visibleTextLines(lines, new Set([0])), [0, 5])
  })
})
