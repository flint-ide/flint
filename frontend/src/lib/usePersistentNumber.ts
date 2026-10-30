import { useCallback, useState } from 'react'

/** A number kept in localStorage (layout sizes); falls back silently when storage is unavailable. */
export function usePersistentNumber(key: string, initial: number): [number, (n: number) => void] {
  const [value, setValue] = useState(() => {
    try {
      const stored = Number(localStorage.getItem(key))
      return Number.isFinite(stored) && stored > 0 ? stored : initial
    } catch {
      return initial
    }
  })
  const update = useCallback(
    (n: number) => {
      setValue(n)
      try {
        localStorage.setItem(key, String(n))
      } catch {
        // Storage blocked: keep the value for this session only.
      }
    },
    [key],
  )
  return [value, update]
}
