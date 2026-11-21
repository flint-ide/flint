import { closeContextMenu, useContextMenu } from '../../store/contextMenu'
import { AppMenu } from './AppMenu'

/** The menu opened by openContextMenu, at the click position (the UI kit's menu, AppMenu). */
export function ContextMenu() {
  const menu = useContextMenu((s) => s.menu)
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
