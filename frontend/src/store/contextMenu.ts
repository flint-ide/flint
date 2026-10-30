import { createStore } from '../lib/store'
import type { DropdownItem } from '../components/ui/Dropdown'

interface ContextMenu {
  x: number
  y: number
  items: DropdownItem[]
  minWidth: number
  /** Called once the menu closes (picked, Escape or a click elsewhere), e.g. to refocus a list. */
  onClose?: () => void
  seq: number
}

interface ContextMenuState {
  menu: ContextMenu | null
}

export const useContextMenu = createStore<ContextMenuState>(() => ({ menu: null }))

let seq = 0

export interface ContextMenuOptions {
  minWidth?: number
  onClose?: () => void
}

/** Shows a menu at the mouse position (right click); one at a time. */
export function openContextMenu(x: number, y: number, items: DropdownItem[], options: ContextMenuOptions = {}): void {
  useContextMenu.getState().menu?.onClose?.()
  useContextMenu.setState({ menu: { x, y, items, minWidth: options.minWidth ?? 200, onClose: options.onClose, seq: ++seq } })
}

export function closeContextMenu(): void {
  const menu = useContextMenu.getState().menu
  if (!menu) return
  useContextMenu.setState({ menu: null })
  menu.onClose?.()
}
