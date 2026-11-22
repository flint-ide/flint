import { Alert, Meter } from '@wunderui/react'
import { TriangleAlert } from 'lucide-react'
import type { ReactNode } from 'react'
import { ViewToolbar } from '../ViewToolbar'
import { contextLevel } from './status'

export type ChatHeaderProps = {
  /** The agent's name, in the accent colour. */
  title: ReactNode
  /** The model, muted. */
  subtitle?: ReactNode
  /** After the names: a ChatContextUsage. */
  meta?: ReactNode
  /** Right-aligned size="sm" controls. */
  actions?: ReactNode
}

/** The bar over a chat: who you talk to, how much context is used, and the chat's switches. */
export function ChatHeader({ title, subtitle, meta, actions }: ChatHeaderProps) {
  return (
    <ViewToolbar aria-label="Chat" className="fui-chat-header">
      <span className="fui-chat-agent">{title}</span>
      {subtitle && <span className="fui-muted fui-ellipsis">{subtitle}</span>}
      {meta}
      <span className="fui-grow" />
      {actions}
    </ViewToolbar>
  )
}

export type ChatContextUsageProps = {
  /** Context window fill, 0–100; null when the agent doesn't report it. */
  percent: number | null
  /** The full breakdown, as the tooltip. */
  title?: string
  /** Tokens and cost, as short spans. */
  children?: ReactNode
}

/** Compact running usage: a small context gauge (amber from 70 %, red from 90 %), tokens and cost. */
export function ChatContextUsage({ percent, title, children }: ChatContextUsageProps) {
  return (
    <span className="fui-chat-usage" title={title}>
      {percent !== null && (
        <>
          <Meter value={percent} aria-label="Context used" className="fui-chat-context" data-level={contextLevel(percent)} />
          <span>{percent}% context</span>
        </>
      )}
      {children}
    </span>
  )
}

export type ChatBannerProps = {
  /** `negative` when it blocks something (paused turns), `warning` otherwise. */
  tone?: 'warning' | 'negative'
  children: ReactNode
  /** Small buttons after the text. */
  actions?: ReactNode
  /** Shows the dismiss button. */
  onDismiss?: () => void
}

/** A full-width notice under the chat header that never blocks typing (a spent budget). */
export function ChatBanner({ tone = 'warning', children, actions, onDismiss }: ChatBannerProps) {
  return (
    <Alert variant={tone} size="small" role="status" icon={<TriangleAlert />} onClose={onDismiss} className="fui-chat-banner">
      <span className="fui-grow">{children}</span>
      {actions}
    </Alert>
  )
}
