import { useEffect, useState } from 'react'
import { Modal } from '../../components/layout/Modal'
import { StepFlow } from './StepFlow'
import { ScriptNotice } from './ScriptNotice'
import { acceptSchedule, loadList, useWorkflows } from './store'
import { SCHEDULE_CHANGED_NOTE, stepCount } from './workflows'
import type { ScheduleView } from '../../../bindings/flint/internal/features/workflows/models'
import { Alert, Button, Field, FieldDescription, FieldLabel, Form, FormActions } from '@flint-ide/ui'

/** The Review and accept… button of a changed schedule, with the dialog it opens. */
export function ReviewButton({ schedule }: { schedule: ScheduleView }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button variant="ghost" size="sm" type="button" onClick={() => setOpen(true)} title="See the workflow as it is now, then accept it">
        Review and accept…
      </Button>
      {open && <ScheduleReview schedule={schedule} onClose={() => setOpen(false)} />}
    </>
  )
}

/** The current steps of the schedule's workflow (with the commands of its script steps) and the button that accepts them. */
function ScheduleReview({ schedule, onClose }: { schedule: ScheduleView; onClose: () => void }) {
  const listed = useWorkflows((s) => s.lists[schedule.folder])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => void loadList(schedule.folder), [schedule.folder])
  const workflow = listed?.listing?.workflows.find((w) => w.name === schedule.workflow)
  const sandbox = listed?.listing?.sandbox ?? ''
  const accept = async () => {
    if (busy) return
    setBusy(true)
    const err = await acceptSchedule(schedule.name)
    setBusy(false)
    if (err) return setError(err)
    onClose()
  }
  return (
    <Modal title={`Review ${schedule.name}`} onClose={onClose} className="new-task wf-dialog">
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void accept()
        }}
      >
        <div className="wf-dialog-body">
          <Field>
            <FieldDescription>{SCHEDULE_CHANGED_NOTE}</FieldDescription>
          </Field>
          <Field>
            <FieldLabel>
              {workflow ? `${schedule.workflow} now (${stepCount(workflow.steps.length)}, one after the other)` : schedule.workflow}
            </FieldLabel>
            {workflow ? (
              <div className="wf-preview">
                <StepFlow steps={workflow.steps} defaultAgent={schedule.agent} label={`Steps of ${workflow.name}`} />
              </div>
            ) : (
              <FieldDescription>{listed?.error || (listed ? 'This workflow is not offered for the folder any more.' : 'Loading the workflow…')}</FieldDescription>
            )}
          </Field>
          {workflow && <ScriptNotice steps={workflow.steps} sandbox={sandbox} onSettings={onClose} />}
          {workflow?.problem && <Alert size="inline" variant="negative">{workflow.problem}</Alert>}
          {error && <Alert size="inline" variant="negative">{error}</Alert>}
        </div>
        <FormActions align="end">
          <Button variant="plain" type="button" onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={busy}>{busy ? 'Accepting…' : 'Accept and run on schedule'}</Button>
        </FormActions>
      </Form>
    </Modal>
  )
}
