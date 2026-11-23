import { useEffect, useState } from 'react'
import { Modal } from '../../components/layout/Modal'
import { Select } from '../../components/ui/Select'
import { useWorkspace } from '../../store/workspace'
import { AgentPicker } from './AgentPicker'
import { DAYS, firstProblem, hasProblem, scheduleProblems, type Day, type Schedule } from './settings'
import { loadList, useWorkflows } from './store'
import { usable } from './workflows'
import { Alert, Button, Checkbox, Field, FieldDescription, FieldLabel, Fieldset, FieldsetLegend, Form, FormActions, Input, Textarea } from '@flint-ide/ui'

const DAY_LABELS: Record<Day, string> = { mon: 'Mon', tue: 'Tue', wed: 'Wed', thu: 'Thu', fri: 'Fri', sat: 'Sat', sun: 'Sun' }

const basename = (p: string) => p.replace(/\/+$/, '').slice(p.replace(/\/+$/, '').lastIndexOf('/') + 1)

const EMPTY: Schedule = { name: '', workflow: '', folder: '', task: '', agent: '', at: '02:30', days: [] }

/** A problem is shown once the user has touched the field, or tried to save. */
function Problem({ text, show }: { text: string; show: boolean }) {
  return show && text ? <span className="error">{text}</span> : null
}

/**
 * Add or edit a schedule. The same bounds as Go are checked here with the reason next to the field; Go stays the
 * authority: its refusal comes back as the "Settings not saved" message.
 */
export function ScheduleDialog({ initial, taken, onSave, onClose }: {
  /** The schedule to edit; null adds a new one. */
  initial: Schedule | null
  /** The names of the other schedules. */
  taken: readonly string[]
  onSave: (s: Schedule) => void
  onClose: () => void
}) {
  const roots = useWorkspace((s) => s.roots)
  const [draft, setDraft] = useState<Schedule>(initial ?? { ...EMPTY, folder: roots[0] ?? '' })
  const [tried, setTried] = useState(false)
  const listed = useWorkflows((s) => s.lists[draft.folder])
  useEffect(() => void loadList(draft.folder), [draft.folder])
  const set = (change: Partial<Schedule>) => setDraft((d) => ({ ...d, ...change }))
  const workflows = listed?.listing?.workflows ?? []
  const problems = scheduleProblems(draft, taken)
  const submit = () => {
    setTried(true)
    if (hasProblem(problems)) return
    onSave({ ...draft, task: draft.task.trim() })
  }
  const folders = draft.folder && !roots.includes(draft.folder) ? [...roots, draft.folder] : roots
  const names = draft.workflow && !workflows.some((w) => w.name === draft.workflow) ? [...workflows.map((w) => w.name), draft.workflow] : workflows.map((w) => w.name)
  const toggleDay = (day: Day, on: boolean) => set({ days: DAYS.filter((d) => (d === day ? on : draft.days.includes(d))) })
  return (
    <Modal title={initial ? 'Edit Schedule' : 'Add Schedule'} onClose={onClose} className="new-task wf-dialog">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <div className="wf-dialog-body">
          <Input label="Name" autoFocus value={draft.name} placeholder="nightly-changelog" onChange={(e) => set({ name: e.target.value })} />
          <Problem text={problems.name} show={tried || draft.name !== ''} />
          <Field>
            <FieldLabel>Folder</FieldLabel>
            {folders.length === 0 ? (
              <FieldDescription>Open a folder in Flint first.</FieldDescription>
            ) : (
              <Select block value={draft.folder} options={folders.map((f) => ({ value: f, label: basename(f) || f, hint: f }))} onChange={(folder) => set({ folder, workflow: '' })} />
            )}
            <Problem text={problems.folder} show={tried} />
          </Field>
          <Field>
            <FieldLabel>Workflow</FieldLabel>
            {names.length === 0 ? (
              <FieldDescription>{listed?.error || (listed ? 'No workflow is offered for this folder.' : 'Loading the workflows…')}</FieldDescription>
            ) : (
              <Select
                block
                value={draft.workflow}
                allowEmpty="choose a workflow"
                options={names.map((n) => {
                  const w = workflows.find((x) => x.name === n)
                  return { value: n, label: n, disabled: !!w && !usable(w), hint: w?.problem || w?.description }
                })}
                onChange={(workflow) => set({ workflow })}
              />
            )}
            <Problem text={problems.workflow} show={tried} />
          </Field>
          <Textarea
            label="What should be done?"
            rows={4}
            value={draft.task}
            placeholder="Describe the change. Every step of the workflow gets this text."
            spellCheck
            onChange={(e) => set({ task: e.target.value })}
          />
          <Problem text={problems.task} show={tried || draft.task !== ''} />
          <Field>
            <FieldLabel>Agent</FieldLabel>
            <AgentPicker value={draft.agent} none="default agent" onChange={(agent) => set({ agent })} />
          </Field>
          <Input label="Time" type="time" value={draft.at} onChange={(e) => set({ at: e.target.value })} />
          <Problem text={problems.at} show={tried || draft.at === ''} />
          {/* A Fieldset, not a Field: inside a Field the kit points every checkbox label at the Field's one control, so clicking a day's name toggled nothing. */}
          <Fieldset>
            <FieldsetLegend>Days</FieldsetLegend>
            <div className="wf-buttons">
              {DAYS.map((d) => (
                <Checkbox key={d} label={DAY_LABELS[d]} checked={draft.days.includes(d)} onCheckedChange={(on) => toggleDay(d, on === true)} />
              ))}
            </div>
            <span className="muted">{draft.days.length === 0 ? 'No day chosen: it runs every day.' : 'It runs on the days that are checked.'}</span>
          </Fieldset>
          {tried && hasProblem(problems) && <Alert size="inline" variant="negative">Fix the fields marked above.</Alert>}
        </div>
        <FormActions align="end">
          <Button variant="plain" type="button" onClick={onClose}>Cancel</Button>
          <span title={hasProblem(problems) ? firstProblem(problems) : undefined}>
            <Button variant="primary" type="submit" disabled={hasProblem(problems)}>{initial ? 'Save' : 'Add'}</Button>
          </span>
        </FormActions>
      </Form>
    </Modal>
  )
}
