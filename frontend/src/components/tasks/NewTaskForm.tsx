import { useEffect, useMemo, useState } from 'react'
import { Git } from '../../store/git'
import { useAgentChat } from '../../store/agentChat'
import { useAgent } from '../../store/agent'
import { useSettings } from '../../store/settings'
import { activeRoot } from '../../store/activeRoot'
import { closeNewTask, startTask } from '../../store/agentTasks'
import { Modal } from '../layout/Modal'
import { Select } from '../ui/Select'
import type { Branch } from '../../store/git'

/**
 * New Agent Task: what to do, which agent, from which branch. Start creates the
 * branch and worktree, opens it and sends the description as the first message.
 */
export function NewTaskForm() {
  const agents = useAgentChat((s) => s.agents)
  const preferred = useAgent((s) => s.preferred)
  const prefix = useSettings((s) => s.settings.agentTasks.branchPrefix)
  const [description, setDescription] = useState('')
  const [agent, setAgent] = useState('')
  const [branches, setBranches] = useState<Branch[]>([])
  const [base, setBase] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const root = useMemo(() => activeRoot(), [])

  useEffect(() => {
    if (!useAgent.getState().loaded) void useAgent.getState().load()
    if (useAgentChat.getState().agents.length === 0) void useAgentChat.getState().load()
    Git.Branches(root).then(
      (list) => {
        const local = (list ?? []).filter((b) => !b.remote)
        setBranches(local)
        setBase((cur) => cur || local.find((b) => b.current)?.name || local[0]?.name || '')
      },
      () => setError('This folder is not a git repository: agent tasks need one.'),
    )
  }, [root])

  const chosen = agent || (agents.includes(preferred) ? preferred : agents[0] ?? '')

  const submit = async () => {
    setBusy(true)
    setError(null)
    const err = await startTask({ description, agent: chosen, base })
    setBusy(false)
    if (err) setError(err)
  }

  return (
    <Modal title="New Agent Task" onClose={closeNewTask} className="new-task">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <label>
          Task
          <textarea
            autoFocus
            rows={6}
            value={description}
            placeholder="What should the agent do? The first line names the branch."
            spellCheck
            onChange={(e) => setDescription(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                if (!busy && description.trim() && chosen && base) void submit()
              }
            }}
          />
        </label>
        <div className="form-row">
          <label className="grow">
            Agent
            {agents.length === 0 ? (
              <span className="muted">No agent installed (claude, codex or a custom agent in Settings ▸ Agents)</span>
            ) : (
              <Select block value={chosen} options={agents.map((a) => ({ value: a, label: a, hint: a === preferred ? 'default' : undefined }))} onChange={setAgent} />
            )}
          </label>
          <label className="grow">
            From branch
            <Select
              block
              searchable
              value={base}
              options={branches.map((b) => ({ value: b.name, label: b.name, hint: b.current ? 'current' : undefined }))}
              onChange={setBase}
            />
          </label>
        </div>
        <p className="muted form-note">
          Flint creates a branch {prefix || ''}… and a worktree for it, opens the folder here and sends the task to the agent in a new chat.
        </p>
        {error && <div className="form-status error">{error}</div>}
        <div className="form-actions">
          <span className="spacer" />
          <button type="button" onClick={closeNewTask}>Cancel</button>
          <button type="submit" className="primary" disabled={busy || !description.trim() || !chosen || !base}>
            {busy ? 'Starting…' : 'Start'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
