import { useEffect, useRef } from 'react'

/** Calls fn when the window regains focus (while enabled), e.g. to refresh data. */
export function useWindowFocus(fn: () => void, enabled = true): void {
  const latest = useRef(fn)
  latest.current = fn
  useEffect(() => {
    if (!enabled) return
    const onFocus = () => latest.current()
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [enabled])
}
