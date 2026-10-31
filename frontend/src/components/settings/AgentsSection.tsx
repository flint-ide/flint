import { useEffect } from 'react'
import { flushSettings, useSettings } from '../../store/settings'
import { useAgent } from '../../store/agent'
import { EMPTY_AGENT, formatArgs, isDefaultAgent, parseArgs, withAgent, type AgentSettings } from '../../lib/toolSettings'
import * as SettingsAPI from '../../../bindings/flint/settingsapi'
import { settings as models } from '../../wails/models'
import { Select } from '../ui/Select'
import { CheckedField, Row } from './fields'
import { CustomAgentsSection, reloadAgents } from './CustomAgentsSection'

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
  const update = useSettings((s) => s.update)
  useEffect(() => {
    if (!loaded) void useAgent.getState().load()
  }, [loaded])
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
            options={names.map((a) => ({ value: a, label: a, hint: available.includes(a) ? undefined : 'not installed' }))}
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
      <h3>Notifications</h3>
      <Row label="Tell me when work finishes in the background" hint="A macOS notification and a count on the Dock icon when a run, tests or an agent finish while Flint isn't the active app.">
        <input type="checkbox" checked={notify} onChange={(e) => update({ notifyInBackground: e.target.checked })} />
      </Row>
    </section>
  )
}

/** Path, model and extra arguments of one agent CLI. */
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
          <button onClick={() => commit({ ...EMPTY_AGENT, args: [] })}>Reset {label} to Defaults</button>
        </div>
      )}
    </>
  )
}
