import type { ComponentProps } from 'react'
import { classNames } from './treeIndent'

/**
 * A count as a small muted number: the items of a section, a column or a log. Quiet on purpose, not a pill, so the
 * heads of a sidebar or a board do not compete with the items under them. TreeRow and TreeSection use it for a
 * numeric `badge` / `count`.
 */
export function Count({ className, ...rest }: ComponentProps<'span'>) {
  return <span {...rest} className={classNames('fui-count', className)} />
}
