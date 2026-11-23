import { useState } from 'react'
import { Modal } from '../../components/layout/Modal'
import { Select } from '../../components/ui/Select'
import { AgentPicker, useDefaultAgent } from './AgentPicker'
import { ScriptNotice } from './ScriptNotice'
import { StepFlow } from './StepFlow'
import {
  COORDINATE_EXPLAIN, COORDINATE_TOOL_OFF, goalProblem, isRestrictedError, MAX_GOAL,
  defaultSendBack, nameProblem, noteProblem, pickWorkflow, sendBackChoices, sendBackLabel, sourceLabel, stepCount, taskProblem, usable,
  whereItRuns,
} from './workflows'
import { closeDialog, coordinateWithAgent, createWorkflow, openAgentContextSettings, sendBack, startRun, trustWorkflowFolder, useWorkflows } from './store'
import { Alert, Button, Field, FieldDescription, FieldLabel, Form, FormActions, Input, Textarea } from '@flint-ide/ui'

export type DialogHost = 'panel' | 'run'

const basename = (p: string) => p.replace(/\/+$/, '').slice(p.replace(/\/+$/, '').lastIndexOf('/') + 1)

/** The dialog of the panel (Run Workflow…, New Workflow…) or of a run tab (Send back…): only the host it was asked for draws it. */
export function WorkflowDialogs({ host, runId }: { host: DialogHost; runId?: string }) {
  const dialog = useWorkflows((s) => s.dialog)
  if (!dialog) return null
  if (dialog.kind === 'sendBack') return host === 'run' && dialog.run === runId ? <SendBackDialog runId={dialog.run} /> : null
  if (host !== 'panel') return null
  if (dialog.kind === 'coordinate') return <CoordinateDialog dir={dialog.dir} />
  return dialog.kind === 'run' ? <RunDialog dir={dialog.dir} initial={dialog.workflow} /> : <NewDialog dir={dialog.dir} />
}

/**
 * Coordinate with Agent…: the goal and the agent. The prompt Go builds goes into a chat draft with that agent
 * (nothing is sent for the user). When the workflow tools are off, the dialog says what to switch on.
 */
function CoordinateDialog({ dir }: { dir: string }) {
  const { agents, fallback } = useDefaultAgent()
  const [goal, setGoal] = useState('')
  const [agent, setAgent] = useState('')
  const [error, setError] = useState('')
  const [toolOff, setToolOff] = useState(false)
  const [busy, setBusy] = useState(false)
  const chosen = agent || fallback
  const problem = goalProblem(goal)
  const ready = !busy && !problem && !!chosen
  const submit = async () => {
    if (!ready) return
    setBusy(true)
    const r = await coordinateWithAgent(dir, goal, chosen)
    setBusy(false)
    if (r.error) setError(r.error)
    setToolOff(r.toolOff)
  }
  if (toolOff) {
    return (
      <Modal title="Coordinate with Agent" onClose={closeDialog} className="new-prompt">
        <Form onSubmit={(e) => e.preventDefault()}>
          <Alert size="inline" variant="warning">{COORDINATE_TOOL_OFF}</Alert>
          <FormActions align="end">
            <Button variant="plain" type="button" onClick={closeDialog}>Close</Button>
            <Button variant="primary" type="button" onClick={openAgentContextSettings}>Open Agent Context Settings</Button>
          </FormActions>
        </Form>
      </Modal>
    )
  }
  return (
    <Modal title="Coordinate with Agent" onClose={closeDialog} className="wf-dialog">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <div className="wf-dialog-body">
          <Textarea
            autoFocus
            label="Goal"
            rows={6}
            value={goal}
            placeholder="What should be built or changed? The agent starts the workflow runs for it."
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
          <span className={`muted${[...goal].length > MAX_GOAL ? ' error' : ''}`}>{[...goal].length} / {MAX_GOAL}</span>
          <Field>
            <FieldLabel>Agent</FieldLabel>
            {agents.length === 0 ? (
              <FieldDescription>No agent installed (claude, codex or a custom agent in Settings ▸ Agents)</FieldDescription>
            ) : (
              <AgentPicker value={chosen} onChange={setAgent} />
            )}
          </Field>
          <Field>
            <FieldDescription>{COORDINATE_EXPLAIN} You read the prompt in a chat draft and send it yourself.</FieldDescription>
          </Field>
          {isRestrictedError(error) && (
            <Alert size="inline" variant="negative">
              This folder is in Restricted Mode: agents get no workflow tools there.{' '}
              <Button variant="link" size="sm" type="button" onClick={() => void trustWorkflowFolder(dir).then(() => setError(''))} title={dir}>Trust Folder…</Button>
            </Alert>
          )}
          {error && !isRestrictedError(error) && <Alert size="inline" variant="negative">{error}</Alert>}
        </div>
        <FormActions align="end">
          <Button variant="plain" type="button" onClick={closeDialog}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={!ready}>{busy ? 'Preparing…' : 'Put Prompt in Chat'}</Button>
        </FormActions>
      </Form>
    </Modal>
  )
}

/**
 * Run Workflow…: the workflow with its steps as a preview, what should be done and the agent. Start makes the
 * agent task (a branch and a worktree) and opens the run.
 */
function RunDialog({ dir, initial }: { dir: string; initial: string }) {
  const listed = useWorkflows((s) => s.lists[dir])
  const { agents, fallback } = useDefaultAgent()
  const [choice, setChoice] = useState(initial)
  const [description, setDescription] = useState('')
  const [agent, setAgent] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const workflows = listed?.listing?.workflows ?? []
  const picked = pickWorkflow(workflows, choice)
  const workflow = workflows.find((w) => w.name === picked)
  const chosen = agent || fallback
  const problem = taskProblem(description)
  const ready = !busy && !!workflow && !problem && !!chosen
  const submit = async () => {
    if (!ready || !workflow) return
    setBusy(true)
    setError('')
    const err = await startRun({ dir, workflow: workflow.name, description, agent: chosen })
    setBusy(false)
    if (err) setError(err)
  }
  return (
    <Modal title="Run Workflow" onClose={closeDialog} className="new-task wf-dialog">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <div className="wf-dialog-body">
          <Field>
            <FieldLabel>Workflow</FieldLabel>
            {workflows.length === 0 ? (
              <FieldDescription>{listed?.error || (listed ? 'No workflow is offered for this folder.' : 'Loading the workflows…')}</FieldDescription>
            ) : (
              <Select
                block
                value={picked}
                options={workflows.map((w) => ({
                  value: w.name,
                  label: w.name,
                  disabled: !usable(w),
                  hint: w.problem || [w.description, sourceLabel(w.source)].filter(Boolean).join(' · '),
                }))}
                onChange={setChoice}
              />
            )}
            {workflow && workflow.description && <FieldDescription>{workflow.description}</FieldDescription>}
          </Field>
          {workflow && (
            <Field>
              <FieldLabel>{`What it does (${stepCount(workflow.steps.length)}, one after the other)`}</FieldLabel>
              <div className="wf-preview">
                <StepFlow steps={workflow.steps} defaultAgent={chosen} label={`Steps of ${workflow.name}`} />
              </div>
            </Field>
          )}
          {workflow && <ScriptNotice steps={workflow.steps} sandbox={listed?.listing?.sandbox ?? ''} allowed={listed?.listing?.unsandboxedOK ?? false} dir={dir} onSettings={closeDialog} />}
          <Textarea
            autoFocus
            label="What should be done?"
            rows={5}
            value={description}
            placeholder="Describe the change you want. Every step of the workflow gets this text."
            spellCheck
            onChange={(e) => {
              setDescription(e.target.value)
              setError('')
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                void submit()
              }
            }}
          />
          <Field>
            <FieldLabel>Agent</FieldLabel>
            {agents.length === 0 ? (
              <FieldDescription>No agent installed (claude, codex or a custom agent in Settings ▸ Agents)</FieldDescription>
            ) : (
              <AgentPicker value={chosen} onChange={setAgent} />
            )}
            <FieldDescription>It takes every step that names no agent of its own.</FieldDescription>
          </Field>
          <Field>
            <FieldDescription>{whereItRuns(basename(dir))} You can stop, send back or approve from the run tab.</FieldDescription>
          </Field>
          {error && <Alert size="inline" variant="negative">{error}</Alert>}
        </div>
        <FormActions align="end">
          <Button variant="plain" type="button" onClick={closeDialog}>Cancel</Button>
          <span title={ready ? undefined : workflow ? problem || 'No agent is installed' : 'Choose a workflow that can run'}>
            <Button variant="primary" type="submit" disabled={!ready}>{busy ? 'Starting…' : 'Start'}</Button>
          </span>
        </FormActions>
      </Form>
    </Modal>
  )
}

/** New Workflow…: a name (lowercase letters, digits, dashes); the starter file opens in the editor. */
function NewDialog({ dir }: { dir: string }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const problem = name === '' ? '' : nameProblem(name)
  const valid = name !== '' && !problem
  const create = async () => {
    if (!valid || busy) return
    setBusy(true)
    const err = await createWorkflow(dir, name)
    setBusy(false)
    if (err) return setError(err)
    closeDialog()
  }
  return (
    <Modal title="New Workflow" onClose={closeDialog} className="new-prompt">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <Input
          label="Name"
          autoFocus
          value={name}
          placeholder="plan-build-review"
          onChange={(e) => {
            setName(e.target.value)
            setError('')
          }}
        />
        <Field>
          <FieldDescription>
            Lowercase letters, digits and dashes. Flint writes a starter file to .flint/workflows/{problem ? 'name' : name || 'name'}.md in {basename(dir)}, shares it
            through git with your team, and opens it so you can change the steps.
          </FieldDescription>
        </Field>
        {problem && <Alert size="inline" variant="negative">{problem}</Alert>}
        {error && <Alert size="inline" variant="negative">{error}</Alert>}
        <FormActions align="end">
          <Button variant="plain" type="button" onClick={closeDialog}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={!valid || busy}>Create and Open</Button>
        </FormActions>
      </Form>
    </Modal>
  )
}

/** Send back…: the step to take again (any step up to the current one; the one the failure belongs to is chosen) and a note for its agent. */
function SendBackDialog({ runId }: { runId: string }) {
  const run = useWorkflows((s) => s.runs?.find((r) => r.id === runId))
  const [step, setStep] = useState('')
  const [note, setNote] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  if (!run) return null
  const choices = sendBackChoices(run)
  const target = choices.some((c) => c.id === step) ? step : defaultSendBack(run)
  const problem = noteProblem(note)
  const ready = !busy && !!target && !problem
  const submit = async () => {
    if (!ready) return
    setBusy(true)
    const err = await sendBack(run.id, target, note)
    setBusy(false)
    if (err) setError(err)
  }
  return (
    <Modal title="Send back" onClose={closeDialog} className="wf-dialog">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <div className="wf-dialog-body">
          <Field>
            <FieldLabel>Take again from</FieldLabel>
            {choices.length === 0 ? (
              <FieldDescription>This run has no step to send back to.</FieldDescription>
            ) : (
              <Select block value={target} options={choices.map((c) => ({ value: c.id, label: sendBackLabel(c) }))} onChange={setStep} />
            )}
            <FieldDescription>This step and the ones after it are taken again.</FieldDescription>
          </Field>
          <Textarea
            autoFocus
            label="Note for the agent"
            rows={4}
            value={note}
            placeholder="What should be different this time?"
            spellCheck
            onChange={(e) => {
              setNote(e.target.value)
              setError('')
            }}
          />
          {problem && <Alert size="inline" variant="negative">{problem}</Alert>}
          {error && <Alert size="inline" variant="negative">{error}</Alert>}
        </div>
        <FormActions align="end">
          <Button variant="plain" type="button" onClick={closeDialog}>Cancel</Button>
          <span title={ready ? undefined : choices.length === 0 ? 'No step to send back to' : problem}>
            <Button variant="primary" type="submit" disabled={!ready}>{busy ? 'Sending…' : 'Send Back'}</Button>
          </span>
        </FormActions>
      </Form>
    </Modal>
  )
}
