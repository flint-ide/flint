import { useEffect, useMemo, useState } from 'react'
import { Git } from '../../store/git'
import { useAgentChat } from '../../store/agentChat'
import { useAgent } from '../../store/agent'
import { useAgentDefs } from '../../store/agentDefs'
import { agentLabel, agentOption } from '../../lib/agentDefs'
import { useSettings } from '../../store/settings'
import { activeRoot } from '../../store/activeRoot'
import { closeNewTask, startTask } from '../../store/agentTasks'
import { MAX_GROUP, planAgents, planNote, planProblem } from '../../lib/agentTasks'
import { Modal } from '../layout/Modal'
import { Select } from '../ui/Select'
import type { Branch } from '../../store/git'
import { Alert, Button, Checkbox, Field, FieldDescription, FieldLabel, Form, FormActions, Textarea } from '@flint-ide/ui'

/**
 * New Agent Task: what to do, which agent, from which branch. Start creates the
 * branch and worktree, opens it and sends the description as the first message.
 * With "Compare agents" the same task goes to several agents (or several attempts
 * of one), each on its own branch from the same commit, linked as a group.
 */
export function NewTaskForm() {
  const agents = useAgentChat((s) => s.agents)
  const defined = useAgentDefs((s) => s.defined)
  const preferred = useAgent((s) => s.preferred)
  const prefix = useSettings((s) => s.settings.agentTasks.branchPrefix)
  const maxParallel = useSettings((s) => s.settings.agentTasks.maxParallel)
  const [compare, setCompare] = useState(false)
  const [extra, setExtra] = useState<string[]>([])
  const [attempts, setAttempts] = useState(1)
  const [description, setDescription] = useState('')
  const [agent, setAgent] = useState('')
  const [branches, setBranches] = useState<Branch[]>([])
  const [base, setBase] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const root = useMemo(() => activeRoot(), [])

  useEffect(() => {
    if (!useAgent.getState().loaded) void useAgent.getState().load()
    // Read again: definitions are files that change while Flint runs.
    void useAgentChat.getState().load()
    Git.Branches(root).then(
      (list) => {
        const local = (list ?? []).filter((b) => !b.remote)
        setBranches(local)
        setBase((cur) => cur || local.find((b) => b.current)?.name || local[0]?.name || '')
      },
      () => setError('This folder is not a git repository: agent tasks need one.'),
    )
  }, [root])

  // The default agent from settings is known at once; Agent.Info may still be loading.
  const defaultAgent = useSettings((s) => s.settings.defaultAgent)
  const fallback = [defaultAgent, preferred].find((a) => a && agents.includes(a)) ?? agents[0] ?? ''
  const chosen = agent || fallback
  // The agent of the select is always in; "Compare agents" adds others and attempts.
  const plan = compare ? planAgents([chosen, ...extra.filter((a) => agents.includes(a))], attempts) : planAgents([chosen], 1)
  const problem = chosen ? planProblem(plan) : null
  const ready = !busy && !!description.trim() && !!chosen && !!base && !problem

  const submit = async () => {
    setBusy(true)
    setError(null)
    const err = await startTask({ description, agents: plan, base })
    setBusy(false)
    if (err) setError(err)
  }

  return (
    <Modal title="New Agent Task" onClose={closeNewTask} className="new-task">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <Textarea
          autoFocus
          label="Task"
          rows={6}
          value={description}
          placeholder="What should the agent do? The first line names the branch."
          spellCheck
          onChange={(e) => setDescription(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault()
              if (ready) void submit()
            }
          }}
        />
        <div className="form-row">
          <Field className="grow">
            <FieldLabel>Agent</FieldLabel>
            {agents.length === 0 ? (
              <FieldDescription>No agent installed (claude, codex or a custom agent in Settings ▸ Agents)</FieldDescription>
            ) : (
              <Select block value={chosen} options={agents.map((a) => agentOption(a, defined, a === preferred ? 'default' : undefined))} onChange={setAgent} />
            )}
          </Field>
          <Field className="grow">
            <FieldLabel>From branch</FieldLabel>
            <Select
              block
              searchable
              value={base}
              options={branches.map((b) => ({ value: b.name, label: b.name, hint: b.current ? 'current' : undefined }))}
              onChange={setBase}
            />
          </Field>
        </div>
        {agents.length > 0 && (
          <Checkbox
            label="Compare agents: give the same task to several agents or attempts"
            checked={compare}
            onCheckedChange={setCompare}
          />
        )}
        {compare && (
          <div className="form-row compare-agents">
            <div className="grow compare-agent-list">
              {agents.filter((a) => a !== chosen).map((a) => (
                <Checkbox
                  key={a}
                  label={`also ${agentLabel(a, defined)}`}
                  checked={extra.includes(a)}
                  onCheckedChange={(on) => setExtra((cur) => (on ? [...cur, a] : cur.filter((x) => x !== a)))}
                />
              ))}
              {agents.length < 2 && <span className="muted">Only one agent is installed: use several attempts.</span>}
            </div>
            <Field>
              <FieldLabel>Attempts per agent</FieldLabel>
              <Select
                block
                value={attempts}
                options={Array.from({ length: MAX_GROUP }, (_, i) => ({ value: i + 1, label: String(i + 1) }))}
                onChange={setAttempts}
              />
            </Field>
          </div>
        )}
        <FieldDescription>
          Flint creates a branch {prefix || ''}… and a worktree for it, opens the folder here and sends the task to the agent in a new chat.{' '}
          {planNote(plan, maxParallel)}
        </FieldDescription>
        {problem && <Alert size="inline" variant="negative">{problem}</Alert>}
        {error && <Alert size="inline" variant="negative">{error}</Alert>}
        <FormActions align="end">
          <Button variant="plain" type="button" onClick={closeNewTask}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={!ready}>
            {busy ? 'Starting…' : plan.length > 1 ? `Start ${plan.length} tasks` : 'Start'}
          </Button>
        </FormActions>
      </Form>
    </Modal>
  )
}
