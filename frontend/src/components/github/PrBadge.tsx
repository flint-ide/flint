import { Badge, type BadgeColor } from '@flint-ide/ui'
import type { BadgeTone } from '../../lib/prReview'

// A pull request state or count in the kit's Badge, coloured like the theme's status colours.
const COLOR: Record<BadgeTone, BadgeColor> = {
  ok: 'green',
  bad: 'red',
  pending: 'indigo',
  merged: 'purple',
  muted: 'grey',
}

export function PrBadge({ tone, title, children }: { tone: BadgeTone; title?: string; children: React.ReactNode }) {
  return (
    <Badge size="sm" color={COLOR[tone]} badgeStyle="light" title={title}>
      {children}
    </Badge>
  )
}
