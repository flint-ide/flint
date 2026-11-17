import * as MachinesAPI from '../../bindings/flint/machinesapi'
import { createStore } from '../lib/store'
import { EventsOn } from '../wails/runtime'
import { withStatus, type CheckResult, type MachineIssue, type MachineStatus } from '../lib/machines'
import { errorMessage } from '../api/backend'
import { settings as models } from '../wails/models'
import { activeRoot } from './activeRoot'
import { showToast } from './dialog'
import { useSettings } from './settings'
import { useTerminal } from './terminal'

// Remote machines (Settings ▸ Machines): terminals that run on another computer over the
// user's own ssh. Go owns the connections; this store keeps what the UI shows of them
// (machine:status events) and starts the tabs. Nothing here connects on its own: the first
// terminal of a machine asks the user in a native dialog.

interface MachinesState {
  /** Connection state per machine, from machine:status. */
  statuses: MachineStatus[]
  /** Entries of settings.json that validation dropped, with the reason. */
  issues: MachineIssue[]
  /** The "New Terminal on Machine…" picker is open. */
  pickerOpen: boolean
}

export const useMachines = createStore<MachinesState>(() => ({ statuses: [], issues: [], pickerOpen: false }))

let subscribed = false
/** Listens for connection changes; reads the current states once. */
export function subscribeMachineEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('machine:status', (st: MachineStatus) => useMachines.setState((s) => ({ statuses: withStatus(s.statuses, st) })))
  void refreshMachines()
}

/** Asks Go for the machines' states and the entries dropped from settings.json. */
export async function refreshMachines(): Promise<void> {
  try {
    const [statuses, issues] = await Promise.all([MachinesAPI.Status(), MachinesAPI.Issues()])
    useMachines.setState({ statuses: (statuses ?? []) as MachineStatus[], issues: (issues ?? []) as MachineIssue[] })
  } catch {
    // Settings then shows what it has; the next event or refresh fills it in.
  }
}

/** The machines Go has configured now, for restoring tabs (settings may not have loaded yet). */
export async function configuredMachineIds(): Promise<Set<string>> {
  try {
    return new Set(((await MachinesAPI.Status()) ?? []).map((s) => s.id))
  } catch {
    return new Set()
  }
}

export function openMachinePicker(): void {
  useMachines.setState({ pickerOpen: true })
}

export function closeMachinePicker(): void {
  useMachines.setState({ pickerOpen: false })
}

/** Opens a terminal tab on a machine. The shell starts in the remote home folder. */
export function newTerminalOnMachine(machineId: string): void {
  if (!useSettings.getState().settings.machines.some((m) => m.id === machineId)) {
    showToast(`No machine "${machineId}" in Settings ▸ Machines`, 'error')
    return
  }
  // The project is only where the tab belongs for the session file; nothing of it goes to the machine.
  const root = activeRoot()
  useTerminal.getState().create('', { machine: machineId, project: root })
}

/** Go's verdict on a list of machines ('' = it would be saved). */
export const checkMachineList = (list: Parameters<typeof models.Machine.createFrom>[0][]): Promise<string> =>
  MachinesAPI.CheckList(list.map((m) => models.Machine.createFrom(m)))

/** Runs the Check of a saved machine (after the native confirmation). */
export async function checkMachine(id: string): Promise<{ result?: CheckResult; error?: string }> {
  try {
    return { result: (await MachinesAPI.Check(id)) as CheckResult }
  } catch (err) {
    return { error: errorMessage(err) }
  }
}

export { MachinesAPI }
