import { createStore } from '../lib/store'
import type { DropdownItem } from '../components/ui/Dropdown'

interface ContextMenuState {
  menu: { x: number; y: number; items: DropdownItem[]; seq: number } | null
}

export const useContextMenu = createStore<ContextMenuState>(() => ({ menu: null }))

let seq = 0

/** Shows a menu at the mouse position (right click); one at a time. */
export function openContextMenu(x: number, y: number, items: DropdownItem[]): void {
  useContextMenu.setState({ menu: { x, y, items, seq: ++seq } })
}

export function closeContextMenu(): void {
  useContextMenu.setState({ menu: null })
}
