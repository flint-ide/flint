import { useEffect } from 'react'
import { flushSettings, useSettings } from '../../store/settings'
import { useAgent } from '../../store/agent'
import { EMPTY_AGENT, formatArgs, isDefaultAgent, parseArgs, withAgent, withTier, type AgentSettings } from '../../lib/toolSettings'
import * as SettingsAPI from '../../../bindings/flint/settingsapi'
import { settings as models } from '../../wails/models'
import { Select } from '../ui/Select'
import { CheckedField, Row } from './fields'
import { CustomAgentsSection, reloadAgents } from './CustomAgentsSection'
import { AgentTasksSection } from './AgentTasksSection'
import { OrchestrationSection } from './OrchestrationSection'
import { TerminalAgentSection } from './TerminalAgentSection'
import { PromptsSection } from './PromptsSection'
import { AgentReviewSection } from './AgentReviewSection'
import { useAgentChat } from '../../store/agentChat'
import { useAgentDefs } from '../../store/agentDefs'
import { agentOption, DEFAULT_TIERS, MODEL_TIERS, type ModelTier } from '../../lib/agentDefs'
import { AgentDefsSection } from './AgentDefsSection'
import { isMac } from '../../lib/platform'

// The agent CLIs Flint drives (Go: agent.known).
const AGENTS = [
  { id: 'claude', label: 'Claude Code', modelHint: 'An alias (sonnet, opus) or a full model name.' },
  { id: 'codex', label: 'Codex', modelHint: 'A model name such as gpt-5-codex.' },
]

export function AgentsSection() {
  const available = useAgent((s) => s.available)
  const loaded = useAgent((s) => s.loaded)
  const defaultAgent = useSettings((s) => s.settings.defaultAgent)
  const notify = useSettings((s) => s.settings.notifyInBackground)
  const notifyApproval = useSettings((s) => s.settings.notifyOnApproval)
  const update = useSettings((s) => s.update)
  const defined = useAgentDefs((s) => s.defined)
  useEffect(() => {
    if (!loaded) void useAgent.getState().load()
  }, [loaded])
  // Definitions are files: read them again when the page opens.
  useEffect(() => {
    void useAgentChat.getState().load()
  }, [])
  const first = available[0]
  // A default that is no longer installed still shows, so it can be changed back.
  const names = defaultAgent && !available.includes(defaultAgent) ? [...available, defaultAgent] : available
  return (
    <section>
      <h2>Agents</h2>
      <Row label="Default agent" hint="Starts new agent chats and writes commit messages; resolves conflicts when it is Claude Code or Codex.">
        {loaded && available.length === 0 ? (
          <span className="muted">No agent installed (claude, codex or a custom agent below)</span>
        ) : (
          <Select
            value={defaultAgent}
            allowEmpty={first ? `First installed (${first})` : 'First installed'}
            options={names.map((a) => agentOption(a, defined, available.includes(a) ? undefined : 'not installed'))}
            onChange={(a) => useAgent.getState().setPreferred(a)}
          />
        )}
      </Row>
      {AGENTS.map((a) => (
        <AgentRows key={a.id} id={a.id} label={a.label} modelHint={a.modelHint} />
      ))}
      <p className="muted settings-note">
        Applies to new chats, commit messages and conflict resolution; running chats keep their settings. Extra arguments go before Flint's
        own flags, may not change the flags Flint sets (output format, permission prompt, setting sources, sandbox, approvals, model) and
        are ignored in Restricted Mode.
      </p>
      <CustomAgentsSection />
      <AgentDefsSection />
      <AgentTasksSection />
      <OrchestrationSection />
      <TerminalAgentSection />
      <PromptsSection />
      <AgentReviewSection />
      <h3>Notifications</h3>
      <Row label="Tell me when work finishes in the background" hint={`${isMac() ? 'A macOS notification and a count on the Dock icon' : 'A desktop notification'} when a run, tests or an agent finish while Flint isn't the active app.`}>
        <input type="checkbox" checked={notify} onChange={(e) => update({ notifyInBackground: e.target.checked })} />
      </Row>
      <Row label="Tell me when an agent waits for my approval" hint={`A notification naming the agent and what it asks for, and a count on the badge until you answer.${notify ? '' : ' Needs "Tell me when work finishes in the background", which is off.'}`}>
        <input type="checkbox" checked={notifyApproval} onChange={(e) => update({ notifyOnApproval: e.target.checked })} />
      </Row>
    </section>
  )
}

/** Path, model, tier models and extra arguments of one agent CLI. */
function AgentRows({ id, label, modelHint }: { id: string; label: string; modelHint: string }) {
  const agent = useSettings((s) => s.settings.agents[id]) ?? EMPTY_AGENT
  const update = useSettings((s) => s.update)
  const check = (change: Partial<AgentSettings>) => SettingsAPI.CheckAgent(id, models.AgentSettings.createFrom({ ...agent, ...change }))
  const commit = (change: Partial<AgentSettings>) => {
    update((s) => ({ agents: withAgent(s.agents, id, change) }))
    // Another path can make the agent (un)available.
    if ('path' in change) void flushSettings().then(reloadAgents)
  }
  return (
    <>
      <h3>{label}</h3>
      <Row label="Path" hint={`Empty looks up ${id} on your login shell's PATH; otherwise a command name or an absolute path.`}>
        <CheckedField value={agent.path} placeholder={id} check={(path) => check({ path })} onCommit={(path) => commit({ path })} />
      </Row>
      <Row label="Model" hint={`Empty uses the CLI's default. ${modelHint}`}>
        <CheckedField value={agent.model} placeholder="CLI default" check={(model) => check({ model })} onCommit={(model) => commit({ model })} />
      </Row>
      {MODEL_TIERS.map((tier) => (
        <TierRow key={tier} id={id} tier={tier} tiers={agent.tiers ?? {}} check={check} commit={commit} />
      ))}
      <Row label="Extra arguments" hint="One per line.">
        <CheckedField
          multiline
          value={formatArgs(agent.args)}
          placeholder={id === 'codex' ? '-c\nmodel_reasoning_effort=high' : '--add-dir\n/Users/me/notes'}
          check={(text) => check({ args: parseArgs(text) })}
          onCommit={(text) => commit({ args: parseArgs(text) })}
        />
      </Row>
      {!isDefaultAgent(agent) && (
        <div className="settings-actions">
          <button onClick={() => commit({ ...EMPTY_AGENT, args: [], tiers: {} })}>Reset {label} to Defaults</button>
        </div>
      )}
    </>
  )
}

const TIER_LABELS: Record<ModelTier, string> = { fast: 'Fast model', balanced: 'Balanced model', strong: 'Strong model' }

/**
 * The model one tier means on this CLI, for defined agents whose model is the tier. Empty
 * returns it to the default (the placeholder); a tier set to "" in settings.json has none.
 */
function TierRow(props: {
  id: string
  tier: ModelTier
  tiers: Record<string, string>
  check: (change: Partial<AgentSettings>) => Promise<string>
  commit: (change: Partial<AgentSettings>) => void
}) {
  const { id, tier, tiers, check, commit } = props
  const fallback = DEFAULT_TIERS[id]?.[tier]
  const cleared = tiers[tier] === ''
  const placeholder = fallback && !cleared ? fallback : 'Model above'
  const set = (model: string) => withTier(tiers, tier, model === '' ? undefined : model)
  return (
    <Row
      label={TIER_LABELS[tier]}
      hint={
        cleared
          ? `For defined agents with model: ${tier}. Set to "" in settings.json, so they use the model above.`
          : `For defined agents with model: ${tier}. Empty uses ${fallback ?? 'the model above'}.`
      }
    >
      <CheckedField value={tiers[tier] ?? ''} placeholder={placeholder} check={(model) => check({ tiers: set(model) })} onCommit={(model) => commit({ tiers: set(model) })} />
    </Row>
  )
}
