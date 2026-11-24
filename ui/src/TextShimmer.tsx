import type { ElementType, ReactNode } from 'react'
import { classNames } from './treeIndent'

export type TextShimmerProps = {
  /** The element to render; a span keeps it inline. */
  as?: ElementType
  /** Sweep the highlight across the text. False, or reduced motion, leaves plain muted text. */
  active?: boolean
  className?: string
  children: ReactNode
}

/**
 * Text with a soft highlight sweeping across it, for a "Thinking…" label while an agent works.
 * The text stays in the DOM and selectable, so screen readers read it as plain text.
 */
export function TextShimmer({ as: Tag = 'span', active = true, className, children }: TextShimmerProps) {
  return (
    <Tag className={classNames('fui-shimmer', active && 'fui-shimmer-active', className)}>{children}</Tag>
  )
}
