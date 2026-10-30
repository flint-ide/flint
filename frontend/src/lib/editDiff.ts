// Agent edit diffs: hunks come from Go (DiffAPI), this file only shapes them for display.
import { useEffect, useState } from 'react'
import { Diff as DiffAPI, type Hunk } from '../api/backend'
import type { Diff } from './agentReducer'

export interface PreviewLine {
  kind: 'add' | 'del' | 'sep'
  text: string
}

// Diff objects are immutable reducer output, so identity is a safe cache key.
const cache = new WeakMap<Diff, Promise<Hunk[]>>()

function hunksFor(d: Diff): Promise<Hunk[]> {
  let p = cache.get(d)
  if (!p) {
    p = DiffAPI.Hunks(d.oldText, d.newText).then((h) => h ?? [])
    p.catch(() => cache.delete(d)) // retry on next mount
    cache.set(d, p)
  }
  return p
}

/** Hunks per diff, or null while the backend computes them. */
export function useHunks(diffs: Diff[]): Hunk[][] | null {
  const [state, setState] = useState<{ diffs: Diff[]; hunks: Hunk[][] } | null>(null)
  useEffect(() => {
    let current = true
    Promise.all(diffs.map(hunksFor))
      .then((hunks) => current && setState({ diffs, hunks }))
      .catch(() => undefined) // stats are decoration; leave them out
    return () => {
      current = false
    }
  }, [diffs])
  return state?.diffs === diffs ? state.hunks : null
}

export function countLines(hunks: Hunk[][]): { add: number; del: number } {
  let add = 0
  let del = 0
  for (const h of hunks.flat()) {
    add += h.toB - h.fromB
    del += h.toA - h.fromA
  }
  return { add, del }
}

/** Changed lines only (red/green), like a compact unified diff; stops after ~max lines. */
export function previewLines(d: Diff, hunks: Hunk[], max: number): PreviewLine[] {
  const a = d.oldText.split('\n')
  const b = d.newText.split('\n')
  const out: PreviewLine[] = []
  for (const h of hunks) {
    if (out.length) out.push({ kind: 'sep', text: '⋯' })
    for (const text of a.slice(h.fromA, h.toA)) out.push({ kind: 'del', text })
    for (const text of b.slice(h.fromB, h.toB)) out.push({ kind: 'add', text })
    if (out.length > max) break
  }
  return out
}
