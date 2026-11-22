import { Button } from '@flint-ide/ui'
import { useSettings } from '../../store/settings'
import { useEditor } from '../../store/editor'
import { closeMachinePicker, newTerminalOnMachine, useMachines } from '../../store/machines'
import { machineLine, machineLabel } from '../../lib/machines'
import { Modal } from './Modal'

/** "New Terminal on Machine…": the machines of Settings ▸ Machines; picking one opens a tab on it. */
export function MachinePicker() {
  const machines = useSettings((s) => s.settings.machines)
  const statuses = useMachines((s) => s.statuses)
  const pick = (id: string) => {
    closeMachinePicker()
    newTerminalOnMachine(id)
  }
  return (
    <Modal title="New Terminal on Machine" onClose={closeMachinePicker} className="tool-check">
      <p className="muted tool-check-intro">
        The shell starts in your home folder on that machine, over your own ssh keys. The first time per run Flint shows the exact ssh command and asks.
      </p>
      <div className="tool-check-list">
        {machines.length === 0 && <div className="muted tool-check-empty">No machines yet. Add one under Settings ▸ Machines.</div>}
        {machines.map((m) => {
          const status = statuses.find((s) => s.id === m.id)
          const state = status?.state
          const bad = state === 'failed'
          return (
            <div key={m.id} className="tool-row">
              <span className={`tool-dot ${state === 'connected' ? 'ok' : bad ? 'bad' : 'warn'}`} />
              <div className="tool-main">
                <div className="tool-head">
                  <span className="tool-name selectable">{machineLabel(m)}</span>
                  <span className="muted selectable">{m.port ? `${m.target}:${m.port}` : m.target}</span>
                  <span className="spacer" />
                  <Button variant="plain" size="sm" autoFocus={m === machines[0]} onClick={() => pick(m.id)}>
                    Open Terminal
                  </Button>
                </div>
                {status && state !== 'idle' && state !== 'closed' && <div className="muted">{machineLine(status)}</div>}
              </div>
            </div>
          )
        })}
      </div>
      <div className="form-actions">
        <Button
          variant="plain"
          onClick={() => {
            closeMachinePicker()
            useEditor.getState().openSettings('machines')
          }}
        >
          Machines Settings…
        </Button>
        <span className="spacer" />
        <Button variant="primary" onClick={closeMachinePicker}>
          Close
        </Button>
      </div>
    </Modal>
  )
}
