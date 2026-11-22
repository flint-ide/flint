import type { ReactNode } from 'react'
import { Badge } from '@flint-ide/ui'

type BadgeColor = NonNullable<Parameters<typeof Badge>[0]['color']>

// The same states, in the same colours, as the tab strip's pills (`.task-state`).
const STATE_COLORS: Record<string, BadgeColor> = {
  queued: 'yellow',
  interrupted: 'yellow',
  working: 'blue',
  waiting: 'indigo',
  finished: 'green',
  failed: 'red',
  stale: 'red',
}

const MARKER_COLORS: Record<string, BadgeColor> = { error: 'red', warn: 'yellow' }

interface BadgeProps {
  title?: string
  children: ReactNode
}

/** A task or agent state as a status badge; `state` is Go's vocabulary (working, waiting, finished…). */
export function TaskStateBadge({ state, title, children }: BadgeProps & { state: string }) {
  return (
    <Badge color={STATE_COLORS[state] ?? 'grey'} badgeStyle="light" size="sm" title={title}>
      {children}
    </Badge>
  )
}

/** A short note on a row (lead, not ready, restricted…); `tone` is the `cls` the project helpers return. */
export function MarkerBadge({ tone, title, children }: BadgeProps & { tone?: string }) {
  return (
    <Badge color={MARKER_COLORS[tone ?? ''] ?? 'grey'} badgeStyle="light" size="sm" title={title}>
      {children}
    </Badge>
  )
}

/** A count at the end of a row or header. */
export function CountBadge({ title, children }: BadgeProps) {
  return (
    <Badge color="grey" badgeStyle="light" size="sm" className="count-badge" title={title}>
      {children}
    </Badge>
  )
}
