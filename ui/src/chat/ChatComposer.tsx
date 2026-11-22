import { Textarea } from '@wunderui/react'
import type { ComponentProps, ReactNode } from 'react'

export type ChatComposerProps = Omit<ComponentProps<typeof Textarea>, 'label' | 'helper' | 'error' | 'optional' | 'wrapperClassName' | 'children'> & {
  /** Removable attachment badges above the field. */
  attachments?: ReactNode
  /** Between the attachments and the field: a prompt picker or another popup that pushes the field down. */
  above?: ReactNode
  /** Left side of the bar under the field (attach buttons). */
  toolbar?: ReactNode
  /** Right side of the bar: Send, or Stop while the agent works. */
  actions?: ReactNode
}

/**
 * The message box under the transcript: attachments, the textarea (56–200 px,
 * selectable) and a bar with tools on the left and Send/Stop on the right. The
 * textarea props (ref, value, onKeyDown…) go straight to the kit's Textarea, so
 * the caller keeps its keyboard handling.
 */
export function ChatComposer({ attachments, above, toolbar, actions, ...textarea }: ChatComposerProps) {
  return (
    <div className="fui-chat-composer">
      {attachments && <div className="fui-chat-chips">{attachments}</div>}
      {above}
      <Textarea {...textarea} />
      <div className="fui-chat-composer-bar">
        {toolbar}
        <span className="fui-grow" />
        {actions}
      </div>
    </div>
  )
}

export type ChatChangedFilesProps = {
  /** The files, usually ChatFileLinks. */
  children: ReactNode
  /** How many more files changed than are shown. */
  more?: number
  label?: ReactNode
}

/** The strip over the composer listing the files the last turns changed. */
export function ChatChangedFiles({ children, more = 0, label = 'Changed:' }: ChatChangedFilesProps) {
  return (
    <div className="fui-chat-changed">
      <span className="fui-muted">{label}</span>
      {children}
      {more > 0 && <span className="fui-muted">+{more}</span>}
    </div>
  )
}
