import { Toolbar } from '@wunderui/react'
import type { ComponentProps } from 'react'
import { classNames } from './treeIndent'

export type ViewToolbarProps = Omit<ComponentProps<typeof Toolbar>, 'className'> & { className?: string }

/**
 * The bar across the top of an editor view (query console, diff, run output, profiler, HTTP
 * client): 32 px, a bottom border, 12 px text, controls 4 px apart. WunderUI's Toolbar underneath
 * gives role="toolbar" and arrow-key focus between ToolbarButtons; its floating card frame is
 * dropped. Put size="sm" kit controls in it; a <span className="fui-grow" /> pushes the rest right.
 */
export function ViewToolbar({ className, ...props }: ViewToolbarProps) {
  return <Toolbar {...props} className={classNames('fui-view-toolbar', className)} />
}
