import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react'
import { createPortal } from 'react-dom'

// The one popover menu used across Flint (title bar, "+" menu, selects, settings,
// right-click context menus via store/contextMenu). Rendered in a portal with fixed
// positioning so it never gets clipped by scrolling tab strips or panels.

export type DropdownItem =
  | {
      type?: 'item'
      key?: string
      label: string
      icon?: ReactNode
      /** Secondary text (path, description) shown dimmed after the label. */
      hint?: string
      kbd?: string
      checked?: boolean
      disabled?: boolean
      danger?: boolean
      /** Extra words matched by the filter. */
      keywords?: string
      onSelect: (e: { altKey: boolean; metaKey: boolean }) => void
      /** Small trailing action (e.g. remove from list); doesn't close the menu. */
      action?: { label: string; title: string; onClick: () => void }
    }
  | { type: 'section'; label: string }
  | { type: 'separator' }

type Item = Extract<DropdownItem, { label: string; onSelect: unknown }>

export interface DropdownProps {
  /** Renders the trigger; spread `props` onto a button. */
  trigger: (props: TriggerProps) => ReactNode
  items: DropdownItem[] | (() => DropdownItem[])
  align?: 'start' | 'end'
  /** Show a filter box (also enabled automatically for long lists). */
  searchable?: boolean
  placeholder?: string
  minWidth?: number
  /** Called when the menu opens (e.g. to refresh data). */
  onOpen?: () => void
  emptyText?: string
}

export interface TriggerProps {
  ref: (el: HTMLElement | null) => void
  onClick: () => void
  onKeyDown: (e: ReactKeyboardEvent) => void
  'aria-haspopup': 'menu'
  'aria-expanded': boolean
  'data-open': boolean
}

const AUTO_SEARCH_AT = 9

export function Dropdown({ trigger, items, align = 'start', searchable, placeholder = 'Filter…', minWidth = 200, onOpen, emptyText = 'Nothing here' }: DropdownProps) {
  const [open, setOpen] = useState(false)
  const anchor = useRef<HTMLElement | null>(null)

  const close = useCallback(() => {
    setOpen(false)
    anchor.current?.focus()
  }, [])
  const toggle = () => {
    if (!open) onOpen?.()
    setOpen(!open)
  }

  return (
    <>
      {trigger({
        ref: (el) => (anchor.current = el),
        onClick: toggle,
        onKeyDown: (e) => {
          if ((e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') && !open) {
            e.preventDefault()
            onOpen?.()
            setOpen(true)
          }
        },
        'aria-haspopup': 'menu',
        'aria-expanded': open,
        'data-open': open,
      })}
      {open && anchor.current && (
        <Menu
          anchor={anchor.current}
          items={typeof items === 'function' ? items() : items}
          align={align}
          searchable={searchable}
          placeholder={placeholder}
          minWidth={minWidth}
          emptyText={emptyText}
          onClose={close}
        />
      )}
    </>
  )
}

export interface MenuProps {
  anchor: HTMLElement
  /** Opened at a point (context menu): placed below-right of it, flipped to stay on screen. */
  atPoint?: boolean
  items: DropdownItem[]
  align: 'start' | 'end'
  searchable?: boolean
  placeholder: string
  minWidth: number
  emptyText: string
  onClose: () => void
}

/** The menu itself, positioned next to anchor (also used for context menus). */
export function Menu({ anchor, atPoint, items, align, searchable, placeholder, minWidth, emptyText, onClose }: MenuProps) {
  const menu = useRef<HTMLDivElement>(null)
  const [query, setQuery] = useState('')
  const [cursor, setCursorState] = useState(-1)
  // Mirror in a ref so key handlers see the latest position even between renders.
  const cursorRef = useRef(-1)
  const setCursor = (i: number) => {
    cursorRef.current = i
    setCursorState(i)
  }
  const [pos, setPos] = useState<{ top: number; left: number; maxHeight: number; width: number } | null>(null)
  const withSearch = searchable ?? items.filter(isItem).length >= AUTO_SEARCH_AT

  const visible = useMemo(() => filterItems(items, query), [items, query])
  const selectable = visible.flatMap((it, i) => (isItem(it) && !it.disabled ? [i] : []))

  // Start on the checked item (selects) or the first one.
  useEffect(() => {
    const checked = visible.findIndex((it) => isItem(it) && it.checked)
    setCursor(checked >= 0 ? checked : (selectable[0] ?? -1))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])

  useLayoutEffect(() => {
    const r = anchor.getBoundingClientRect()
    if (atPoint) {
      setPos({ top: r.top + 2, left: Math.max(8, Math.min(r.left, window.innerWidth - minWidth - 8)), maxHeight: window.innerHeight - 16, width: minWidth })
      return
    }
    const width = Math.max(minWidth, r.width)
    const below = window.innerHeight - r.bottom - 8
    const above = r.top - 8
    const openUp = below < 220 && above > below
    const left = align === 'end' ? Math.max(8, r.right - width) : Math.min(r.left, window.innerWidth - width - 8)
    setPos({
      top: openUp ? Math.max(8, r.top - 4 - Math.min(above, 420)) : r.bottom + 4,
      left,
      maxHeight: Math.min(openUp ? above : below, 420),
      width,
    })
  }, [anchor, atPoint, align, minWidth])

  // A context menu that would run off the bottom or right edge opens up / to the left instead.
  useLayoutEffect(() => {
    const el = menu.current
    if (!atPoint || !pos || !el) return
    const r = el.getBoundingClientRect()
    const top = r.bottom > window.innerHeight - 8 ? Math.max(8, pos.top - 4 - r.height) : pos.top
    const left = r.right > window.innerWidth - 8 ? Math.max(8, window.innerWidth - 8 - r.width) : pos.left
    if (top !== pos.top || left !== pos.left) setPos({ ...pos, top, left })
  }, [atPoint, pos])

  // Close on outside click, scroll elsewhere, window blur or resize.
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (!menu.current?.contains(e.target as Node) && !anchor.contains(e.target as Node)) onClose()
    }
    const onScroll = (e: Event) => {
      if (!menu.current?.contains(e.target as Node)) onClose()
    }
    window.addEventListener('mousedown', onDown, true)
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('resize', onClose)
    window.addEventListener('blur', onClose)
    return () => {
      window.removeEventListener('mousedown', onDown, true)
      window.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('resize', onClose)
      window.removeEventListener('blur', onClose)
    }
  }, [anchor, onClose])

  useEffect(() => {
    menu.current?.querySelector<HTMLElement>('[data-cursor="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  useEffect(() => {
    if (!withSearch) menu.current?.focus()
  }, [withSearch, pos])

  const choose = (i: number, e: { altKey: boolean; metaKey: boolean }) => {
    const it = visible[i]
    if (!it || !isItem(it) || it.disabled) return
    onClose()
    it.onSelect(e)
  }

  const onKeyDown = (e: ReactKeyboardEvent) => {
    const cur = cursorRef.current
    const at = selectable.indexOf(cur)
    if (e.key === 'ArrowDown') {
      setCursor(selectable[Math.min(at + 1, selectable.length - 1)] ?? -1)
    } else if (e.key === 'ArrowUp') {
      setCursor(selectable[Math.max(at - 1, 0)] ?? -1)
    } else if (e.key === 'Enter') {
      choose(cur, { altKey: e.altKey, metaKey: e.metaKey })
    } else if (e.key === 'Escape' || e.key === 'Tab') {
      onClose()
    } else {
      return
    }
    e.preventDefault()
    e.stopPropagation()
  }

  if (!pos) return null
  return createPortal(
    <div
      ref={menu}
      className="dd-menu"
      role="menu"
      tabIndex={-1}
      style={{ top: pos.top, left: pos.left, minWidth: pos.width, maxHeight: pos.maxHeight }}
      onKeyDown={onKeyDown}
    >
      {withSearch && (
        <input
          className="dd-search"
          autoFocus
          value={query}
          placeholder={placeholder}
          spellCheck={false}
          onChange={(e) => setQuery(e.target.value)}
        />
      )}
      <div className="dd-items">
        {visible.length === 0 && <div className="dd-empty">{query ? 'No matches' : emptyText}</div>}
        {visible.map((it, i) => {
          if (it.type === 'separator') return <div key={`s${i}`} className="dd-separator" />
          if (it.type === 'section') return <div key={`h${i}`} className="dd-section">{it.label}</div>
          return (
            <div
              key={it.key ?? `${it.label}${i}`}
              role="menuitem"
              aria-disabled={it.disabled}
              data-cursor={i === cursor}
              className={`dd-item${it.danger ? ' danger' : ''}${it.disabled ? ' disabled' : ''}`}
              onMouseMove={() => !it.disabled && setCursor(i)}
              onClick={(e) => choose(i, { altKey: e.altKey, metaKey: e.metaKey })}
            >
              <span className="dd-check">{it.checked ? '✓' : ''}</span>
              {it.icon && <span className="dd-icon">{it.icon}</span>}
              <span className="dd-label">{it.label}</span>
              {it.hint && <span className="dd-hint">{it.hint}</span>}
              {it.kbd && <kbd>{it.kbd}</kbd>}
              {it.action && (
                <button
                  className="dd-action"
                  title={it.action.title}
                  onClick={(e) => {
                    e.stopPropagation()
                    it.action!.onClick()
                  }}
                >
                  {it.action.label}
                </button>
              )}
            </div>
          )
        })}
      </div>
    </div>,
    document.body,
  )
}

function isItem(it: DropdownItem): it is Item {
  return it.type === undefined || it.type === 'item'
}

/**
 * Filters items by label/hint/keywords. A section header is kept only above
 * matching items; separators are dropped while filtering.
 */
export function filterItems(items: DropdownItem[], query: string): DropdownItem[] {
  const q = query.trim().toLowerCase()
  if (!q) return items
  const out: DropdownItem[] = []
  let section: DropdownItem | null = null
  for (const it of items) {
    if (it.type === 'separator') continue
    if (it.type === 'section') {
      section = it
      continue
    }
    if (!`${it.label} ${it.hint ?? ''} ${it.keywords ?? ''}`.toLowerCase().includes(q)) continue
    if (section) {
      out.push(section)
      section = null
    }
    out.push(it)
  }
  return out
}
