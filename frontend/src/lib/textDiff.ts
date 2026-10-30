export interface TextChange {
  from: number
  to: number
  insert: string
}

/**
 * Returns the smallest single replacement turning `a` into `b` (common prefix and
 * suffix kept), so external reloads don't reset scroll, folds or the cursor.
 */
export function minimalChange(a: string, b: string): TextChange | null {
  if (a === b) return null
  let start = 0
  const max = Math.min(a.length, b.length)
  while (start < max && a.charCodeAt(start) === b.charCodeAt(start)) start++
  let endA = a.length
  let endB = b.length
  while (endA > start && endB > start && a.charCodeAt(endA - 1) === b.charCodeAt(endB - 1)) {
    endA--
    endB--
  }
  return { from: start, to: endA, insert: b.slice(start, endB) }
}
