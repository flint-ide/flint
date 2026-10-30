import { describe, expect, it } from 'vitest'
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
    expect(filterItems(items, '  ')).toBe(items)
  })

  it('keeps a section only above its matches and drops separators', () => {
    const labels = filterItems(items, 'poc').map((i) => ('label' in i ? i.label : i.type))
    expect(labels).toEqual(['Recent', 'poc-mailos'])
  })

  it('matches hints and keywords, case-insensitively', () => {
    expect(filterItems(items, 'WORK').map((i) => ('label' in i ? i.label : ''))).toContain('poc-mailos')
    expect(filterItems(items, 'mail')).toHaveLength(2)
    expect(filterItems(items, 'zzz')).toEqual([])
  })
})
