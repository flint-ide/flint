import { useState } from 'react'
import { closeContextMenu, useContextMenu } from '../../store/contextMenu'
import { Menu, type DropdownItem } from './Dropdown'

/** The menu opened by openContextMenu, anchored at the click position. */
export function ContextMenu() {
  const menu = useContextMenu((s) => s.menu)
  if (!menu) return null
  return <AnchoredMenu key={menu.seq} x={menu.x} y={menu.y} items={menu.items} />
}

function AnchoredMenu({ x, y, items }: { x: number; y: number; items: DropdownItem[] }) {
  // A zero-size element at the pointer stands in for the trigger a dropdown has.
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  return (
    <>
      <span ref={setAnchor} className="context-anchor" style={{ left: x, top: y }} />
      {anchor && (
        <Menu
          anchor={anchor}
          items={items}
          align="start"
          searchable={false}
          placeholder=""
          minWidth={200}
          emptyText="Nothing here"
          onClose={closeContextMenu}
        />
      )}
    </>
  )
}
