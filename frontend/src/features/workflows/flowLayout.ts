// How the steps of a workflow lay out in the room they have. Pure, so the rules are tested; StepFlow measures
// its width and applies them.
//
// The steps are ONE horizontal line of blocks joined by connector lines; they never wrap into a second row.
// When the line does not fit, the blocks shrink to NODE_MIN and the line scrolls sideways inside its own area.
// Only when there is no room for even two blocks side by side does it become a vertical stepper.

/** The narrowest a step block gets before the line scrolls, and the widest it grows (CSS: .wf-node). */
export const NODE_MIN = 196
export const NODE_MAX = 300
/** The shortest connector between two blocks (CSS: .wf-link; it grows to fill spare room). */
export const LINK_WIDTH = 28

export interface FlowLayout {
  /** One block per line with the connector running down the side: no room for two blocks in a row. */
  vertical: boolean
  /** The blocks at their narrowest still do not fit: the line scrolls sideways. */
  overflow: boolean
}

/** The room `count` blocks need in a row at their narrowest. */
export const rowWidth = (count: number): number => (count <= 0 ? 0 : count * NODE_MIN + (count - 1) * LINK_WIDTH)

/** The layout for `count` steps in a container `width` px wide (0: not measured yet, drawn as a row). */
export function flowLayout(width: number, count: number): FlowLayout {
  if (width <= 0 || count <= 1) return { vertical: false, overflow: count === 1 && width > 0 && width < NODE_MIN }
  if (width < rowWidth(2)) return { vertical: true, overflow: false }
  return { vertical: false, overflow: width < rowWidth(count) }
}

/**
 * The step a key moves to from `index` among `count` steps: the arrows step back and forth (left/up, right/down,
 * so the same keys work in a row and in the vertical stepper), Home and End jump to the ends. null for any other
 * key, and at an end (no wrapping: a pipeline has a start and a finish).
 */
export function stepForKey(key: string, index: number, count: number): number | null {
  if (count <= 0) return null
  let next: number
  switch (key) {
    case 'ArrowRight':
    case 'ArrowDown':
      next = index + 1
      break
    case 'ArrowLeft':
    case 'ArrowUp':
      next = index - 1
      break
    case 'Home':
      next = 0
      break
    case 'End':
      next = count - 1
      break
    default:
      return null
  }
  return next < 0 || next >= count || next === index ? null : next
}

/**
 * The scrollLeft that centres a block in a scrolling line, clamped to what the line can scroll.
 * nodeLeft/nodeWidth: the block's offset and width inside the line; view: the line's visible width; total: its scroll width.
 */
export function centredScroll(nodeLeft: number, nodeWidth: number, view: number, total: number): number {
  const max = Math.max(0, total - view)
  return Math.round(Math.min(max, Math.max(0, nodeLeft - (view - nodeWidth) / 2)))
}

/** Which ends of a scrolling line hide blocks: the fades and the previous/next buttons follow this. 1px of slack for sub-pixel scroll. */
export function scrollEdges(scrollLeft: number, view: number, total: number): { before: boolean; after: boolean } {
  return { before: scrollLeft > 1, after: scrollLeft + view < total - 1 }
}
