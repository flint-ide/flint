// A context menu for lists and trees: one menu shared by every row, opened at the
// pointer by a right-click (or Shift+F10 / the menu key) or under a "more" button.
// WunderUI has no context menu, so this is its DropdownMenu anchored to an
// invisible point moved to where the menu should open.
//
//   <ContextMenuProvider>
//     <Row onContextMenu={menu.onContextMenu([{ label: 'Copy', onSelect: copy }])} />
//   </ContextMenuProvider>
//
// with `const menu = useContextMenu()` inside the provider.
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@wunderui/react'
import { createContext, type MouseEvent, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { contextMenuPoint, type ContextMenuEntry, type ContextMenuItems, type Point, underPoint, visibleEntries } from './menuEntries'

type OpenAt = (items: ContextMenuItems, at: Point) => void

export type ContextMenuApi = {
  /** An onContextMenu handler that opens these entries at the pointer. */
  readonly onContextMenu: (items: ContextMenuItems) => (e: MouseEvent<HTMLElement>) => void
  /** An onClick handler for a "more" button: opens the entries under the button. */
  readonly onOpenUnder: (items: ContextMenuItems) => (e: MouseEvent<HTMLElement>) => void
  /** Opens the entries at a point in viewport coordinates. */
  readonly openAt: OpenAt
}

type MenuState = { readonly entries: readonly ContextMenuEntry[]; readonly at: Point; readonly open: boolean }

const CLOSED: MenuState = { entries: [], at: { x: 0, y: 0 }, open: false }
const MenuContext = createContext<OpenAt | null>(null)

export function useContextMenu(): ContextMenuApi {
  const openAt = useContext(MenuContext)
  if (!openAt) throw new Error('useContextMenu() needs a <ContextMenuProvider> above it')
  return useMemo<ContextMenuApi>(
    () => ({
      openAt,
      onContextMenu: (items) => (e) => {
        e.preventDefault()
        e.stopPropagation()
        openAt(items, contextMenuPoint({ x: e.clientX, y: e.clientY }, e.currentTarget.getBoundingClientRect()))
      },
      onOpenUnder: (items) => (e) => {
        e.stopPropagation()
        openAt(items, underPoint(e.currentTarget.getBoundingClientRect()))
      },
    }),
    [openAt],
  )
}

export type ContextMenuProviderProps = {
  readonly children: ReactNode
  /** Extra class for the menu popup, e.g. 'fui-dense' for 24 px rows. */
  readonly className?: string
}

/** Holds the one menu its descendants open with useContextMenu(). */
export function ContextMenuProvider({ children, className }: ContextMenuProviderProps) {
  const [menu, setMenu] = useState<MenuState>(CLOSED)
  const openAt = useCallback<OpenAt>((items, at) => {
    const entries = visibleEntries(items)
    if (entries.length > 0) setMenu({ entries, at, open: true })
  }, [])
  const close = useCallback(() => setMenu((m) => (m.open ? { ...m, open: false } : m)), [])

  // Like a native context menu, it closes when anything scrolls or the window
  // loses focus or changes size: the point it opened at no longer means anything.
  useEffect(() => {
    if (!menu.open) return undefined
    window.addEventListener('scroll', close, true)
    window.addEventListener('blur', close)
    window.addEventListener('resize', close)
    return () => {
      window.removeEventListener('scroll', close, true)
      window.removeEventListener('blur', close)
      window.removeEventListener('resize', close)
    }
  }, [menu.open, close])

  return (
    <MenuContext.Provider value={openAt}>
      {children}
      <DropdownMenu open={menu.open} onOpenChange={(open) => (open ? undefined : close())}>
        <DropdownMenuTrigger
          render={
            <button type="button" tabIndex={-1} aria-hidden className="fui-menu-anchor" style={{ left: menu.at.x, top: menu.at.y }} />
          }
        />
        <DropdownMenuContent className={className} align="start" sideOffset={2}>
          {menu.entries.map((entry, i) => (entry === 'separator' ? <DropdownMenuSeparator key={`sep${i}`} /> : <Entry key={`${i}:${entry.label}`} entry={entry} />))}
        </DropdownMenuContent>
      </DropdownMenu>
    </MenuContext.Provider>
  )
}

function Entry({ entry }: { entry: Exclude<ContextMenuEntry, 'separator'> }) {
  return (
    <DropdownMenuItem variant={entry.danger ? 'destructive' : 'default'} disabled={entry.disabled} icon={entry.icon} onClick={entry.onSelect}>
      {entry.label}
      {entry.hint && <DropdownMenuShortcut>{entry.hint}</DropdownMenuShortcut>}
    </DropdownMenuItem>
  )
}
