import type { ReactNode } from 'react'
import type { DropdownItem } from './Dropdown'

// The pure part of Flint's Select (Select.tsx): which popup it opens and how its
// values map to the kit Select's string values. Kept free of React and the DOM
// so node:test covers it.

export interface SelectOption<T extends string | number> {
  value: T
  label: string
  icon?: ReactNode
  hint?: string
  disabled?: boolean
  /** Heading the option is listed under; options of one group must be next to each other. */
  group?: string
}

/** The kit Select's value for the allowEmpty row (Base UI keeps null for "nothing chosen"). */
export const EMPTY_KEY = ''

/** The kit Select works on strings: numbers become their decimal text. */
export function optionKey(value: string | number): string {
  return String(value)
}

/** The kit Select's value for `value`: its option's key, the empty row's, or null (placeholder). */
export function selectedKey<T extends string | number>(options: readonly SelectOption<T>[], value: T, allowEmpty: string | undefined): string | null {
  const current = options.find((o) => o.value === value)
  if (current) return optionKey(current.value)
  return allowEmpty !== undefined && value === '' ? EMPTY_KEY : null
}

/**
 * The value a key picked in the kit Select stands for, wrapped so a falsy value
 * (0, '') is told apart from "no such option".
 */
export function valueForKey<T extends string | number>(options: readonly SelectOption<T>[], key: string | null, allowEmpty: string | undefined): { value: T } | null {
  if (key === null) return null
  const option = options.find((o) => optionKey(o.value) === key)
  if (option) return { value: option.value }
  return allowEmpty !== undefined && key === EMPTY_KEY ? { value: '' as T } : null
}

/** The kit Select's items (value → label), used for the trigger text and typeahead. */
export function kitItems<T extends string | number>(options: readonly SelectOption<T>[], allowEmpty: string | undefined): { value: string; label: string }[] {
  const empty = allowEmpty !== undefined ? [{ value: EMPTY_KEY, label: allowEmpty }] : []
  return [...empty, ...options.map((o) => ({ value: optionKey(o.value), label: o.label }))]
}

/** What the trigger shows: the option's label, the empty row's, or the placeholder (the kit dims it). */
export function triggerText<T extends string | number>(current: SelectOption<T> | undefined, value: T, allowEmpty: string | undefined, placeholder: string): string {
  if (current) return current.label
  return value === '' && allowEmpty ? allowEmpty : placeholder
}

/**
 * Whether the select opens Flint's menu (Dropdown) instead of the kit Select:
 * for a filter box (asked for, or a long list when not ruled out, like every
 * Dropdown) and for group headings, which the kit Select has no part for.
 */
export function opensAsMenu(options: readonly { group?: string }[], searchable: boolean | undefined, rows: number, autoSearchAt: number): boolean {
  if (searchable ?? rows >= autoSearchAt) return true
  return options.some((o) => o.group !== undefined)
}

/** The menu items with a section heading where an option's group starts (items[i] belongs to options[i]). */
export function withGroupHeadings(options: readonly { group?: string }[], items: readonly DropdownItem[]): DropdownItem[] {
  return items.flatMap((item, i) => {
    const group = options[i]?.group
    return group !== undefined && group !== options[i - 1]?.group ? [{ type: 'section', label: group }, item] : [item]
  })
}
