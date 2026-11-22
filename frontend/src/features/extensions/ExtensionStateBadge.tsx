import { Badge } from '@flint-ide/ui'
import type { ReactNode } from 'react'

/** How an extension's state reads: running is good, invalid is bad, everything else is neutral. */
export type StateTone = 'ok' | 'bad' | 'none'

const COLORS = { ok: 'green', bad: 'red', none: 'grey' } as const

/** The state label on the right of an extension card's head. */
export function ExtensionStateBadge({ tone, children }: { tone: StateTone; children: ReactNode }) {
  return (
    <span className="extension-state">
      <Badge size="sm" badgeStyle="light" color={COLORS[tone]}>
        {children}
      </Badge>
    </span>
  )
}
