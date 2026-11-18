import { engineLabel, ignoredNote, modelLabel } from '../../lib/agentDefs'
import { openAgentDefDialog, openAgentDefFile, useAgentDefs } from '../../store/agentDefs'

/**
 * Settings ▸ Agents ▸ Defined agents: Markdown files that define an agent (engine, model, instructions).
 * Shows the store's last read: the page reads the agents when it opens (AgentsSection) and
 * New Agent… after it creates one, so this list makes no read of its own.
 */
export function AgentDefsSection() {
  const defs = useAgentDefs((s) => s.defs)
  const list = defs?.agents ?? []
  return (
    <>
      <h3>Defined agents</h3>
      <p className="muted settings-note">
        A Markdown file with an optional engine (claude or codex), an optional model and standing instructions defines an agent you can pick
        wherever an agent is chosen. Personal ones live in {defs?.personalDir || '…'}; repository ones in .flint/agents of a trusted folder.
        Without an engine, an agent runs on the default agent. A model tier (fast, balanced or strong) works on either engine: each engine's
        tier models above say what it means. A defined agent cannot change permissions or tools.
      </p>
      {defs && list.length === 0 && <p className="muted settings-note">No defined agents yet.</p>}
      {list.map((a) => (
        <div key={a.path} className="settings-actions">
          <span title={a.description || undefined}>
            <strong>{a.name}</strong> · {engineLabel(a)}
            {a.model && <> · {modelLabel(a)}</>} · {a.source}
            {a.description && <span className="muted"> · {a.description}</span>}
            {!a.resolvedEngine && <span className="muted"> · no engine installed</span>}
          </span>
          <button onClick={() => openAgentDefFile(a.path)}>Open</button>
        </div>
      ))}
      {(defs?.problems ?? []).map((p) => (
        <p key={p} className="settings-note error selectable">{p}</p>
      ))}
      {(defs?.ignored ?? []).map((i) => (
        <p key={i.dir} className="muted settings-note selectable">{ignoredNote(i)}</p>
      ))}
      <div className="settings-actions">
        <button onClick={openAgentDefDialog}>New Agent…</button>
      </div>
    </>
  )
}
