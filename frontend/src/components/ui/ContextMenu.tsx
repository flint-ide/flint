import { useEffect, useRef } from 'react'
import { returnFocusIfLost } from '../../lib/returnFocus'
import { closeContextMenu, useContextMenu } from '../../store/contextMenu'
import { AppMenu } from './AppMenu'

type Menu = NonNullable<ReturnType<typeof useContextMenu.getState>['menu']>

/** The menu opened by openContextMenu, at the click position (the UI kit's menu, AppMenu). */
export function ContextMenu() {
  const menu = useContextMenu((s) => s.menu)
  const shown = useRef<Menu | null>(null)

  // Once it is gone: focus that went down with it (a picked row) goes back to what had it.
  useEffect(() => {
    if (menu) {
      shown.current = menu
      return
    }
    const closed = shown.current
    shown.current = null
    if (closed) returnFocusIfLost(closed.returnFocus)
  }, [menu])

  if (!menu) return null
  return (
    <AppMenu
      key={menu.seq}
      at={{ left: menu.x, top: menu.y, width: 0, height: 0 }}
      items={menu.items}
      align="start"
      minWidth={menu.minWidth}
      emptyText="Nothing here"
      onClose={closeContextMenu}
    />
  )
}
