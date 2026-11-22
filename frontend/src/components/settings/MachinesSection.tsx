import { useEffect, useState } from 'react'
import { flushSettings, useSettings } from '../../store/settings'
import {
  DEFAULT_MACHINE_COMMAND,
  EMPTY_MACHINE,
  MAX_MACHINES,
  checkMachines,
  machineLabel,
  suggestMachineId,
  withMachine,
  withoutMachine,
  type CheckResult,
  type Machine,
} from '../../lib/machines'
import { checkMachine, checkMachineList, MachinesAPI, refreshMachines, useMachines } from '../../store/machines'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { CheckedField, NumberField, Row } from './fields'
import { Button, Input } from '@flint-ide/ui'

interface Install {
  notice: string
  releasesUrl: string
  command: string
  steps: { title: string; command: string; note: string }[]
}

/** Both verdicts: the mirror of Go's patterns answers at once, Go's own has the last word. */
const checkList = async (list: Machine[]): Promise<string> => checkMachines(list) || checkMachineList(list)

/** Settings ▸ Machines: the computers whose terminals run over ssh. */
export function MachinesSection() {
  const list = useSettings((s) => s.settings.machines)
  const issues = useMachines((s) => s.issues)
  useEffect(() => void refreshMachines(), [])
  return (
    <section>
      <h2>Machines</h2>
      <p className="muted settings-note">
        Terminals can run on another computer over your own ssh: key-based login only (ssh runs in BatchMode, so it never asks for a
        password), using your ssh configuration, agent and known hosts. Flint stores no credential and installs nothing there. Open one
        with "New Terminal on Machine…". Closing a tab ends its shell; closing Flint leaves it running on the machine. Files, the editor, git
        and search stay on this computer, and agent chats on a machine are not available yet.
      </p>
      {issues.length > 0 && (
        <div className="settings-note error" role="alert">
          <strong>Left out of settings.json:</strong>
          <ul>
            {issues.map((i) => (
              <li key={`${i.index}:${i.id}`}>
                <code>{i.id || `entry ${i.index + 1}`}</code>: {i.reason}
              </li>
            ))}
          </ul>
          Saving settings removes these entries; fix them in settings.json to keep them.
        </div>
      )}
      {list.map((machine, index) => (
        <MachineRows key={machine.id} machine={machine} index={index} list={list} />
      ))}
      {list.length < MAX_MACHINES && <AddMachine list={list} />}
    </section>
  )
}

/** Saves now (Go reads the machines when it saves) and refreshes what the page shows of them. */
const saveNow = () => void flushSettings().then(refreshMachines)

function MachineRows({ machine, index, list }: { machine: Machine; index: number; list: Machine[] }) {
  const update = useSettings((s) => s.update)
  const status = useMachines((s) => s.statuses.find((x) => x.id === machine.id))
  const [probe, setProbe] = useState<{ busy: boolean; result?: CheckResult; error?: string } | null>(null)
  const check = (change: Partial<Machine>) => checkList(withMachine(list, index, change))
  const commit = (change: Partial<Machine>) => {
    update((s) => ({ machines: withMachine(s.machines, index, change) }))
    setProbe(null)
    saveNow()
  }
  const remove = () => {
    update((s) => ({ machines: withoutMachine(s.machines, index) }))
    saveNow()
  }
  const runCheck = async () => {
    setProbe({ busy: true })
    await flushSettings() // Check takes the machine's id: Go runs what it has saved
    const { result, error } = await checkMachine(machine.id)
    setProbe({ busy: false, result, error })
  }
  return (
    <>
      <h3>
        {machineLabel(machine)} <span className="muted">· {status && status.state !== 'idle' ? status.state : 'not connected'}</span>
      </h3>
      <Row label="Id" hint="Names the machine in tabs. Lowercase letters, digits and dashes.">
        <CheckedField value={machine.id} check={(id) => check({ id })} onCommit={(id) => commit({ id })} />
      </Row>
      <Row label="Name" hint="Shown in tabs and menus; empty shows the id.">
        <CheckedField value={machine.name} placeholder={machine.id} check={(name) => check({ name })} onCommit={(name) => commit({ name })} />
      </Row>
      <Row label="Target" hint="host, user@host or an alias from ~/.ssh/config.">
        <CheckedField value={machine.target} check={(target) => check({ target })} onCommit={(target) => commit({ target })} />
      </Row>
      <Row label="Port" hint="0 leaves it to ssh (its configuration, else 22).">
        <NumberField value={machine.port} min={0} max={65535} step={1} onCommit={(port) => commit({ port: Math.round(port) })} />
      </Row>
      <Row label="Command" hint={`Flint on the machine: a name or a path, never with arguments (default ${DEFAULT_MACHINE_COMMAND}).`}>
        <CheckedField value={machine.command} check={(command) => check({ command })} onCommit={(command) => commit({ command })} />
      </Row>
      <div className="settings-actions">
        <Button variant="plain" onClick={() => void runCheck()} disabled={probe?.busy} title="Runs the command with --version over ssh and reads the answer">
          Check
        </Button>
        <Button variant="plain" onClick={remove}>Remove {machineLabel(machine)}</Button>
        {probe?.busy && <span className="muted">Asking the machine…</span>}
        {probe?.error && <span className="error">{probe.error}</span>}
        {probe?.result && <span className={probe.result.status === 'ok' ? 'muted' : 'error'}>{probe.result.message}</span>}
      </div>
      {probe?.result && (probe.result.status === 'notFound' || probe.result.status === 'incompatible') && <InstallSteps />}
    </>
  )
}

/** Copyable steps to install the headless host by hand; Flint runs none of them. */
function InstallSteps() {
  const [install, setInstall] = useState<Install | null>(null)
  useEffect(() => {
    void MachinesAPI.InstallSteps().then((i) => setInstall(i as Install), () => setInstall(null))
  }, [])
  if (!install) return null
  const copy = (text: string) => navigator.clipboard.writeText(text).then(() => showToast('Copied'), (err: unknown) => showToast(errorMessage(err), 'error'))
  return (
    <div className="settings-note">
      <strong>Install Flint's headless host on the machine</strong>
      <p className="muted">{install.notice}</p>
      <ol>
        {install.steps.map((step) => (
          <li key={step.title}>
            {step.title}
            {step.command && (
              <div>
                <code className="selectable">{step.command}</code> <Button variant="plain" onClick={() => void copy(step.command)}>Copy</Button>
              </div>
            )}
            {step.note && <div className="muted">{step.note}</div>}
          </li>
        ))}
      </ol>
      <p className="muted">Flint does not run these steps. Releases page: {install.releasesUrl}</p>
    </div>
  )
}

/** A form for a new machine. */
function AddMachine({ list }: { list: Machine[] }) {
  const update = useSettings((s) => s.update)
  const [draft, setDraft] = useState<Machine>(EMPTY_MACHINE)
  const [error, setError] = useState('')
  const add = async () => {
    const machine: Machine = {
      ...draft,
      id: draft.id.trim() || suggestMachineId(draft.name || draft.target, list),
      name: draft.name.trim(),
      target: draft.target.trim(),
      command: draft.command.trim() || DEFAULT_MACHINE_COMMAND,
    }
    const reason = await checkList([...list, machine]).catch((err: unknown) => errorMessage(err))
    setError(reason)
    if (reason) return
    update((s) => ({ machines: [...s.machines, machine] }))
    saveNow()
    setDraft(EMPTY_MACHINE)
  }
  const text = (key: 'id' | 'name' | 'target' | 'command', placeholder: string) => (
    <Input className="settings-text" value={draft[key]} placeholder={placeholder} spellCheck={false} onChange={(e) => setDraft({ ...draft, [key]: e.target.value })} />
  )
  return (
    <>
      <h3>Add a machine</h3>
      <Row label="Target" hint="host, user@host or an alias from ~/.ssh/config. ssh must log in with a key.">
        {text('target', 'me@build.example.com')}
      </Row>
      <Row label="Name" hint="Optional; shown in tabs.">
        {text('name', 'Build box')}
      </Row>
      <Row label="Port" hint="0 leaves it to ssh.">
        <NumberField value={draft.port} min={0} max={65535} step={1} onCommit={(port) => setDraft({ ...draft, port: Math.round(port) })} />
      </Row>
      <Row label="Command" hint={`Flint on the machine; default ${DEFAULT_MACHINE_COMMAND}.`}>
        {text('command', DEFAULT_MACHINE_COMMAND)}
      </Row>
      <Row label="Id" hint="Optional; derived from the name or target.">
        {text('id', suggestMachineId(draft.name || draft.target, list))}
      </Row>
      <div className="settings-actions">
        <Button variant="plain" onClick={() => void add()} disabled={!draft.target.trim()}>
          Add Machine
        </Button>
        {error && <span className="error">{error}</span>}
      </div>
    </>
  )
}
