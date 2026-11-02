import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { contribute, fileMenuFor, tabKind, useContributions } from './contributions'
import { checkFeatures, defineFeature, type StatusItem } from './features'
import { mergePanels, type PanelSpec } from '../panels/registry'

const Nothing = () => null
const panel = (id: string, order: number): PanelSpec => ({ id, title: id, icon: 'files', order, component: Nothing })
const status = (id: string, order: number): StatusItem => ({ id, order, component: Nothing })

describe('contributions', () => {
  it('flattens sources, sorts ordered kinds and withdraws a source as a whole', () => {
    const offA = contribute('feature:a', { statusItems: [status('a1', 20)], tabKinds: [{ id: 'view-a', component: Nothing }] })
    const offB = contribute('feature:b', { statusItems: [status('b1', 10)], panels: [panel('b', 5)] })
    try {
      assert.deepEqual(useContributions.getState().statusItems.map((s) => s.id), ['b1', 'a1'])
      assert.equal(tabKind('view-a')?.id, 'view-a')
      offA()
      assert.deepEqual(useContributions.getState().statusItems.map((s) => s.id), ['b1'])
      assert.equal(tabKind('view-a'), undefined)
      offA() // a second call does nothing
      assert.equal(useContributions.getState().panels.length, 1)
    } finally {
      offB()
    }
    assert.equal(useContributions.getState().panels.length, 0)
  })

  it('refuses a duplicate id or source and keeps what was there', () => {
    const off = contribute('feature:a', { statusItems: [status('same', 1)] })
    try {
      assert.throws(() => contribute('feature:b', { statusItems: [status('same', 2)] }), /duplicate statusItems id: same/)
      assert.throws(() => contribute('feature:a', {}), /already contributed/)
      assert.deepEqual(useContributions.getState().statusItems.map((s) => s.id), ['same'])
      contribute('feature:b', {})() // and nothing was left behind for feature:b
    } finally {
      off()
    }
  })

  it('merges contributed panels into the built-in ones by order', () => {
    const core = [panel('files', 10), panel('search', 20)] as const
    assert.equal(mergePanels(core, []), core)
    assert.deepEqual(mergePanels(core, [panel('docker', 15)]).map((p) => p.id), ['files', 'docker', 'search'])
    assert.throws(() => mergePanels(core, [panel('files', 1)]), /duplicate panel id/)
  })
})

describe('feature modules', () => {
  const a = defineFeature({ id: 'a', commands: [{ id: 'a.run', title: 'Run A', run: () => {} }], panels: [panel('pa', 1)] })

  it('accepts distinct features', () => {
    checkFeatures([a, defineFeature({ id: 'b', panels: [panel('pb', 1)] })])
  })

  it('refuses colliding ids', () => {
    assert.throws(() => checkFeatures([a, defineFeature({ id: 'a' })]), /duplicate feature id: a/)
    assert.throws(() => checkFeatures([a, defineFeature({ id: 'b', panels: [panel('pa', 2)] })]), /duplicate panel id: pa/)
    assert.throws(
      () => checkFeatures([a, defineFeature({ id: 'b', commands: [{ id: 'a.run', title: 'Again', run: () => {} }] })]),
      /duplicate command id: a.run/,
    )
  })
})

describe('file menu entries', () => {
  it('offers entries whose when accepts the paths and reports failures', async () => {
    const ran: string[][] = []
    const errors: unknown[] = []
    const items = fileMenuFor(
      [
        { id: 'go', label: 'Go Thing', when: (p) => p.every((x) => x.endsWith('.go')), run: (p) => void ran.push([...p]) },
        { id: 'broken-when', label: 'Never', when: () => { throw new Error('x') }, run: () => {} },
        { id: 'fails', label: 'Fails', run: () => Promise.reject(new Error('no')) },
      ],
      ['/p/a.go'],
      (err) => errors.push(err),
    )
    assert.deepEqual(items.map((i) => i.label), ['Go Thing', 'Fails'])
    items.forEach((i) => i.onSelect())
    await new Promise((resolve) => setTimeout(resolve, 0))
    assert.deepEqual(ran, [['/p/a.go']])
    assert.equal(errors.length, 1)
  })
})
