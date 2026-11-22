import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@flint-ide/ui'
import { Check } from '@flint-ide/ui/icons'
import { useCallback, useRef, type CSSProperties, type KeyboardEvent } from 'react'
import { modKey } from '../../lib/platform'
import type { DropdownItem } from './Dropdown'

type Item = Extract<DropdownItem, { label: string; onSelect: unknown }>

export interface AppMenuProps {
  /** Where it opens: under a trigger's box, or at a point (a context menu: a box of no size). */
  readonly at: { readonly left: number; readonly top: number; readonly width: number; readonly height: number }
  readonly items: readonly DropdownItem[]
  readonly align: 'start' | 'end'
  readonly minWidth: number
  readonly emptyText: string
  /** Called once, however the menu closes (picked, Escape, a click elsewhere). */
  readonly onClose: () => void
  /** The dropdown's own trigger: a press on it is left to its click, which toggles the menu. */
  readonly trigger?: HTMLElement
}

/**
 * A menu without a filter box, as the UI kit's DropdownMenu (Base UI): keyboard
 * navigation, typeahead, submenu-safe focus, outside-click and Escape handling.
 * It opens at a box (an invisible trigger stands there), so the same component
 * serves dropdowns (Dropdown.tsx) and context menus (ContextMenu.tsx).
 */
export function AppMenu({ at, items, align, minWidth, emptyText, onClose, trigger }: AppMenuProps) {
  const closed = useRef(false)
  const close = useCallback(() => {
    if (closed.current) return
    closed.current = true
    onClose()
  }, [onClose])
  const withChecks = items.some((it) => isItem(it) && it.checked !== undefined)
  const anchorStyle: CSSProperties = { left: at.left, top: at.top, width: at.width, height: at.height }

  return (
    <DropdownMenu
      open
      // Not modal, like the menus it replaces: a right-click elsewhere opens the next
      // menu straight away, and the page doesn't stop scrolling.
      modal={false}
      onOpenChange={(open, details) => {
        if (open) return
        const target = details.event?.target
        if (details.reason === 'outside-press' && trigger && target instanceof Node && trigger.contains(target)) return
        close()
      }}
    >
      <DropdownMenuTrigger render={<button type="button" tabIndex={-1} aria-hidden className="fui-menu-anchor" style={anchorStyle} />} />
      <DropdownMenuContent
        className="app-menu"
        align={align}
        sideOffset={at.height ? 4 : 2}
        finalFocus={false}
        style={{ minWidth: Math.max(minWidth, at.width) }}
        onKeyDown={stopEscape}
      >
        {items.length === 0 && <div className="app-menu-empty">{emptyText}</div>}
        {items.map((it, i) => {
          if (it.type === 'separator') return <DropdownMenuSeparator key={`s${i}`} />
          if (it.type === 'section') return <DropdownMenuLabel key={`h${i}`}>{it.label}</DropdownMenuLabel>
          return <Row key={it.key ?? `${it.label}${i}`} item={it} withCheck={withChecks} close={close} />
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function Row({ item, withCheck, close }: { item: Item; withCheck: boolean; close: () => void }) {
  return (
    <DropdownMenuItem
      variant={item.danger ? 'destructive' : 'default'}
      disabled={item.disabled}
      icon={item.icon}
      onClick={(e) => {
        // Close first: what the item does may open the next menu or dialog.
        close()
        item.onSelect({ altKey: e.altKey, metaKey: modKey(e) })
      }}
    >
      {withCheck && <span className="app-menu-check">{item.checked && <Check aria-hidden />}</span>}
      <span className="app-menu-label">{item.label}</span>
      {item.hint && <span className="app-menu-hint">{item.hint}</span>}
      {item.kbd && <DropdownMenuShortcut>{item.kbd}</DropdownMenuShortcut>}
      {item.action && (
        <button
          type="button"
          className="app-menu-action"
          title={item.action.title}
          onClick={(e) => {
            e.stopPropagation()
            item.action?.onClick()
          }}
        >
          {item.action.label}
        </button>
      )}
    </DropdownMenuItem>
  )
}

/** Escape closes the menu only: a dialog under it (Modal) skips an Escape already handled. */
function stopEscape(e: KeyboardEvent) {
  if (e.key === 'Escape') e.preventDefault()
}

function isItem(it: DropdownItem): it is Item {
  return it.type === undefined || it.type === 'item'
}
