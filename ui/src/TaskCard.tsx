import { BadgeProcessing, Card } from '@wunderui/react'
import type { ComponentProps, MouseEvent, ReactNode } from 'react'
import { ListItem } from './ListItem'
import { type CheckState, processingState, visibleParts } from './listParts'
import { classNames, type TwistyState, twistyGlyph } from './treeIndent'

export type TaskCardNote = {
  text: ReactNode
  /** 'danger' for an error, 'warn' for a problem, 'faint' for a quiet hint; muted by default. */
  tone?: 'muted' | 'faint' | 'warn' | 'danger'
  title?: string
  /** One line with an ellipsis instead of wrapping (a URL, a folder). */
  truncate?: boolean
  /** Monospace (git's progress line, a path). */
  mono?: boolean
}

export type TaskCardCI = {
  state: CheckState
  /** Default "CI". */
  label?: ReactNode
  title?: string
}

export type TaskCardProps = Omit<ComponentProps<'div'>, 'children' | 'title'> & {
  /** The title line's text. */
  label: ReactNode
  /** The state badge before the title (TaskStateBadge, a status Badge). */
  status?: ReactNode
  /** Markers after the title. */
  badges?: ReactNode
  /** Right-aligned on the title line (a priority). */
  trailing?: ReactNode
  /** The muted line under the title: branch, changes, usage… Empty parts are dropped. */
  meta?: readonly ReactNode[]
  /** Tooltip of the title lines. */
  tooltip?: string
  /** A twisty before the status, for a card whose head shows and hides its sub-rows. */
  twisty?: TwistyState
  /** Click (and Enter/Space) on the title lines. */
  onOpen?: (e: MouseEvent<HTMLDivElement>) => void
  /** The checks of the task's pull request, as a processing badge on the title line. */
  ci?: TaskCardCI | null
  /** Lines under the meta line: an error, what the item waits for. Falsy entries are skipped. */
  notes?: readonly (TaskCardNote | null | false | undefined)[]
  /** The always visible bar of ghost sm buttons. A `<span className="fui-grow" />` pushes the rest right. */
  actions?: ReactNode
  /** Sub-rows under the card (changed files, agent tasks); outside the title's click. */
  children?: ReactNode
  /** 'row' (default): a list entry with a divider. 'card': a WunderUI Card, for a board column. 'bare': no divider (inside a box of its own). */
  frame?: 'row' | 'card' | 'bare'
  /** The card is being dragged: it fades. */
  dragging?: boolean
}

/** An agent task or a work item: state, title, meta, CI, notes, actions and sub-rows. */
export function TaskCard({
  label, status, badges, trailing, meta, tooltip, twisty, onOpen, ci, notes, actions, children, frame = 'row', dragging,
  className, ...rest
}: TaskCardProps) {
  const expandable = twisty === 'open' || twisty === 'closed'
  const lines = visibleParts(notes ?? [])
  const end =
    ci || trailing ? (
      <>
        {ci && (
          <BadgeProcessing size="sm" state={processingState(ci.state)} title={ci.title}>
            {ci.label ?? 'CI'}
          </BadgeProcessing>
        )}
        {trailing}
      </>
    ) : undefined
  const body = (
    <>
      <ListItem
        label={label}
        leading={
          twisty || status ? (
            <>
              {twisty && (
                <span className="fui-twisty" aria-hidden>
                  {twistyGlyph(twisty)}
                </span>
              )}
              {status}
            </>
          ) : undefined
        }
        badges={badges}
        trailing={end}
        meta={meta}
        title={tooltip}
        onClick={onOpen}
        role={onOpen ? 'button' : undefined}
        aria-expanded={expandable && onOpen ? twisty === 'open' : undefined}
        divider={false}
      />
      {lines.map((n, i) => (
        <div key={i} className="fui-task-note" data-tone={n.tone ?? 'muted'} data-truncate={n.truncate || undefined} data-mono={n.mono || undefined} title={n.title}>
          {n.text}
        </div>
      ))}
      {actions && <div className="fui-task-actions">{actions}</div>}
      {children && <div className="fui-task-sub">{children}</div>}
    </>
  )
  const common = { className: classNames('fui-task-card', className), 'data-twisty': twisty ? true : undefined, 'data-dragging': dragging || undefined }
  if (frame === 'card') {
    return (
      <Card size="sm" variant="outlined" {...rest} {...common} data-frame="card">
        {body}
      </Card>
    )
  }
  return (
    <div {...rest} {...common} data-frame={frame}>
      {body}
    </div>
  )
}
