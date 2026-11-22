import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@wunderui/react'
import { CircleDot, Square, SquareCheck } from 'lucide-react'

export type ChatPlanEntry = { content: string; status: string }

export type ChatPlanProps = {
  entries: readonly ChatPlanEntry[]
  defaultOpen?: boolean
}

function EntryIcon({ status }: { status: string }) {
  if (status === 'completed') return <SquareCheck aria-label="done" />
  if (status === 'in_progress') return <CircleDot aria-label="in progress" />
  return <Square aria-label="to do" />
}

/** The agent's plan under the chat header: a collapsible checklist with done/total. Nothing when empty. */
export function ChatPlan({ entries, defaultOpen = true }: ChatPlanProps) {
  if (entries.length === 0) return null
  const done = entries.filter((e) => e.status === 'completed').length
  return (
    <Collapsible appearance="row" defaultOpen={defaultOpen} className="fui-chat-plan">
      <CollapsibleTrigger indicator="chevron" className="fui-chat-plan-head">
        <span className="fui-chat-plan-title">
          <span>Plan</span>
          <span className="fui-muted">
            {done}/{entries.length}
          </span>
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent className="fui-chat-plan-body">
        <ul className="fui-chat-plan-list">
          {entries.map((e, i) => (
            <li key={i} data-status={e.status}>
              <EntryIcon status={e.status} />
              <span>{e.content}</span>
            </li>
          ))}
        </ul>
      </CollapsibleContent>
    </Collapsible>
  )
}
