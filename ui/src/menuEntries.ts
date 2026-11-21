// Pure logic behind ContextMenu.tsx: which entries a menu shows and where it opens.
import type { ReactNode } from 'react'

/** One menu entry, or 'separator' for a line between groups. */
export type ContextMenuEntry =
  | 'separator'
  | {
      readonly label: string
      readonly onSelect: () => void
      /** The shortcut shown at the end of the row ("⌘C"); visual only. */
      readonly hint?: string
      /** Destructive entries say so in the label too ("Delete pod"). */
      readonly danger?: boolean
      readonly disabled?: boolean
      /** Leading icon, decorative. */
      readonly icon?: ReactNode
    }

/** What a caller passes: falsy items drop out, so entries can be conditional. */
export type ContextMenuItems = readonly (ContextMenuEntry | false | null | undefined)[]

export type Point = { readonly x: number; readonly y: number }
export type Box = { readonly left: number; readonly top: number; readonly right: number; readonly bottom: number }

/**
 * The entries to show: falsy items removed, separators never first, last or
 * doubled. Returns [] when nothing but separators is left.
 */
export function visibleEntries(items: ContextMenuItems): readonly ContextMenuEntry[] {
  const kept = items.filter((e): e is ContextMenuEntry => !!e)
  const out = kept.filter((e, i) => e !== 'separator' || (i > 0 && kept[i - 1] !== 'separator'))
  const first = out.findIndex((e) => e !== 'separator')
  if (first < 0) return []
  let last = out.length - 1
  while (out[last] === 'separator') last--
  return out.slice(first, last + 1)
}

/**
 * Where a context menu opens. A pointer opens it at the pointer; the keyboard
 * (Shift+F10, the menu key) fires contextmenu at (0, 0), so it opens at the
 * focused element's lower left corner instead.
 */
export function contextMenuPoint(pointer: Point, target: Box | null): Point {
  const fromKeyboard = pointer.x === 0 && pointer.y === 0
  if (fromKeyboard && target) return { x: target.left + 8, y: target.bottom }
  return pointer
}

/** Where a menu opened by a button opens: under it, aligned to its end. */
export function underPoint(button: Box): Point {
  return { x: button.right - 4, y: button.bottom }
}
