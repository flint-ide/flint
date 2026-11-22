import { useEffect, useState } from 'react'
import { Modal } from '../../components/layout/Modal'
import { Select } from '../../components/ui/Select'
import { agentOption } from '../../lib/agentDefs'
import { useAgent } from '../../store/agent'
import { useAgentChat } from '../../store/agentChat'
import { useAgentDefs } from '../../store/agentDefs'
import { useSettings } from '../../store/settings'
import { goalLength, goalProblem, isRestrictedError, MAX_GOAL, pickPlanAgent } from './projects'
import { closeDialog, createProject, createItem, planWithAgent, trustProject, useProjects, type DialogHost } from './store'
import { Button, Input, Textarea } from '@flint-ide/ui'

const basename = (p: string) => p.replace(/\/+$/, '').slice(p.replace(/\/+$/, '').lastIndexOf('/') + 1)

/** The dialog of the panel or of a board: only the host it was asked for draws it. */
export function ProjectDialogs({ host }: { host: DialogHost }) {
  const dialog = useProjects((s) => s.dialog)
  if (!dialog) return null
  if (dialog.kind === 'newProject') return host === 'panel' ? <NewProjectDialog folder={dialog.folder} /> : null
  if (dialog.host !== host) return null
  return dialog.kind === 'plan' ? <PlanDialog folder={dialog.folder} /> : <NewItemDialog folder={dialog.folder} />
}

/**
 * Plan with Agent…: the goal and the agent. The prompt Go builds goes into a chat draft with that agent
 * (nothing is sent for the user); an untrusted project is refused by Go, and Trust Folder… is offered.
 */
function PlanDialog({ folder }: { folder: string }) {
  const info = useProjects((s) => s.infos?.find((i) => i.path === folder))
  const agents = useAgentChat((s) => s.agents)
  const defined = useAgentDefs((s) => s.defined)
  const preferred = useAgent((s) => s.preferred)
  const defaultAgent = useSettings((s) => s.settings.defaultAgent)
  const [goal, setGoal] = useState('')
  const [agent, setAgent] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!useAgent.getState().loaded) void useAgent.getState().load()
    // Read again: agent definitions are files that change while Flint runs.
    void useAgentChat.getState().load()
  }, [])
  const chosen = agent || pickPlanAgent(agents, defaultAgent, preferred)
  const problem = goalProblem(goal)
  const restricted = (info && !info.trusted) || isRestrictedError(error)
  const ready = !busy && !problem && !!chosen
  const submit = async () => {
    if (!ready) return
    setBusy(true)
    const err = await planWithAgent(folder, goal, chosen)
    setBusy(false)
    if (err) setError(err)
  }
  return (
    <Modal title="Plan with Agent" onClose={closeDialog} className="plan-goal">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <label>
          Goal
          <Textarea
            autoFocus
            rows={6}
            value={goal}
            placeholder="What should be built or changed? The agent splits it into items of the project's backlog."
            spellCheck
            onChange={(e) => {
              setGoal(e.target.value)
              setError('')
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                void submit()
              }
            }}
          />
        </label>
        <span className={`muted${goalLength(goal) > MAX_GOAL ? ' error' : ''}`}>{goalLength(goal)} / {MAX_GOAL}</span>
        <label>
          Agent
          {agents.length === 0 ? (
            <span className="muted">No agent installed (claude, codex or a custom agent in Settings ▸ Agents)</span>
          ) : (
            <Select block value={chosen} options={agents.map((a) => agentOption(a, defined, a === (defaultAgent || preferred) ? 'default' : undefined))} onChange={setAgent} />
          )}
        </label>
        <p className="muted form-note">
          Flint puts a prompt into a chat draft with this agent: the goal, this project and the item format. You read it and send it yourself. The agent adds
          items with add_project_item, which you approve one by one.
        </p>
        {restricted && info && (
          <div className="form-status error">
            The project's folder is in Restricted Mode: agents could neither read the project nor add to it.{' '}
            <Button variant="link" size="sm" type="button" onClick={() => void trustProject(info).then(() => setError(''))} title={info.path}>Trust Folder…</Button>
          </div>
        )}
        {error && !isRestrictedError(error) && <div className="form-status error">{error}</div>}
        <div className="form-actions">
          <Button variant="plain" type="button" onClick={closeDialog}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={!ready}>{busy ? 'Preparing…' : 'Put Prompt in Chat'}</Button>
        </div>
      </form>
    </Modal>
  )
}

/** New Project…: names the project in the folder the user picked (optional). */
function NewProjectDialog({ folder }: { folder: string }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const valid = name.length <= 80
  const create = async () => {
    if (!valid || busy) return
    setBusy(true)
    const err = await createProject(folder, name)
    setBusy(false)
    if (err) return setError(err)
    closeDialog()
  }
  return (
    <Modal title="New Project" onClose={closeDialog} className="new-prompt">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <span className="muted selectable" title={folder}>Creates flint-project.json and an items folder in {folder}</span>
        <label>
          Name (shown in the panel; optional)
          <Input
            autoFocus
            value={name}
            placeholder={basename(folder)}
            onChange={(e) => {
              setName(e.target.value)
              setError('')
            }}
          />
        </label>
        {!valid && <span className="form-status error">At most 80 characters.</span>}
        {error && <span className="form-status error">{error}</span>}
        <div className="form-actions">
          <Button variant="plain" type="button" onClick={closeDialog}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={!valid || busy}>Create</Button>
        </div>
      </form>
    </Modal>
  )
}

/** New Item…: asks for the title; Go derives the id and writes the file, which opens in the editor. */
function NewItemDialog({ folder }: { folder: string }) {
  const [title, setTitle] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const valid = title.trim() !== '' && title.length <= 120
  const create = async () => {
    if (!valid || busy) return
    setBusy(true)
    const err = await createItem(folder, title)
    setBusy(false)
    if (err) return setError(err)
    closeDialog()
  }
  return (
    <Modal title="New Project Item" onClose={closeDialog} className="new-prompt">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <label>
          Title
          <Input
            autoFocus
            value={title}
            placeholder="Rate limit the checkout API"
            onChange={(e) => {
              setTitle(e.target.value)
              setError('')
            }}
          />
        </label>
        {title.length > 120 && <span className="form-status error">At most 120 characters.</span>}
        {error && <span className="form-status error">{error}</span>}
        <div className="form-actions">
          <Button variant="plain" type="button" onClick={closeDialog}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={!valid || busy}>Create and Open</Button>
        </div>
      </form>
    </Modal>
  )
}
