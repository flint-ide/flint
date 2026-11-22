import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { DropdownItem } from './Dropdown'
import { withGroupHeadings } from './Select'

const item = (label: string): DropdownItem => ({ label, onSelect: () => {} })
const labels = (items: DropdownItem[]) => items.map((it) => (it.type === 'section' ? `# ${it.label}` : it.type === 'separator' ? '---' : it.label))

describe('withGroupHeadings', () => {
  it('adds a heading where each group starts', () => {
    const options = [{ group: 'Dark' }, { group: 'Dark' }, { group: 'Light' }]
    assert.deepEqual(labels(withGroupHeadings(options, [item('a'), item('b'), item('c')])), ['# Dark', 'a', 'b', '# Light', 'c'])
  })

  it('leaves ungrouped options as they are', () => {
    assert.deepEqual(labels(withGroupHeadings([{}, {}], [item('a'), item('b')])), ['a', 'b'])
  })

  it('starts a new heading after ungrouped options', () => {
    assert.deepEqual(labels(withGroupHeadings([{}, { group: 'G' }], [item('a'), item('b')])), ['a', '# G', 'b'])
  })
})
