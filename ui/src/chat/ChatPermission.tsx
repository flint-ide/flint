import { Button, Card, CardContent, CardFooter, CardHeader, CardTitle } from '@wunderui/react'
import { memo, type ReactNode } from 'react'
import { ToolCallOutput, ToolKindIcon } from './ToolCall'

/** One way to answer: ACP's allow_once, allow_always, reject_once, reject_always. */
export type ChatPermissionOption = { id: string; name: string; kind: string }

export type ChatPermissionProps = {
  /** The tool kind the agent asks about (its icon). */
  kind?: string
  title: ReactNode
  /** The tool's input: a command, a patch. */
  input?: string
  options: readonly ChatPermissionOption[]
  /** Answered or cancelled: the buttons give way to `answer`. */
  settled?: boolean
  /** The chosen option's name; null shows "Cancelled". */
  answer?: string | null
  onAnswer: (optionId: string, optionName: string) => void
}

// Allow once is the expected answer (primary), allow always the plain one, rejects are destructive.
function variantOf(kind: string): 'primary' | 'plain' | 'destructive' {
  if (!kind.startsWith('allow')) return 'destructive'
  return kind === 'allow_once' ? 'primary' : 'plain'
}

/** A question from the agent with its options; a settled one shows the answer instead. */
export const ChatPermission = memo(function ChatPermission({ kind, title, input, options, settled, answer, onAnswer }: ChatPermissionProps) {
  return (
    <Card variant="outlined" size="sm" className="fui-chat-permission" data-settled={settled || undefined}>
      <CardHeader leading={<ToolKindIcon kind={kind} />}>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      {input && (
        <CardContent>
          <ToolCallOutput>{input}</ToolCallOutput>
        </CardContent>
      )}
      <CardFooter divider={false} className="fui-chat-permission-footer">
        {settled ? (
          <span className="fui-muted">{answer ?? 'Cancelled'}</span>
        ) : (
          options.map((o) => (
            <Button key={o.id} size="sm" variant={variantOf(o.kind)} onClick={() => onAnswer(o.id, o.name)}>
              {o.name}
            </Button>
          ))
        )}
      </CardFooter>
    </Card>
  )
})
