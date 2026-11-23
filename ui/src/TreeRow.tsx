import type { ComponentProps, MouseEvent, ReactNode } from 'react'
import { classNames, treeIndent, type TwistyState, twistyGlyph } from './treeIndent'

export type TreeRowProps = Omit<ComponentProps<'div'>, 'children'> & {
  /** The name. A string is cut with an ellipsis; pass an inline editor while renaming. */
  label: ReactNode
  /** Nesting level: 8 px + 14 px per level, like the file tree. */
  depth?: number
  /** Left padding in px instead of `depth`, for a layout that isn't on the 14 px step. */
  indent?: number
  /** Open/closed triangle; 'none' keeps its space so leaves line up with folders. Omit for flat lists. */
  twisty?: TwistyState
  /** Makes the triangle its own button (with aria-expanded) when it does something else than the row click. */
  onTwistyClick?: (e: MouseEvent<HTMLButtonElement>) => void
  /** Accessible name of that button; default "Expand"/"Collapse". */
  twistyLabel?: string
  /** A control before the icon, e.g. the Checkbox that ticks a file in. Its clicks don't reach the row. */
  leading?: ReactNode
  /** A state badge between the leading control and the icon (an agent's or task's state). */
  status?: ReactNode
  icon?: ReactNode
  /** Dimmed text after the label (a folder path, a date); <b> in it is highlighted. Hidden on hover when there are actions. */
  hint?: ReactNode
  /** Extra inline content after the hint (a line number, a conflict count). */
  children?: ReactNode
  /** Right-aligned count: a number becomes a small muted count (a sidebar's counts stay quiet). Hidden while the actions show. */
  badge?: ReactNode
  /** Buttons shown on hover or keyboard focus, right-aligned. Their clicks don't reach the row. */
  actions?: ReactNode
  /** 'always' keeps the actions visible next to the hint and badge instead of only on hover/focus. */
  actionsVisible?: 'hover' | 'always'
  /** The row of the open editor. */
  active?: boolean
  /** Selected; brighter while the tree container has focus. */
  selected?: boolean
  /** Always-bright highlight for a listbox whose focus stays in an input (a picker's cursor row). */
  current?: boolean
  /** Absolutely positioned at the top of a virtualised list: pass top/transform in `style`. */
  virtual?: boolean
  /** 'group' for a bold group header, 'root' for the small caps of a workspace root. */
  emphasis?: 'group' | 'root'
  /** File names get the list's file colour (--fg-name), folders and the rest the text colour. */
  file?: boolean
  /** Renaming in place: above its neighbours (the error popover) and no pointer cursor. */
  editing?: boolean
}

const stop = (e: MouseEvent) => e.stopPropagation()

/** The 22 px row of every sidebar list and tree: twisty, icon, name, hint, count and hover actions. */
export function TreeRow({
  label, depth, indent, twisty, onTwistyClick, twistyLabel, leading, status, icon, hint, children, badge, actions, actionsVisible,
  active, selected, current, virtual, emphasis, file, editing, className, style, ...rest
}: TreeRowProps) {
  const expandable = twisty === 'open' || twisty === 'closed'
  // The row carries aria-expanded only when the caller made it a treeitem/row; a plain div may not have it.
  const rowExpanded = expandable && !onTwistyClick && rest.role ? twisty === 'open' : undefined
  return (
    <div
      aria-expanded={rowExpanded}
      {...rest}
      className={classNames('fui-tree-row', className)}
      data-virtual={virtual || undefined}
      data-active={active || undefined}
      data-selected={selected || undefined}
      data-current={current || undefined}
      data-actions={actions && actionsVisible === 'always' ? 'always' : undefined}
      data-emphasis={emphasis}
      data-file={file || undefined}
      data-editing={editing || undefined}
      style={{ paddingLeft: treeIndent(depth, indent), ...style }}
    >
      {twisty && <Twisty state={twisty} label={twistyLabel} onClick={onTwistyClick} />}
      {leading && (
        <span className="fui-tree-leading" onClick={stop} onDoubleClick={stop}>
          {leading}
        </span>
      )}
      {status && <span className="fui-tree-status">{status}</span>}
      {icon && <span className="fui-tree-icon">{icon}</span>}
      <span className="fui-tree-label">{label}</span>
      {hint && <span className="fui-tree-hint">{hint}</span>}
      {children}
      {actions && (
        <span className="fui-tree-actions" onClick={stop} onDoubleClick={stop}>
          {actions}
        </span>
      )}
      {badge !== undefined && badge !== null && badge !== false && (
        <span className="fui-tree-badge">{typeof badge === 'number' ? <span className="fui-tree-count">{badge}</span> : badge}</span>
      )}
    </div>
  )
}

function Twisty({ state, label, onClick }: { state: TwistyState; label?: string; onClick?: (e: MouseEvent<HTMLButtonElement>) => void }) {
  const glyph = twistyGlyph(state)
  if (!onClick || state === 'none') {
    return (
      <span className="fui-twisty" aria-hidden>
        {glyph}
      </span>
    )
  }
  const open = state === 'open'
  return (
    <button
      type="button"
      className="fui-twisty"
      aria-expanded={open}
      aria-label={label ?? (open ? 'Collapse' : 'Expand')}
      onClick={(e) => {
        e.stopPropagation() // the row's own click does something else
        onClick(e)
      }}
      onDoubleClick={stop}
    >
      {glyph}
    </button>
  )
}
