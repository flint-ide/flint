/** Pure helpers for the composite components; no DOM, so they are unit tested. */

/** Items whose label contains every space-separated word of query (case-insensitive). */
export function filterByLabel<T extends { label: string }>(items: readonly T[], query: string): readonly T[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return items
  return items.filter((item) => {
    const label = item.label.toLowerCase()
    return words.every((w) => label.includes(w))
  })
}

/** Moves an active index by delta within [0, length), clamped at both ends. */
export function moveIndex(index: number, delta: number, length: number): number {
  if (length <= 0) return 0
  return Math.min(Math.max(index + delta, 0), length - 1)
}

/** How many of `blocks` diff-stat blocks are additions (the rest are deletions). */
export function addedBlocks(added: number, removed: number, blocks = 5): number {
  const total = added + removed
  if (total <= 0) return 0
  return Math.round((added / total) * blocks)
}
