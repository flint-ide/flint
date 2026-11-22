// Pure helpers of TreeRow, kept apart so they can be unit tested without a DOM.

/** Left padding of a depth-0 row, and the step per level: the file tree's 8 + depth × 14. */
export const TREE_INDENT_BASE = 8
export const TREE_INDENT_STEP = 14

export type TwistyState = 'open' | 'closed' | 'none'

/** Left padding in px of a row at `depth`; an explicit `indent` (px) wins. Negative or fractional depths are clamped. */
export function treeIndent(depth = 0, indent?: number): number {
  if (indent !== undefined && Number.isFinite(indent)) return Math.max(0, indent)
  const level = Number.isFinite(depth) ? Math.max(0, Math.floor(depth)) : 0
  return TREE_INDENT_BASE + level * TREE_INDENT_STEP
}

/** The glyph the app's trees have always drawn: ▾ open, ▸ closed, nothing (but the space) for a leaf. */
export function twistyGlyph(state: TwistyState): string {
  if (state === 'open') return '▾'
  if (state === 'closed') return '▸'
  return ''
}

/** The space-separated class list of the parts that are set. */
export function classNames(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ')
}
