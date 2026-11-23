import { type ReactNode, useState } from 'react'
import { TreeRow } from './TreeRow'

export type TreeSectionProps = {
  title: ReactNode
  /** Number of items, right-aligned as a small muted count (hidden while the actions show). A string or node ("2/3") is shown as given. */
  count?: ReactNode
  /** Dimmed text after the title, e.g. the branch of a repository. */
  hint?: ReactNode
  /** A control before the title, e.g. the Checkbox that ticks the whole group. */
  leading?: ReactNode
  /** Buttons on the header, shown on hover or focus. */
  actions?: ReactNode
  /** 'always' keeps the header actions visible instead of only on hover/focus. */
  actionsVisible?: 'hover' | 'always'
  /** Controlled open state (e.g. kept in a store by key); omit it to let the section keep its own. */
  open?: boolean
  defaultOpen?: boolean
  onOpenChange?: (open: boolean) => void
  /** Nesting level of the header; give the rows inside depth + 1. */
  depth?: number
  className?: string
  /** The rows, rendered only while open. */
  children?: ReactNode
}

/**
 * A collapsible group of tree rows (Changes, Unversioned, Stashes, a PR list section): a bold
 * TreeRow header with a twisty, then the rows. WunderUI's Collapsible doesn't fit here: its trigger
 * is a button, so the header couldn't hold a checkbox or action buttons, and it is not 22 px.
 */
export function TreeSection({
  title, count, hint, leading, actions, actionsVisible, open, defaultOpen = true, onOpenChange, depth, className, children,
}: TreeSectionProps) {
  const [ownOpen, setOwnOpen] = useState(defaultOpen)
  const isOpen = open ?? ownOpen
  const toggle = () => {
    if (open === undefined) setOwnOpen(!isOpen)
    onOpenChange?.(!isOpen)
  }
  return (
    <div className={className}>
      <TreeRow
        emphasis="group"
        twisty={isOpen ? 'open' : 'closed'}
        // The twisty is the keyboard and screen reader control (a button with aria-expanded);
        // the whole header folds by mouse, as the app's groups always did.
        onTwistyClick={toggle}
        twistyLabel={typeof title === 'string' ? `${isOpen ? 'Collapse' : 'Expand'} ${title}` : undefined}
        depth={depth}
        label={title}
        hint={hint}
        leading={leading}
        actions={actions}
        actionsVisible={actionsVisible}
        badge={count}
        onClick={toggle}
      />
      {isOpen && children}
    </div>
  )
}
