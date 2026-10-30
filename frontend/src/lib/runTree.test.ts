import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { applyNodes, countTests, emptyTree, failedNodes, nodeRequest, rerunFailedRequest, statusOf, type TestNode } from './runTree'

const go = (id: string, status: TestNode['status'], extra: Partial<TestNode> = {}): TestNode => ({
  id,
  name: id.split('/').pop()!,
  kind: 'test',
  status,
  dir: '/p/calc',
  ...extra,
})

describe('runTree', () => {
  const tree = applyNodes(emptyTree, [
    go('calc', 'running', { kind: 'package' }),
    go('calc::TestA', 'running', { parent: 'calc', test: 'TestA' }),
    go('calc::TestA/one', 'failed', { parent: 'calc::TestA', test: 'TestA/one' }),
    go('calc::TestA/two', 'passed', { parent: 'calc::TestA', test: 'TestA/two' }),
    go('calc::TestB', 'skipped', { parent: 'calc', test: 'TestB' }),
    go('calc::TestA', 'failed', { parent: 'calc', test: 'TestA' }),
    go('calc', 'failed', { kind: 'package' }),
  ])

  it('keeps arrival order and replaces updated nodes', () => {
    assert.deepEqual(tree.children[''], ['calc'])
    assert.deepEqual(tree.children['calc'], ['calc::TestA', 'calc::TestB'])
    assert.equal(tree.nodes['calc::TestA'].status, 'failed')
  })

  it('does not mutate the previous tree', () => {
    const next = applyNodes(tree, [go('calc::TestC', 'passed', { parent: 'calc', test: 'TestC' })])
    assert.equal(tree.children['calc'].length, 2)
    assert.equal(next.children['calc'].length, 3)
  })

  it('counts leaf tests only', () => {
    assert.deepEqual(countTests(tree), { passed: 1, failed: 1, skipped: 1, running: 0, total: 3 })
  })

  it('reports the innermost failures and reruns them', () => {
    assert.deepEqual(failedNodes(tree).map((n) => n.id), ['calc::TestA/one'])
    assert.deepEqual(rerunFailedRequest(tree, { lang: 'go', dirs: ['/p'] }), { lang: 'go', dirs: ['/p/calc'], tests: ['TestA/one'] })
  })

  it('reruns a whole package after a build failure', () => {
    const broken = applyNodes(emptyTree, [go('x', 'failed', { kind: 'package', dir: '/p/x' })])
    assert.deepEqual(rerunFailedRequest(broken, { lang: 'go', dirs: ['/p/x'] }), { lang: 'go', dirs: ['/p/x'], tests: [] })
    assert.equal(rerunFailedRequest(emptyTree, { lang: 'go', dirs: ['/p'] }), null)
  })

  it('keeps the file of a JS run', () => {
    const js = applyNodes(emptyTree, [{ id: 's › t', name: 't', kind: 'test', status: 'failed', test: 's t', dir: '/p/web' }])
    const last = { lang: 'js' as const, dirs: ['/p/web/src'], file: '/p/web/src/a.test.ts' }
    assert.deepEqual(rerunFailedRequest(js, last), { ...last, tests: ['s t'] })
    assert.deepEqual(nodeRequest(js.nodes['s › t'], last), { ...last, tests: ['s t'] })
  })

  it('finds a test status for a file in its package', () => {
    assert.equal(statusOf(tree, '/p/calc/calc_test.go', 'TestA/one'), 'failed')
    assert.equal(statusOf(tree, '/p/other/x_test.go', 'TestA/one'), undefined)
    assert.equal(statusOf(tree, '/p/calc/calc_test.go', 'TestZ'), undefined)
  })

  it('applies a batch of many siblings in one pass, matching one-by-one updates', () => {
    const batch = Array.from({ length: 1000 }, (_, i) => go(`calc::T${i}`, 'passed', { parent: 'calc', test: `T${i}` }))
    const base = applyNodes(emptyTree, [go('calc', 'running', { kind: 'package' })])
    const once = applyNodes(base, batch)
    const stepwise = batch.reduce((t, n) => applyNodes(t, [n]), base)
    assert.deepEqual(once, stepwise)
    assert.equal(once.children['calc'].length, 1000)
    assert.equal(base.children['calc'], undefined) // previous tree untouched
    // Updating existing nodes in a later batch keeps their position.
    const updated = applyNodes(once, [go('calc::T5', 'failed', { parent: 'calc', test: 'T5' }), go('calc::T1000', 'passed', { parent: 'calc', test: 'T1000' })])
    assert.equal(updated.children['calc'][5], 'calc::T5')
    assert.equal(updated.children['calc'].at(-1), 'calc::T1000')
    assert.equal(once.children['calc'].length, 1000)
    assert.equal(updated.nodes['calc::T5'].status, 'failed')
  })

  it('keeps statusOf in step with each new tree and prefers the first matching node', () => {
    const t1 = applyNodes(emptyTree, [
      go('a::TestX', 'running', { test: 'TestX', dir: '/p/a' }),
      go('root::TestX', 'passed', { test: 'TestX', dir: '/p' }),
    ])
    assert.equal(statusOf(t1, '/p/a/x_test.go', 'TestX'), 'running') // first match in arrival order
    assert.equal(statusOf(t1, '/p/b/x_test.go', 'TestX'), 'passed') // /p contains /p/b
    const t2 = applyNodes(t1, [go('a::TestX', 'failed', { test: 'TestX', dir: '/p/a' })])
    assert.equal(statusOf(t2, '/p/a/x_test.go', 'TestX'), 'failed')
    assert.equal(statusOf(t1, '/p/a/x_test.go', 'TestX'), 'running') // old tree's index unchanged
    const noDir = applyNodes(emptyTree, [{ id: 'j', name: 'j', kind: 'test', status: 'skipped', test: 's j' }])
    assert.equal(statusOf(noDir, '/anywhere/a.test.ts', 's j'), 'skipped')
  })
})
