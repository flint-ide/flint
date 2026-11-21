import { Avatar, AvatarFallback, Badge, Button } from '@wunderui/react'
import { Bot, Square } from 'lucide-react'

export type AgentState = 'working' | 'waiting' | 'done' | 'failed'

export type AgentRowProps = {
  name: string
  model?: string
  task: string
  state: AgentState
  elapsed?: string
  onStop?: () => void
  onOpen?: () => void
}

const BADGES: Record<AgentState, { color: 'orange' | 'blue' | 'green' | 'red'; label: string }> = {
  working: { color: 'orange', label: 'working' },
  waiting: { color: 'blue', label: 'needs input' },
  done: { color: 'green', label: 'done' },
  failed: { color: 'red', label: 'failed' },
}

/** One agent session: who, what it's doing, its state and a stop button while it works. */
export function AgentRow({ name, model, task, state, elapsed, onStop, onOpen }: AgentRowProps) {
  const badge = BADGES[state]
  return (
    <div className="fui-agent" data-clickable={onOpen ? true : undefined} onClick={onOpen}>
      <Avatar size="sm">
        <AvatarFallback>
          <Bot />
        </AvatarFallback>
      </Avatar>
      <div className="fui-grow fui-agent-text">
        <div className="fui-agent-head">
          <strong>{name}</strong>
          {model && <span className="fui-muted fui-mono fui-nowrap">{model}</span>}
        </div>
        <div className="fui-muted fui-ellipsis">{task}</div>
      </div>
      <Badge color={badge.color} badgeStyle="light">
        {badge.label}
      </Badge>
      <span className="fui-muted fui-mono fui-agent-time">{elapsed}</span>
      <span className="fui-agent-action">
        {state === 'working' && onStop && (
          <Button
            size="sm"
            variant="ghost"
            aria-label={`Stop ${name}`}
            onClick={(e) => {
              e.stopPropagation()
              onStop()
            }}
          >
            <Square />
          </Button>
        )}
      </span>
    </div>
  )
}
