import type { ComponentProps, CSSProperties, KeyboardEvent, MouseEvent, ReactNode } from 'react'
import { isActivationKey, visibleParts } from './listParts'
import { classNames } from './treeIndent'

export type ListItemProps = Omit<ComponentProps<'div'>, 'children'> & {
  /** The first line's text (a title, a SQL statement). Cut with an ellipsis. */
  label: ReactNode
  /** Before the label: a status badge, a CI glyph, an icon. */
  leading?: ReactNode
  /** Right after the label: markers such as "lead" or "failed". */
  badges?: ReactNode
  /** Right-aligned on the first line: a state badge, a count, a time. Hidden while hover actions show. */
  trailing?: ReactNode
  /** The muted second line; empty parts are dropped and the rest separated by a dot. */
  meta?: readonly ReactNode[]
  /** Buttons at the end of the first line. Their clicks don't reach the row. */
  actions?: ReactNode
  /** 'hover' (default): only on hover, keyboard focus inside or selection. 'always': always shown. */
  showActions?: 'hover' | 'always'
  /** More lines under the meta line. */
  children?: ReactNode
  /** The open item (its editor tab is active). */
  active?: boolean
  /** The keyboard cursor of a list, or a selection. */
  selected?: boolean
  /** Label in the monospace font (SQL, paths). */
  mono?: boolean
  /** Label in the muted colour (a failed statement). */
  dimmed?: boolean
  /** One tight line without the divider: a sub-row inside a card. */
  compact?: boolean
  /** The 1 px line under the item; on by default unless compact. */
  divider?: boolean
  /** Extra left padding (px) of the meta line, to line it up with the label after a twisty or an icon. */
  inset?: number
}

const stop = (e: MouseEvent) => e.stopPropagation()

/**
 * A two-line list row: leading status, title, badges and a trailing part, then a
 * muted meta line. Clickable rows are tab stops that Enter and Space activate.
 */
export function ListItem({
  label, leading, badges, trailing, meta, actions, showActions = 'hover', children, active, selected, mono, dimmed,
  compact, divider, inset, className, style, onClick, onKeyDown, tabIndex, ...rest
}: ListItemProps) {
  const parts = visibleParts(meta ?? [])
  const clickable = !!onClick
  const lined = divider ?? !compact
  const keyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    onKeyDown?.(e)
    // Only the row itself: Enter on a button inside belongs to that button.
    if (e.defaultPrevented || !clickable || e.target !== e.currentTarget || !isActivationKey(e.key)) return
    e.preventDefault()
    e.currentTarget.click()
  }
  const vars = inset === undefined ? style : ({ ...style, '--list-inset': `${inset}px` } as CSSProperties)
  return (
    <div
      {...rest}
      className={classNames('fui-list-item', className)}
      style={vars}
      tabIndex={tabIndex ?? (clickable ? 0 : undefined)}
      onClick={onClick}
      onKeyDown={keyDown}
      data-clickable={clickable || undefined}
      data-active={active || undefined}
      data-selected={selected || undefined}
      data-mono={mono || undefined}
      data-dimmed={dimmed || undefined}
      data-compact={compact || undefined}
      data-divider={lined || undefined}
      data-actions={actions ? showActions : undefined}
    >
      <div className="fui-list-main">
        {leading && <span className="fui-list-leading">{leading}</span>}
        <span className="fui-list-label">{label}</span>
        {badges && <span className="fui-list-badges">{badges}</span>}
        {(trailing || actions) && (
          <span className="fui-list-end">
            {trailing && <span className="fui-list-trailing">{trailing}</span>}
            {actions && (
              <span className="fui-list-actions" onClick={stop} onDoubleClick={stop}>
                {actions}
              </span>
            )}
          </span>
        )}
      </div>
      {parts.length > 0 && (
        <div className="fui-list-meta">
          {parts.map((part, i) => (
            <span key={i} className="fui-list-meta-part">
              {part}
            </span>
          ))}
        </div>
      )}
      {children}
    </div>
  )
}

export type ListGroupProps = Omit<ComponentProps<'div'>, 'title'> & {
  /** The group's name, bold. */
  label?: ReactNode
  /** A muted summary after the name (or under it with `stacked`). */
  meta?: ReactNode
  /** Buttons at the end of the head. */
  actions?: ReactNode
  /** The summary on its own line under the name. */
  stacked?: boolean
  /** Tooltip of the head. */
  tooltip?: string
}

/** Items that belong together (attempts of one task, repositories of one agent) behind an accent line and a head. */
export function ListGroup({ label, meta, actions, stacked, tooltip, className, children, ...rest }: ListGroupProps) {
  return (
    <div {...rest} className={classNames('fui-list-group', className)}>
      <div className="fui-list-group-head" data-stacked={stacked || undefined} title={tooltip}>
        {label && <span className="fui-list-group-label">{label}</span>}
        {meta && <span className="fui-list-group-meta">{meta}</span>}
        {actions}
      </div>
      {children}
    </div>
  )
}
