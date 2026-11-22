import { createStore } from '../lib/store'
import { noteMenuReturn } from '../lib/returnFocus'
import type { DropdownItem } from '../components/ui/Dropdown'

interface ContextMenu {
  x: number
  y: number
  items: DropdownItem[]
  minWidth: number
  /** Called once the menu closes (picked, Escape or a click elsewhere), e.g. to refocus a list. */
  onClose?: () => void
  /** What had focus when it opened: focus goes back there if it closes with focus lost. */
  returnFocus: HTMLElement | null
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
  const previous = useContextMenu.getState().menu
  previous?.onClose?.()
  const focused = typeof document === 'undefined' ? null : document.activeElement
  // A right-click while a menu is open: focus is on that menu's row, so keep its target.
  const returnFocus = previous?.returnFocus ?? (typeof HTMLElement !== 'undefined' && focused instanceof HTMLElement ? focused : null)
  useContextMenu.setState({ menu: { x, y, items, minWidth: options.minWidth ?? 200, onClose: options.onClose, returnFocus, seq: ++seq } })
}

export function closeContextMenu(): void {
  const menu = useContextMenu.getState().menu
  if (!menu) return
  noteMenuReturn(menu.returnFocus)
  useContextMenu.setState({ menu: null })
  menu.onClose?.()
}
