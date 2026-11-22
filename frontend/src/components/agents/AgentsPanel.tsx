import { useEffect, useMemo } from 'react'
import { Badge, Button } from '@flint-ide/ui'
import { agentRows, attentionCount, stateHint, stateLabel, type AgentRow } from '../../lib/agentList'
import { TaskStateBadge } from '../tasks/badges'
import { modKey, modifierName } from '../../lib/platform'
import { useAgentChat } from '../../store/agentChat'
import { followTasks, useTasks } from '../../store/agentTasks'
import { focusAgent, peekAgent, settleChat, settledChats, terminalInput, unsettleChat, useAgents } from '../../store/agents'
import { useEditor } from '../../store/editor'
import { useTerminal } from '../../store/terminal'

/** Every agent of this window, the ones that need you first; a click shows its chat. */
export function AgentsPanel() {
  const sessions = useAgentChat((s) => s.sessions)
  const order = useAgentChat((s) => s.order)
  const tasks = useTasks((s) => s.tasks)
  const unseen = useAgents((s) => s.unseen)
  const hidden = useEditor((s) => s.hidden)
  const terminalTabs = useTerminal((s) => s.tabs)
  // Task states come from Go's events; following them is what keeps this list current.
  useEffect(() => followTasks(), [])

  const rows = useMemo(
    () => agentRows({
        sessions: order.flatMap((id) => (sessions[id] ? [sessions[id]] : [])), tasks, unseen, settled: settledChats(hidden),
        terminals: terminalTabs.map(terminalInput),
      }),
    [sessions, order, tasks, unseen, hidden, terminalTabs],
  )
  const attention = attentionCount(rows)

  return (
    <div className="panel tasks-panel">
      <div className="panel-header">
        <span className="panel-title">Agents</span>
        {attention > 0 && <Badge color="orange" badgeStyle="light" size="sm" title={`${attention} waiting for you, failed or finished`}>{attention}</Badge>}
      </div>
      <div className="tree scroll">
        {rows.map((r) => <AgentRowView key={r.key} row={r} />)}
        {rows.length === 0 && <div className="placeholder muted">No agents yet. Start a chat or an agent task and it is listed here.</div>}
      </div>
    </div>
  )
}

function AgentRowView({ row }: { row: AgentRow }) {
  // finished-unseen shares the look of a finished task.
  const look = row.state === 'finished-unseen' ? 'finished' : row.state
  const id = row.sessionId
  return (
    <div className="task">
      <div
        className="tree-row static task-row"
        title={`${row.title}\n\n${row.root}${id ? `\n\n${modifierName('Mod')}-click to peek without leaving your tab` : ''}`}
        onClick={(e) => (id && modKey(e) ? peekAgent(id) : focusAgent(row))}
      >
        <TaskStateBadge state={look} title={stateHint(row.state, row.kind, row.blocked)}>{stateLabel(row.state)}</TaskStateBadge>
        <span className="file-name">{row.title}</span>
      </div>
      <div className="task-meta muted">
        <span>{row.agent}{row.kind === 'task' ? ' · task' : ''}{row.kind === 'terminal' ? ' · terminal' : ''}{row.hidden ? ' · hidden' : ''}</span>
        <span title={row.root}>{row.project}</span>
      </div>
      {id && (
        <div className="task-actions">
          <Button variant="ghost" size="sm" onClick={() => peekAgent(id)} title="Show the chat over your current tab: answer a request or send a prompt, then go back">Peek</Button>
          {row.hidden ? (
            <Button variant="ghost" size="sm" onClick={() => unsettleChat(id)} title="Draw the chat's tab again">Show tab</Button>
          ) : (
            <Button variant="ghost" size="sm" onClick={() => settleChat(id)} title="Hide the chat's tab until the agent needs you">Hide until needed</Button>
          )}
        </div>
      )}
    </div>
  )
}
