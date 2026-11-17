import { useEffect, useMemo } from 'react'
import { agentRows, attentionCount, stateHint, stateLabel, type AgentRow } from '../../lib/agentList'
import { useAgentChat } from '../../store/agentChat'
import { followTasks, useTasks } from '../../store/agentTasks'
import { focusAgent, useAgents } from '../../store/agents'

/** Every agent of this window, the ones that need you first; a click shows its chat. */
export function AgentsPanel() {
  const sessions = useAgentChat((s) => s.sessions)
  const order = useAgentChat((s) => s.order)
  const tasks = useTasks((s) => s.tasks)
  const unseen = useAgents((s) => s.unseen)
  // Task states come from Go's events; following them is what keeps this list current.
  useEffect(() => followTasks(), [])

  const rows = useMemo(
    () => agentRows({ sessions: order.flatMap((id) => (sessions[id] ? [sessions[id]] : [])), tasks, unseen }),
    [sessions, order, tasks, unseen],
  )
  const attention = attentionCount(rows)

  return (
    <div className="panel tasks-panel">
      <div className="panel-header">
        <span className="panel-title">Agents</span>
        {attention > 0 && <span className="badge" title={`${attention} waiting for you, failed or finished`}>{attention}</span>}
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
  return (
    <div className="task">
      <div className="tree-row static task-row" title={`${row.title}\n\n${row.root}`} onClick={() => focusAgent(row)}>
        <span className={`task-state ${look}`} title={stateHint(row.state)}>{stateLabel(row.state)}</span>
        <span className="file-name">{row.title}</span>
      </div>
      <div className="task-meta muted">
        <span>{row.agent}{row.kind === 'task' ? ' · task' : ''}</span>
        <span title={row.root}>{row.project}</span>
      </div>
    </div>
  )
}
