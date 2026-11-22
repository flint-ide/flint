import { Select as KitSelect, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@flint-ide/ui'
import type { KeyboardEvent, ReactNode } from 'react'
import { AUTO_SEARCH_AT, Dropdown, type DropdownItem, type TriggerProps } from './Dropdown'
import { kitItems, opensAsMenu, optionKey, selectedKey, triggerText, valueForKey, withGroupHeadings, type SelectOption } from './selectModel'

export { withGroupHeadings, type SelectOption } from './selectModel'

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

/**
 * Flint's select on the UI kit: the kit Select (Base UI: keyboard, typeahead,
 * focus return), or, for a filter box or group headings, Flint's menu (Dropdown)
 * opened from the same kit trigger.
 */
export function Select<T extends string | number>(props: SelectProps<T>) {
  const rows = props.options.length + (props.allowEmpty !== undefined ? 1 : 0)
  if (opensAsMenu(props.options, props.searchable, rows, AUTO_SEARCH_AT)) return <MenuSelect {...props} />
  return <ListSelect {...props} />
}

function ListSelect<T extends string | number>(props: SelectProps<T>) {
  const { value, options, onChange, disabled, allowEmpty } = props
  return (
    <KitSelect
      value={selectedKey(options, value, allowEmpty)}
      items={kitItems(options, allowEmpty)}
      disabled={disabled}
      onValueChange={(key) => {
        const picked = valueForKey(options, key, allowEmpty)
        if (picked) onChange(picked.value)
      }}
    >
      <Trigger select={props} />
      <SelectContent className="flint-select-popup" onKeyDown={stopEscape}>
        {allowEmpty !== undefined && <SelectItem value="">{allowEmpty}</SelectItem>}
        {options.map((o) => (
          <SelectItem key={optionKey(o.value)} value={optionKey(o.value)} disabled={o.disabled}>
            {o.icon && <span className="flint-select-icon">{o.icon}</span>}
            <span className="flint-select-text">{o.label}</span>
            {o.hint && <span className="flint-select-hint">{o.hint}</span>}
          </SelectItem>
        ))}
      </SelectContent>
    </KitSelect>
  )
}

/**
 * The filter menu behind the kit trigger. The kit Select root is there only for
 * the trigger (its look and states) and stays closed: the trigger's own open
 * requests are ignored and the Dropdown's handlers open the menu instead.
 */
function MenuSelect<T extends string | number>(props: SelectProps<T>) {
  const { value, options, onChange, searchable, align, allowEmpty, disabled } = props
  const items: DropdownItem[] = [
    ...(allowEmpty !== undefined ? [{ label: allowEmpty, checked: value === ('' as T), onSelect: () => onChange('' as T) }] : []),
    ...withGroupHeadings(
      options,
      options.map(
        (o): DropdownItem => ({
          key: optionKey(o.value),
          label: o.label,
          icon: o.icon,
          hint: o.hint,
          disabled: o.disabled,
          checked: o.value === value,
          onSelect: () => onChange(o.value),
        }),
      ),
    ),
  ]
  return (
    <Dropdown
      items={items}
      searchable={searchable}
      align={align}
      trigger={(t) => (
        <KitSelect value={selectedKey(options, value, allowEmpty)} items={kitItems(options, allowEmpty)} disabled={disabled} open={false}>
          <Trigger select={props} menu={t} />
        </KitSelect>
      )}
    />
  )
}

/** The kit trigger with the prefix label, the option's icon and text. A disabled one gets its title from a wrapper (it takes no pointer events). */
function Trigger<T extends string | number>({ select, menu }: { select: SelectProps<T>; menu?: TriggerProps }) {
  const { value, options, placeholder = 'Select…', label, size = 'md', title, disabled, block, allowEmpty } = select
  const current = options.find((o) => o.value === value)
  const text = triggerText(current, value, allowEmpty, placeholder)
  const trigger = (
    <SelectTrigger
      className="flint-select"
      data-size={size}
      data-block={block ? '' : undefined}
      {...(disabled || !title ? {} : { title })}
      {...(menu ? menuTriggerProps(menu) : {})}
    >
      {label && <span className="flint-select-label">{label}</span>}
      <SelectValue className="flint-select-value">
        {() => <TriggerValue icon={current?.icon} text={text} />}
      </SelectValue>
    </SelectTrigger>
  )
  if (!disabled || !title) return trigger
  return (
    <span className="flint-select-wrap" data-block={block ? '' : undefined} title={title}>
      {trigger}
    </span>
  )
}

/** The Dropdown's trigger props on the kit trigger; data-popup-open gives it the kit's open look. */
function menuTriggerProps(t: TriggerProps) {
  return {
    ref: t.ref,
    onClick: t.onClick,
    onKeyDown: t.onKeyDown,
    'aria-haspopup': t['aria-haspopup'],
    'aria-expanded': t['aria-expanded'],
    ...(t['data-open'] ? { 'data-popup-open': '' } : {}),
  }
}

function TriggerValue({ icon, text }: { icon?: ReactNode; text: string }) {
  return (
    <>
      {icon && <span className="flint-select-icon">{icon}</span>}
      <span className="flint-select-text">{text}</span>
    </>
  )
}

/** Escape closes the list only: a dialog under it (Modal) skips an Escape already handled. */
function stopEscape(e: KeyboardEvent) {
  if (e.key === 'Escape') e.preventDefault()
}
