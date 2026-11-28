import { useEffect, useState } from 'react'
import { confirmAction } from '../../store/dialog'
import { useSettings } from '../../store/settings'
import { Row, NumberField } from '../../components/settings/fields'
import { AgentPicker } from './AgentPicker'
import { ScheduleDialog } from './ScheduleDialog'
import { MAX_SCHEDULES, MAX_WRITABLE, parseFolders, writableProblem, type Schedule } from './settings'
import { ReviewButton } from './ScheduleReview'
import { useWorkflows } from './store'
import { SCHEDULE_CHANGED_NOTE, scheduleWhen } from './workflows'
import { Button, Switch, Textarea } from '@flint-ide/ui'

/** Settings ▸ Workflows: attempts per step, usage limits, the script sandbox and the schedules. */
export function WorkflowsSection() {
  const settings = useSettings((s) => s.settings.workflows)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<typeof settings>) => update({ workflows: { ...settings, ...change } })
  return (
    <section>
      <h2>Workflows</h2>
      <Row label="Attempts per step" hint="How many times Flint takes a step again after a check fails. After that the run stops and waits for you.">
        <NumberField value={settings.maxAttempts} min={1} max={10} step={1} onCommit={(maxAttempts) => set({ maxAttempts })} />
      </Row>
      <Row label="Ask again without a result" hint="When an agent's turn ends without its result line (often while it says it waits for something in the background), Flint asks it to finish this many times before the run waits for you. 0 means it waits at once.">
        <NumberField value={settings.nudges} min={0} max={5} step={1} onCommit={(nudges) => set({ nudges })} />
      </Row>
      <Row label="Agent for a usage limit" hint="When an agent reaches its usage limit, this agent takes the step instead. With none, the run is “Paused by a limit”.">
        <div className="settings-control">
          <AgentPicker value={settings.fallbackAgent} none="none" onChange={(fallbackAgent) => set({ fallbackAgent })} />
        </div>
      </Row>
      <Row label="Resume after" hint="A run that is “Paused by a limit” goes on by itself after this long. 0 means it waits until you click Resume.">
        <NumberField value={settings.resumeAfterMinutes} min={0} max={720} step={5} suffix="minutes" onCommit={(resumeAfterMinutes) => set({ resumeAfterMinutes })} />
      </Row>
      <h3>Sandbox</h3>
      <Row label="Run script steps in a sandbox" hint="On by default. A script step can write only in the run's folder and temp folders and has no network. If a build or test needs a cache folder or the network, add the folder or allow the network below. Agent steps are not affected.">
        <Switch checked={settings.sandbox === 'on'} onCheckedChange={(on) => set({ sandbox: on ? 'on' : 'off' })} />
      </Row>
      <Row label="Allow network in the sandbox" hint="Script steps can then reach the network. Needs the sandbox.">
        <Switch disabled={settings.sandbox !== 'on'} checked={settings.sandboxNetwork} onCheckedChange={(sandboxNetwork) => set({ sandboxNetwork })} />
      </Row>
      <Row label="Extra folders a script may write" hint={`One folder per line (an absolute folder, or one under ~/), at most ${MAX_WRITABLE}. Saved when you leave the field.`}>
        <WritableFolders disabled={settings.sandbox !== 'on'} saved={settings.sandboxWritable} onCommit={(sandboxWritable) => set({ sandboxWritable })} />
      </Row>
      <h3>Schedules</h3>
      <p className="muted settings-note">
        A schedule starts a workflow run at a time, while Flint is open and the folder is open and trusted. A missed time is not caught up.
      </p>
      <ScheduleList schedules={settings.schedules} onChange={(schedules) => set({ schedules })} />
    </section>
  )
}

/** The folders as a text field, one per line; saved on blur when valid, the reason shown under the field. */
function WritableFolders({ saved, disabled, onCommit }: { saved: readonly string[]; disabled: boolean; onCommit: (folders: string[]) => void }) {
  const text = saved.join('\n')
  const [draft, setDraft] = useState(text)
  useEffect(() => setDraft(text), [text])
  const folders = parseFolders(draft)
  const problem = writableProblem(folders)
  const commit = () => {
    if (!problem && folders.join('\n') !== text) onCommit(folders)
  }
  return (
    <div className="settings-control settings-control-stack">
      <Textarea
        className="wide-field"
        disabled={disabled}
        aria-invalid={problem ? true : undefined}
        value={draft}
        spellCheck={false}
        placeholder="/Users/me/.cache/build"
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
      />
      {problem && <span className="error">{problem}</span>}
    </div>
  )
}

/** The schedules with Add, Edit and Remove; the dialog checks one, the list is saved with the other settings. */
function ScheduleList({ schedules, onChange }: { schedules: readonly Schedule[]; onChange: (next: Schedule[]) => void }) {
  // index: the schedule being edited; -1 adds a new one; null: no dialog.
  const [editing, setEditing] = useState<number | null>(null)
  const views = useWorkflows((s) => s.schedules)
  const save = (s: Schedule) => {
    onChange(editing === null || editing < 0 ? [...schedules, s] : schedules.map((x, i) => (i === editing ? s : x)))
    setEditing(null)
  }
  const remove = async (index: number) => {
    const name = schedules[index]?.name
    if (name === undefined || !(await confirmAction(`Remove the schedule ${name}? It will no longer start runs.`, 'Remove'))) return
    // Read the list again: it may have changed while the question was open.
    onChange(schedules.filter((s) => s.name !== name))
  }
  return (
    <div className="settings-control-stack">
      {schedules.length === 0 && <p className="muted">No schedule yet.</p>}
      {schedules.map((s, i) => {
        const changed = views?.find((v) => v.name === s.name && v.changed)
        return (
        <div key={s.name} className="settings-row">
          <div className="settings-label">
            <span>{s.name}</span>
            <span className="muted" title={s.folder}>{`${s.workflow} · ${scheduleWhen(s)}`}</span>
            {changed && <span className="warn">{SCHEDULE_CHANGED_NOTE}</span>}
          </div>
          <div className="settings-control">
            {changed && <ReviewButton schedule={changed} />}
            <Button variant="ghost" size="sm" onClick={() => setEditing(i)}>Edit…</Button>
            <Button variant="ghost" size="sm" onClick={() => void remove(i)}>Remove</Button>
          </div>
        </div>
        )
      })}
      <div className="settings-actions">
        <span title={schedules.length >= MAX_SCHEDULES ? `At most ${MAX_SCHEDULES} schedules` : undefined}>
          <Button variant="plain" disabled={schedules.length >= MAX_SCHEDULES} onClick={() => setEditing(-1)}>Add Schedule…</Button>
        </span>
      </div>
      {editing !== null && (
        <ScheduleDialog
          initial={editing >= 0 ? (schedules[editing] ?? null) : null}
          taken={schedules.filter((_, i) => i !== editing).map((s) => s.name)}
          onSave={save}
          onClose={() => setEditing(null)}
        />
      )}
    </div>
  )
}
