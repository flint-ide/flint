import type { ReactNode } from 'react'
import { classNames } from './treeIndent'

export type PanelHeaderProps = {
  /** The small-caps title; a node (a repository switcher) takes its place and keeps its own case. */
  title: ReactNode
  /** Tooltip of the title, e.g. the full paths of the workspace roots. */
  tooltip?: string
  /** Trailing controls: ghost size="sm" IconButtons, a Badge, a small Segment. */
  actions?: ReactNode
  /** A muted line under the header (the branch of the Commit panel). */
  children?: ReactNode
  className?: string
}

/** The 32 px header of a sidebar panel: title on the left, small ghost buttons on the right. */
export function PanelHeader({ title, tooltip, actions, children, className }: PanelHeaderProps) {
  return (
    <>
      <div className={classNames('fui-panel-header', className)}>
        <span className="fui-panel-title" data-custom={typeof title === 'string' ? undefined : true} title={tooltip}>
          {title}
        </span>
        {actions}
      </div>
      {children && <div className="fui-panel-sub">{children}</div>}
    </>
  )
}
