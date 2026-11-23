import { useEffect } from 'react'
import { Select } from '../../components/ui/Select'
import { agentOption } from '../../lib/agentDefs'
import { useAgent } from '../../store/agent'
import { useAgentChat } from '../../store/agentChat'
import { useAgentDefs } from '../../store/agentDefs'
import { useSettings } from '../../store/settings'

/** The agent Run Workflow… starts with: the default agent, else the preferred one, else the first installed. */
export function useDefaultAgent(): { agents: string[]; fallback: string } {
  const agents = useAgentChat((s) => s.agents)
  const preferred = useAgent((s) => s.preferred)
  const defaultAgent = useSettings((s) => s.settings.defaultAgent)
  return { agents, fallback: [defaultAgent, preferred].find((a) => a && agents.includes(a)) ?? agents[0] ?? '' }
}

interface Props {
  value: string
  onChange: (agent: string) => void
  /** The label of the empty choice ("none"); without it an agent is always chosen. */
  none?: string
  disabled?: boolean
}

/** The agent select of New Agent Task, for a workflow: installed agents and defined ones, with the default marked. */
export function AgentPicker({ value, onChange, none, disabled }: Props) {
  const defined = useAgentDefs((s) => s.defined)
  const { agents } = useDefaultAgent()
  const preferred = useAgent((s) => s.preferred)
  const defaultAgent = useSettings((s) => s.settings.defaultAgent)
  useEffect(() => {
    if (!useAgent.getState().loaded) void useAgent.getState().load()
    // Read again: agent definitions are files that change while Flint runs.
    void useAgentChat.getState().load()
  }, [])
  // A chosen agent that is not installed now stays in the list, so the select shows what is saved.
  const names = value && !agents.includes(value) ? [...agents, value] : agents
  return (
    <Select
      block
      disabled={disabled}
      value={value}
      allowEmpty={none}
      options={names.map((a) => agentOption(a, defined, a === (defaultAgent || preferred) ? 'default' : undefined))}
      onChange={onChange}
    />
  )
}
