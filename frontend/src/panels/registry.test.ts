import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { definePanels, isPanelAvailable, panelCommands, panelTooltip, resolvePanel, type PanelSpec } from './registry'

const Empty = () => null
const spec = (id: string, order: number, extra: Partial<PanelSpec> = {}): PanelSpec => ({
  id, title: id.toUpperCase(), icon: 'files', order, component: Empty, ...extra,
})

describe('definePanels', () => {
  it('orders by `order`, keeping listing order on ties', () => {
    const panels = definePanels([spec('c', 30), spec('a', 10), spec('b2', 20), spec('b1', 20)])
    assert.deepEqual(panels.map((p) => p.id), ['a', 'b2', 'b1', 'c'])
  })

  it('rejects duplicate ids', () => {
    assert.throws(() => definePanels([spec('a', 1), spec('a', 2)]), /duplicate panel id: a/)
  })
})

describe('resolvePanel', () => {
  const panels = definePanels([spec('search', 2), spec('files', 1)])

  it('finds a panel by id', () => {
    assert.equal(resolvePanel(panels, 'search')?.id, 'search')
  })

  it('falls back to files for an unknown or removed id', () => {
    assert.equal(resolvePanel(panels, 'gone')?.id, 'files')
  })

  it('falls back to the first panel without files, and to nothing without panels', () => {
    assert.equal(resolvePanel([spec('x', 1), spec('y', 2)], 'gone')?.id, 'x')
    assert.equal(resolvePanel([], 'files'), undefined)
  })
})

describe('isPanelAvailable', () => {
  it('treats a missing or throwing predicate as shown and hidden', () => {
    assert.equal(isPanelAvailable({}), true)
    assert.equal(isPanelAvailable({ when: () => false }), false)
    assert.equal(
      isPanelAvailable({
        when: () => {
          throw new Error('store not ready')
        },
      }),
      false,
    )
  })
})

describe('panelTooltip', () => {
  it('appends the formatted shortcut', () => {
    assert.equal(panelTooltip({ title: 'Search' }, 'Mod+Shift+F'), 'Search (⇧⌘F)')
    assert.equal(panelTooltip({ title: 'Commit' }, 'Mod+K'), 'Commit (⌘K)')
  })

  it('is the bare title without a shortcut', () => {
    assert.equal(panelTooltip({ title: 'Docker' }, undefined), 'Docker')
    assert.equal(panelTooltip({ title: 'Docker' }, ''), 'Docker')
  })
})

describe('panelCommands', () => {
  it('generates "Show <title>" in View by default and keeps overrides', () => {
    const shown: string[] = []
    const when = () => true
    const cmds = panelCommands(
      [
        spec('files', 1, { title: 'Files', command: { keywords: 'tree' } }),
        spec('pulls', 2, { title: 'Pull Requests', when, command: { id: 'github.pullRequests', title: 'Pull Requests…', category: 'GitHub' } }),
        spec('hidden', 3, { command: false }),
        spec('plain', 4, { title: 'Plain' }),
      ],
      (id) => shown.push(id),
    )
    assert.deepEqual(
      cmds.map((c) => [c.id, c.title, c.category, c.keywords]),
      [
        ['view.files', 'Show Files', 'View', 'tree'],
        ['github.pullRequests', 'Pull Requests…', 'GitHub', undefined],
        ['view.plain', 'Show Plain', 'View', undefined],
      ],
    )
    assert.equal(cmds[1].when, when)
    cmds.forEach((c) => void c.run())
    assert.deepEqual(shown, ['files', 'pulls', 'plain'])
  })
})
