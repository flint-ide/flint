import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { filterItems, type DropdownItem } from './Dropdown'

const noop = () => undefined
const items: DropdownItem[] = [
  { label: 'Open Folder…', onSelect: noop },
  { type: 'separator' },
  { type: 'section', label: 'Recent' },
  { label: 'flint', hint: '~/projects', onSelect: noop },
  { label: 'poc-mailos', hint: '~/work', keywords: 'mail', onSelect: noop },
  { type: 'section', label: 'Other' },
  { label: 'notes', onSelect: noop },
]

describe('filterItems', () => {
  it('returns everything for an empty query', () => {
    assert.equal(filterItems(items, '  '), items)
  })

  it('keeps a section only above its matches and drops separators', () => {
    const labels = filterItems(items, 'poc').map((i) => ('label' in i ? i.label : i.type))
    assert.deepEqual(labels, ['Recent', 'poc-mailos'])
  })

  it('matches hints and keywords, case-insensitively', () => {
    assert.ok((filterItems(items, 'WORK').map((i) => ('label' in i ? i.label : ''))).includes('poc-mailos'))
    assert.equal((filterItems(items, 'mail')).length, 2)
    assert.deepEqual(filterItems(items, 'zzz'), [])
  })
})
