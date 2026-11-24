/** Logic for `StatusTracker`. Pure and DOM-free, so it is tested with node:test. */

export type TrackerState = 'ok' | 'failed' | 'warn' | 'running' | 'skipped' | 'none'

export type TrackerItem = {
  id: string
  state: TrackerState
  title?: string
}

/** One block of the strip: a real item or a `none` placeholder on the left. */
export type TrackerSlot = TrackerItem & { placeholder: boolean }

/** How many runs to show: `max` floored, everything for Infinity, nothing for 0, negatives and NaN. */
function shownCount(max: number, available: number): number {
  if (max === Number.POSITIVE_INFINITY) return available
  return Number.isFinite(max) ? Math.max(0, Math.floor(max)) : 0
}

/** The newest `count` items; `slice(-0)` would be everything, so zero is handled here. */
function newest<T>(items: readonly T[], count: number): T[] {
  return count === 0 ? [] : items.slice(-count)
}

/** The newest `max` items, with `none` placeholders in front when there are fewer. */
export function trackerSlots(items: readonly TrackerItem[], max: number): TrackerSlot[] {
  const count = shownCount(max, items.length)
  const shown = newest(items, count).map((item) => ({ ...item, placeholder: false }))
  const pad = Array.from({ length: count - shown.length }, (_, i): TrackerSlot => ({ id: `pad-${i}`, state: 'none', placeholder: true }))
  return [...pad, ...shown]
}

const SUMMARY_ORDER: readonly (readonly [TrackerState, string])[] = [
  ['ok', 'passed'],
  ['failed', 'failed'],
  ['warn', 'with warnings'],
  ['running', 'running'],
  ['skipped', 'skipped'],
]

/** "Last 30 runs: 27 passed, 2 failed, 1 running"; "No runs" when there is nothing to show. */
export function trackerSummary(items: readonly TrackerItem[], max: number): string {
  const shown = newest(items, shownCount(max, items.length))
  if (shown.length === 0) return 'No runs'
  const parts = SUMMARY_ORDER.map(([state, word]) => [shown.filter((i) => i.state === state).length, word] as const)
    .filter(([n]) => n > 0)
    .map(([n, word]) => `${n} ${word}`)
  const head = `Last ${shown.length} ${shown.length === 1 ? 'run' : 'runs'}`
  return parts.length > 0 ? `${head}: ${parts.join(', ')}` : head
}

/** The block that takes focus after `key`, or null when the key is not one the strip handles. */
export function rovingIndex(current: number, key: string, count: number): number | null {
  if (count <= 0) return null
  const last = count - 1
  switch (key) {
    case 'ArrowRight':
      return Math.min(current + 1, last)
    case 'ArrowLeft':
      return Math.max(current - 1, 0)
    case 'Home':
      return 0
    case 'End':
      return last
    default:
      return null
  }
}
