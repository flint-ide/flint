// Fixed-row-height list virtualization: only rows near the viewport are rendered.
import { useCallback, useLayoutEffect, useState } from 'react'

export interface VirtualItem {
  index: number
  /** Offset of the row's top edge inside the scroll content, in px. */
  start: number
}

interface Options {
  count: number
  itemSize: number
  /** Extra rows rendered above and below the viewport. */
  overscan: number
}

/** First and last (exclusive) row indices to render for a scroll position. */
export function visibleRange(
  scrollTop: number,
  viewport: number,
  count: number,
  itemSize: number,
  overscan: number,
): [number, number] {
  if (count === 0 || itemSize <= 0) return [0, 0]
  // The list may shrink before the scroll position catches up; keep the window inside it.
  const top = Math.min(Math.max(0, scrollTop), Math.max(0, count * itemSize - viewport))
  const first = Math.floor(top / itemSize)
  const last = Math.ceil((top + viewport) / itemSize)
  return [Math.max(0, first - overscan), Math.min(count, last + overscan)]
}

/**
 * Tracks the scroll container passed to `scrollRef` and returns the rows to render.
 * Rows are absolutely positioned at `start` inside a box of `totalSize` height.
 */
export function useVirtualList({ count, itemSize, overscan }: Options) {
  const [el, setEl] = useState<HTMLElement | null>(null)
  const [viewport, setViewport] = useState({ top: 0, height: 0 })

  useLayoutEffect(() => {
    if (!el) return
    const measure = () => setViewport({ top: el.scrollTop, height: el.clientHeight })
    measure()
    el.addEventListener('scroll', measure, { passive: true })
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => {
      el.removeEventListener('scroll', measure)
      observer.disconnect()
    }
  }, [el])

  // A shorter list clamps scrollTop without firing a scroll event: re-measure.
  useLayoutEffect(() => {
    if (el) setViewport({ top: el.scrollTop, height: el.clientHeight })
  }, [el, count])

  const [from, to] = visibleRange(viewport.top, viewport.height, count, itemSize, overscan)
  const items: VirtualItem[] = []
  for (let i = from; i < to; i++) items.push({ index: i, start: i * itemSize })

  const scrollRef = useCallback((node: HTMLElement | null) => setEl(node), [])
  return { scrollRef, totalSize: count * itemSize, items }
}
