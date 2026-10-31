import { createStore } from '../lib/store'
import * as DockerAPI from '../../bindings/flint/dockerapi'
import { docker } from '../wails/models'
import { errorMessage } from '../api/backend'
import { confirmAction, showToast } from './dialog'
import { useTerminal } from './terminal'
import { activeRoot } from './activeRoot'
import { logsCommand, shellCommand, type ComposeProject, type Container } from '../lib/dockerTree'

interface DockerState {
  containers: Container[]
  projects: ComposeProject[]
  loading: boolean
  error: string | null
  /** Container ids / project dirs with an action in flight. */
  busy: ReadonlySet<string>
}

export const useDocker = createStore<DockerState>(() => ({ containers: [], projects: [], loading: false, error: null, busy: new Set() }))

let pending: Promise<void> | null = null

export function refreshDocker(): Promise<void> {
  pending ??= (async () => {
    useDocker.setState({ loading: true })
    try {
      const o = await DockerAPI.Overview()
      useDocker.setState({ containers: o.containers ?? [], projects: o.projects ?? [], error: null })
    } catch (err) {
      useDocker.setState({ error: errorMessage(err), containers: [], projects: [] })
    } finally {
      useDocker.setState({ loading: false })
      pending = null
    }
  })()
  return pending
}

async function withBusy(key: string, fn: () => Promise<void>): Promise<void> {
  useDocker.setState((s) => ({ busy: new Set(s.busy).add(key) }))
  try {
    await fn()
  } catch (err) {
    showToast(errorMessage(err), 'error')
  } finally {
    useDocker.setState((s) => {
      const busy = new Set(s.busy)
      busy.delete(key)
      return { busy }
    })
    void refreshDocker()
  }
}

export async function containerAction(c: Container, action: 'start' | 'stop' | 'restart' | 'rm'): Promise<void> {
  if (action === 'rm' && !(await confirmAction(`Remove container ${c.name}?`, 'Remove'))) return
  return withBusy(c.id, () => DockerAPI.Act(c.id, action))
}

export async function composeAction(p: ComposeProject, action: 'up' | 'down' | 'restart' | 'pull' | 'stop'): Promise<void> {
  if (action === 'down' && !(await confirmAction(`Stop and remove the containers of ${p.name || p.dir}?`, 'Down'))) return
  return withBusy(p.dir, () => DockerAPI.Compose(docker.ComposeProject.createFrom(p), action))
}

/** Opens a terminal running `command` (docker logs / exec), named after the container. */
function runInTerminal(title: string, command: string | null): void {
  if (!command) {
    showToast('Unexpected container name', 'error')
    return
  }
  useTerminal.getState().create(activeRoot(), { title, input: `${command}\n` })
}

export const openLogs = (c: Container) => runInTerminal(`⛴ logs ${c.name}`, logsCommand(c.name))
export const openShell = (c: Container) => runInTerminal(`⛴ ${c.name}`, shellCommand(c.name))
