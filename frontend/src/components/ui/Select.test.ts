import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type { DropdownItem } from './Dropdown'
import { EMPTY_KEY, kitItems, opensAsMenu, selectedKey, triggerText, valueForKey, withGroupHeadings, type SelectOption } from './selectModel'

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

const numbers: SelectOption<number>[] = [
  { value: 0, label: 'none' },
  { value: 10, label: 'ten' },
]
const names: SelectOption<string>[] = [{ value: 'a', label: 'Alpha' }]

describe('kit Select values', () => {
  it('maps values to string keys and back, falsy ones included', () => {
    assert.equal(selectedKey(numbers, 0, undefined), '0')
    assert.deepEqual(valueForKey(numbers, '0', undefined), { value: 0 })
    assert.deepEqual(valueForKey(numbers, '10', undefined), { value: 10 })
  })

  it('has no key for a value without an option (the placeholder shows)', () => {
    assert.equal(selectedKey(numbers, 5, undefined), null)
    assert.equal(valueForKey(numbers, '5', undefined), null)
    assert.equal(valueForKey(numbers, null, undefined), null)
  })

  it('gives the empty value its own row only with allowEmpty', () => {
    assert.equal(selectedKey(names, '', 'Default'), EMPTY_KEY)
    assert.equal(selectedKey(names, '', undefined), null)
    assert.deepEqual(valueForKey(names, EMPTY_KEY, 'Default'), { value: '' })
    assert.equal(valueForKey(names, EMPTY_KEY, undefined), null)
    assert.deepEqual(kitItems(names, 'Default'), [
      { value: '', label: 'Default' },
      { value: 'a', label: 'Alpha' },
    ])
    assert.deepEqual(kitItems(names, undefined), [{ value: 'a', label: 'Alpha' }])
  })
})

describe('triggerText', () => {
  it('shows the label, the empty row or the placeholder', () => {
    assert.equal(triggerText(names[0], 'a', undefined, 'Pick'), 'Alpha')
    assert.equal(triggerText(undefined, '', 'Default', 'Pick'), 'Default')
    assert.equal(triggerText(undefined, '', undefined, 'Pick'), 'Pick')
  })
})

describe('opensAsMenu', () => {
  it('uses the filter menu when searchable, or for a long list unless ruled out', () => {
    assert.equal(opensAsMenu([], true, 2, 9), true)
    assert.equal(opensAsMenu([], undefined, 9, 9), true)
    assert.equal(opensAsMenu([], false, 30, 9), false)
    assert.equal(opensAsMenu([], undefined, 3, 9), false)
  })

  it('uses the menu for group headings', () => {
    assert.equal(opensAsMenu([{}, { group: 'Dark' }], false, 2, 9), true)
  })
})
