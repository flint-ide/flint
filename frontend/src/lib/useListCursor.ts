// The highlighted row of a keyboard-driven list (pickers, menus, popups).
import { useCallback, useEffect, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { flushSync } from 'react-dom'

export interface ListCursor<T extends HTMLElement> {
  /** The scrolling list; its highlighted row matches `selector`. */
  listRef: RefObject<T | null>
  cursor: number
  /** Keys and resets: the new row is scrolled into view. */
  setCursor: Dispatch<SetStateAction<number>>
  /** The pointer is over row i. Stable, so memoized rows can take it. */
  hover: (i: number) => void
}

/**
 * Arrow keys move the cursor and scroll it into view; the pointer moves it in the
 * frame it moved in and never scrolls (the list would slide under a still pointer
 * and hover yet another row).
 */
export function useListCursor<T extends HTMLElement = HTMLDivElement>(initial = 0, selector = '.selected'): ListCursor<T> {
  const listRef = useRef<T>(null)
  const [cursor, setCursor] = useState(initial)
  const cursorRef = useRef(cursor)
  cursorRef.current = cursor
  const byPointer = useRef(false)

  useEffect(() => {
    if (byPointer.current) {
      byPointer.current = false
      return
    }
    listRef.current?.querySelector(selector)?.scrollIntoView({ block: 'nearest' })
  }, [cursor, selector])

  const hover = useCallback((i: number) => {
    if (i === cursorRef.current) return
    cursorRef.current = i
    byPointer.current = true
    // mousemove is a continuous event: React would render the change in a later task,
    // after the browser painted, so the highlight trailed the pointer by a frame.
    flushSync(() => setCursor(i))
  }, [])

  return { listRef, cursor, setCursor, hover }
}
