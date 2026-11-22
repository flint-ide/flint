import { useSettings } from '../../store/settings'
import { ORCHESTRATION_LIMITS, type OrchestrationSettings } from '../../lib/orchestration'
import { NumberField, Row } from './fields'
import { Switch } from '@flint-ide/ui'

const NUMBERS: { key: keyof typeof ORCHESTRATION_LIMITS; label: string; hint: string; suffix: string }[] = [
  { key: 'maxChildren', label: 'Agents per chat', hint: 'How many agents one chat may have started at a time.', suffix: 'agents' },
  {
    key: 'maxDepth',
    label: 'Levels deep',
    hint: 'How many levels of agents may start agents: 1 lets your chat start agents that cannot start any.',
    suffix: 'levels',
  },
  {
    key: 'maxWaitSeconds',
    label: 'Longest wait',
    hint: "Caps one wait_for_agent call (Flint's MCP server never holds a call longer than 10 minutes).",
    suffix: 's',
  },
  { key: 'outputLines', label: 'Output lines', hint: 'Most lines read_agent_output returns, redacted.', suffix: 'lines' },
  {
    key: 'maxAutoTurns',
    label: 'Turns without you',
    hint: 'Most turns agents may start in each other (messages, first prompts, notices) before you write in one of their chats again. Past it they are told to stop and report to you.',
    suffix: 'turns',
  },
]

/** Settings ▸ Agents ▸ Agent orchestration: agents that start and direct other agents. */
export function OrchestrationSection() {
  const o = useSettings((s) => s.settings.orchestration)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<OrchestrationSettings>) => update((s) => ({ orchestration: { ...s.orchestration, ...change } }))
  return (
    <>
      <h3>Agent orchestration</h3>
      <Row
        label="Let agents start and direct agents"
        hint="Off by default. When on, an agent in a trusted folder can start other agents there, send them messages and read their output through Flint's MCP server (which must be on). The first time a chat does, Flint asks you once; what it sends appears in the other chats, redacted, without a preview."
      >
        <Switch checked={o.enabled} onCheckedChange={(checked) => set({ enabled: checked })} />
      </Row>
      {NUMBERS.map((n) => (
        <Row key={n.key} label={n.label} hint={n.hint}>
          <NumberField
            value={o[n.key]}
            min={ORCHESTRATION_LIMITS[n.key].min}
            max={ORCHESTRATION_LIMITS[n.key].max}
            step={1}
            suffix={n.suffix}
            onCommit={(v) => set({ [n.key]: Math.round(v) })}
          />
        </Row>
      ))}
      <Row label="Tell a chat when its agents need it" hint="When an agent a chat started waits for approval, fails or finishes, Flint sends that chat one short message with the agent's last lines, so it need not poll.">
        <Switch checked={o.notifyParent} onCheckedChange={(checked) => set({ notifyParent: checked })} />
      </Row>
    </>
  )
}
