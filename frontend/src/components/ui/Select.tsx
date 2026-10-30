import type { ReactNode } from 'react'
import { Dropdown, type DropdownItem } from './Dropdown'

export interface SelectOption<T extends string | number> {
  value: T
  label: string
  icon?: ReactNode
  hint?: string
  disabled?: boolean
}

export interface SelectProps<T extends string | number> {
  value: T
  options: SelectOption<T>[]
  onChange: (value: T) => void
  placeholder?: string
  /** Prefix shown in the trigger, e.g. "limit". */
  label?: string
  searchable?: boolean
  size?: 'sm' | 'md'
  title?: string
  disabled?: boolean
  /** Stretch to the container's width (forms). */
  block?: boolean
  align?: 'start' | 'end'
  /** Option rendered for the empty value (e.g. "— none —"). */
  allowEmpty?: string
}

/** Flint's select: same popover as every other menu, keyboard friendly, optional filter. */
export function Select<T extends string | number>(props: SelectProps<T>) {
  const { value, options, onChange, placeholder = 'Select…', label, searchable, size = 'md', title, disabled, block, align, allowEmpty } = props
  const current = options.find((o) => o.value === value)

  const items: DropdownItem[] = [
    ...(allowEmpty !== undefined
      ? [{ label: allowEmpty, checked: value === ('' as T), onSelect: () => onChange('' as T) } as DropdownItem]
      : []),
    ...options.map(
      (o): DropdownItem => ({
        key: String(o.value),
        label: o.label,
        icon: o.icon,
        hint: o.hint,
        disabled: o.disabled,
        checked: o.value === value,
        onSelect: () => onChange(o.value),
      }),
    ),
  ]

  return (
    <Dropdown
      items={items}
      searchable={searchable}
      align={align}
      trigger={(t) => (
        <button
          {...t}
          type="button"
          title={title}
          disabled={disabled}
          className={`select ${size}${block ? ' block' : ''}${current ? '' : ' empty'}`}
        >
          {label && <span className="select-prefix">{label}</span>}
          {current?.icon && <span className="dd-icon">{current.icon}</span>}
          <span className="select-value">{current?.label ?? (value === '' && allowEmpty ? allowEmpty : placeholder)}</span>
          <svg className="select-chevron" width="10" height="10" viewBox="0 0 10 10" aria-hidden>
            <path d="M2 3.5l3 3 3-3" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      )}
    />
  )
}
