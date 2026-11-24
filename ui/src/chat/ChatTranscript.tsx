import { Alert, Separator, Spinner } from '@wunderui/react'
import { useEffect, useRef, type ReactNode } from 'react'
import { TextShimmer } from '../TextShimmer'
import { classNames } from '../treeIndent'
import { isNearBottom } from './status'

export type ChatTranscriptProps = {
  /** Changes whenever the transcript grows (the items array): the view follows it to the bottom. */
  follow: unknown
  /** Marks the transcript while the agent works: running tool calls animate only then. */
  busy?: boolean
  children: ReactNode
  className?: string
  'aria-label'?: string
}

/**
 * The scrolling message list. Follows the stream unless the reader scrolled up
 * (more than 40 px from the bottom); scrolling, which reads the layout, happens at
 * most once per frame however often `follow` changes.
 */
export function ChatTranscript({ follow, busy, children, className, 'aria-label': label }: ChatTranscriptProps) {
  const scroller = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const frame = useRef(0)

  useEffect(() => {
    if (!stick.current || frame.current) return
    frame.current = requestAnimationFrame(() => {
      frame.current = 0
      const el = scroller.current
      if (el && stick.current) el.scrollTop = el.scrollHeight
    })
  }, [follow])
  useEffect(() => () => cancelAnimationFrame(frame.current), [])

  return (
    <div
      ref={scroller}
      aria-label={label}
      className={classNames('fui-chat-log', className)}
      data-busy={busy || undefined}
      onScroll={(e) => {
        stick.current = isNearBottom(e.currentTarget)
      }}
    >
      {children}
    </div>
  )
}

export type ChatMessageRole = 'user' | 'assistant' | 'thought' | 'error'

export type ChatMessageProps = {
  role: ChatMessageRole
  /** Who sent a user message when it wasn't typed here ("from Review"). */
  from?: ReactNode
  /** Attachment badges under a user message. */
  attachments?: ReactNode
  /** Usually a ChatMarkdown; plain text for errors. */
  children: ReactNode
}

/**
 * One message: the user's as a right-aligned bubble, the agent's as plain text,
 * its reasoning muted and indented, an error as a negative inline Alert.
 */
export function ChatMessage({ role, from, attachments, children }: ChatMessageProps) {
  if (role === 'error') {
    return (
      <Alert variant="negative" size="inline" className="fui-chat-msg" data-role="error">
        <span className="fui-chat-error">{children}</span>
      </Alert>
    )
  }
  return (
    <div className="fui-chat-msg" data-role={role}>
      {from && <div className="fui-chat-from">from {from}</div>}
      {children}
      {attachments && <div className="fui-chat-chips">{attachments}</div>}
    </div>
  )
}

export type ChatNoticeProps = {
  /** `divider` draws a dashed rule above (a restored conversation); `placeholder` is the empty chat's hint. */
  variant?: 'plain' | 'divider' | 'placeholder'
  children: ReactNode
}

/** A muted line between messages: turn usage, "restored after restart", the empty-chat hint. */
export function ChatNotice({ variant = 'plain', children }: ChatNoticeProps) {
  return (
    <div className="fui-chat-notice" data-variant={variant}>
      {variant === 'divider' && <Separator dashed />}
      <span>{children}</span>
    </div>
  )
}

/** The whole chat: header, banners, plan, transcript and composer stacked to fill the tab. */
export function ChatView({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={classNames('fui-chat', className)}>{children}</div>
}

/** The agent is working: a spinner and a label at the end of the transcript. */
export function ChatWorking({ children = 'working…' }: { children?: ReactNode }) {
  return (
    <div className="fui-chat-working" role="status">
      <Spinner aria-hidden />
      <TextShimmer>{children}</TextShimmer>
    </div>
  )
}
