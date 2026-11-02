// A stable function that always calls the latest `fn`, for props of memoized rows.
import { useCallback, useLayoutEffect, useRef } from 'react'

export function useEventCallback<A extends unknown[], R>(fn: (...args: A) => R): (...args: A) => R {
  const latest = useRef(fn)
  useLayoutEffect(() => {
    latest.current = fn
  })
  return useCallback((...args: A) => latest.current(...args), [])
}
