import { useEffect, useMemo, useState } from 'react'
import { trustedRoots, validAgentName } from '../../lib/agentDefs'
import { rootLabels } from '../../lib/workspaceRoots'
import { activeRoot } from '../../store/activeRoot'
import { closeAgentDefDialog, createAgentDef } from '../../store/agentDefs'
import { useAgentChat } from '../../store/agentChat'
import { useTrust } from '../../store/trust'
import { useWorkspace } from '../../store/workspace'
import { Modal } from '../layout/Modal'
import { Select } from '../ui/Select'

/**
 * New Agent Definition…: names an agent and creates its Markdown file from a template
 * in the config folder (personal) or in .flint/agents of a trusted workspace folder.
 */
export function AgentDefDialog() {
  const roots = useWorkspace((s) => s.roots)
  const trusted = useTrust((s) => s.trusted)
  const projects = useMemo(() => trustedRoots(roots, trusted), [roots, trusted])
  const labels = useMemo(() => rootLabels(projects), [projects])
  const [name, setName] = useState('')
  const [personal, setPersonal] = useState(projects.length === 0)
  const [root, setRoot] = useState(() => (projects.includes(activeRoot()) ? activeRoot() : (projects[0] ?? '')))
  const [error, setError] = useState('')
  // Definitions are files: read them again, so a taken name shows up before Go says so.
  useEffect(() => {
    void useAgentChat.getState().load()
  }, [])
  const valid = validAgentName(name)
  const create = async () => {
    if (!valid) return
    const err = await createAgentDef(personal ? '' : root, personal, name)
    if (err) return setError(err)
    void useAgentChat.getState().load()
    closeAgentDefDialog()
  }
  return (
    <Modal title="New Agent" onClose={closeAgentDefDialog} className="new-prompt">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <label>
          Name (used wherever an agent name is)
          <input
            autoFocus
            value={name}
            spellCheck={false}
            placeholder="reviewer"
            onChange={(e) => {
              setName(e.target.value.trim())
              setError('')
            }}
          />
        </label>
        {name && !valid && <span className="form-status error">Lowercase letters, digits and - only, starting with a letter (at most 32).</span>}
        <label className="check">
          <input type="radio" name="scope" checked={!personal} disabled={projects.length === 0} onChange={() => setPersonal(false)} />
          Project: .flint/agents, shared with the team through the repository
        </label>
        {!personal && projects.length > 1 && (
          <Select block value={root} options={projects.map((r, i) => ({ value: r, label: labels[i] ?? r }))} onChange={setRoot} />
        )}
        {projects.length === 0 && <span className="muted">Open and trust a folder to create a project agent.</span>}
        <label className="check">
          <input type="radio" name="scope" checked={personal} onChange={() => setPersonal(true)} />
          Personal: only you, in every project
        </label>
        {error && <span className="form-status error">{error}</span>}
        <div className="form-actions">
          <button type="button" onClick={closeAgentDefDialog}>Cancel</button>
          <button type="submit" className="primary" disabled={!valid}>Create and Open</button>
        </div>
      </form>
    </Modal>
  )
}
