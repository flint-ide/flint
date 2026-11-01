import { useState } from 'react'
import { flushSettings, useSettings } from '../../store/settings'
import { useAgent } from '../../store/agent'
import { useAgentChat } from '../../store/agentChat'
import { errorMessage } from '../../api/backend'
import {
  displayName,
  EMPTY_CUSTOM_AGENT,
  followDefault,
  MAX_CUSTOM_AGENTS,
  suggestId,
  unusedPresets,
  withCustomAgent,
  withoutCustomAgent,
  type CustomAgent,
} from '../../lib/customAgents'
import { formatArgs, parseArgs } from '../../lib/toolSettings'
import * as SettingsAPI from '../../../bindings/flint/settingsapi'
import { settings as models } from '../../wails/models'
import { CheckedField, Row } from './fields'

/** Go's verdict on a whole list ('' = it would be saved). */
const checkList = (list: CustomAgent[]) => SettingsAPI.CheckCustomAgents(list.map((c) => models.CustomAgent.createFrom(c)))

/** Re-reads which agents are installed (pickers, chat menu) after settings changed. */
export function reloadAgents(): void {
  void useAgent.getState().load()
  void useAgentChat.getState().load()
}

/** Saves now and refreshes the agent lists: Go registers the agents on save. */
const saveAndReload = () => void flushSettings().then(reloadAgents)

/** Settings ▸ Agents ▸ Custom agents (ACP): list, edit, check, remove and add. */
export function CustomAgentsSection() {
  const list = useSettings((s) => s.settings.customAgents)
  return (
    <>
      <h3>Custom agents (ACP)</h3>
      <p className="muted settings-note">
        Any CLI with an Agent Client Protocol mode works in the agent chat, in "Ask the agent" and for commit messages; conflict resolution
        uses Claude Code or Codex. Custom agents start only in trusted folders, and their questions go through the same approval policy as the
        other agents. Log in with the agent's own command first: settings.json is plain text, so it holds no API keys.
      </p>
      {list.map((agent, index) => (
        <CustomAgentRows key={agent.id} agent={agent} index={index} list={list} />
      ))}
      {list.length < MAX_CUSTOM_AGENTS && <AddCustomAgent list={list} />}
    </>
  )
}

/** One saved custom agent: its fields, Check and Remove. */
function CustomAgentRows({ agent, index, list }: { agent: CustomAgent; index: number; list: CustomAgent[] }) {
  const update = useSettings((s) => s.update)
  const installed = useAgent((s) => s.available.includes(agent.id))
  const loaded = useAgent((s) => s.loaded)
  const [probe, setProbe] = useState<{ busy: boolean; text: string; error: boolean } | null>(null)
  const check = (change: Partial<CustomAgent>) => checkList(withCustomAgent(list, index, change))
  const commit = (change: Partial<CustomAgent>) => {
    update((s) => ({
      customAgents: withCustomAgent(s.customAgents, index, change),
      defaultAgent: change.id ? followDefault(s.defaultAgent, agent.id, change.id) : s.defaultAgent,
    }))
    saveAndReload()
  }
  const remove = () => {
    update((s) => ({ customAgents: withoutCustomAgent(s.customAgents, index), defaultAgent: followDefault(s.defaultAgent, agent.id, '') }))
    saveAndReload()
  }
  const runCheck = async () => {
    setProbe({ busy: true, text: 'Starting…', error: false })
    try {
      await flushSettings()
      const info = await SettingsAPI.ProbeCustomAgent(agent.id)
      const name = [info.title || info.name || displayName(agent), info.version].filter(Boolean).join(' ')
      const login = info.authMethods.length ? ` · log in: ${info.authMethods.join(', ')}` : ''
      setProbe({ busy: false, text: `${name} speaks ACP${login}`, error: false })
    } catch (err) {
      setProbe({ busy: false, text: errorMessage(err), error: true })
    }
  }
  return (
    <>
      <h3>
        {displayName(agent)} {loaded && !installed && <span className="muted">· {agent.command} not found</span>}
      </h3>
      <Row label="Id" hint="Names the agent in the agent menus and the default agent.">
        <CheckedField value={agent.id} check={(id) => check({ id })} onCommit={(id) => commit({ id })} />
      </Row>
      <Row label="Name" hint="Shown here; empty shows the id.">
        <CheckedField value={agent.name} placeholder={agent.id} check={(name) => check({ name })} onCommit={(name) => commit({ name })} />
      </Row>
      <Row label="Command" hint="A command on your login shell's PATH or an absolute path.">
        <CheckedField value={agent.command} check={(command) => check({ command })} onCommit={(command) => commit({ command })} />
      </Row>
      <Row label="Arguments" hint="Start the CLI in ACP mode. One per line.">
        <CheckedField
          multiline
          value={formatArgs(agent.args)}
          check={(text) => check({ args: parseArgs(text) })}
          onCommit={(text) => commit({ args: parseArgs(text) })}
        />
      </Row>
      <div className="settings-actions">
        <button onClick={() => void runCheck()} disabled={probe?.busy} title="Start the agent, run the ACP handshake and stop it again">
          Check
        </button>
        <button onClick={remove}>Remove {displayName(agent)}</button>
        {probe && <span className={probe.error ? 'error' : 'muted'}>{probe.text}</span>}
      </div>
    </>
  )
}

/** A form for a new custom agent, with presets as one-click fill-ins. */
function AddCustomAgent({ list }: { list: CustomAgent[] }) {
  const update = useSettings((s) => s.update)
  const [draft, setDraft] = useState<CustomAgent>(EMPTY_CUSTOM_AGENT)
  const [argsText, setArgsText] = useState('')
  const [hint, setHint] = useState('')
  const [error, setError] = useState('')
  const fill = (agent: CustomAgent, presetHint = '') => {
    setDraft(agent)
    setArgsText(formatArgs(agent.args))
    setHint(presetHint)
    setError('')
  }
  const add = async () => {
    const agent: CustomAgent = {
      ...draft,
      id: draft.id.trim() || suggestId(draft.name || draft.command, list),
      name: draft.name.trim(),
      command: draft.command.trim(),
      args: parseArgs(argsText),
    }
    const reason = await checkList([...list, agent]).catch((err: unknown) => errorMessage(err))
    setError(reason)
    if (reason) return
    update((s) => ({ customAgents: [...s.customAgents, agent] }))
    saveAndReload()
    fill(EMPTY_CUSTOM_AGENT)
  }
  const field = (key: 'id' | 'name' | 'command', placeholder: string) => (
    <input
      className="settings-text"
      value={draft[key]}
      placeholder={placeholder}
      spellCheck={false}
      onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
    />
  )
  const presets = unusedPresets(list)
  return (
    <>
      <h3>Add a custom agent</h3>
      {presets.length > 0 && (
        <Row label="Presets" hint="Fill in a CLI whose ACP mode is known; install it yourself.">
          {presets.map((p) => (
            <button key={p.id} onClick={() => fill(p, p.hint)}>
              {p.name}
            </button>
          ))}
        </Row>
      )}
      <Row label="Command" hint="A command on your login shell's PATH or an absolute path.">
        {field('command', 'goose')}
      </Row>
      <Row label="Arguments" hint="Start the CLI in ACP mode. One per line.">
        <textarea className="settings-textarea" value={argsText} placeholder="acp" spellCheck={false} onChange={(e) => setArgsText(e.target.value)} />
      </Row>
      <Row label="Name" hint="Optional.">
        {field('name', 'Goose')}
      </Row>
      <Row label="Id" hint="Optional; derived from the name or command. Lowercase letters, digits and dashes.">
        {field('id', suggestId(draft.name || draft.command, list))}
      </Row>
      <div className="settings-actions">
        <button onClick={() => void add()} disabled={!draft.command.trim()}>
          Add Agent
        </button>
        {hint && !error && <span className="muted">{hint}</span>}
        {error && <span className="error">{error}</span>}
      </div>
    </>
  )
}
