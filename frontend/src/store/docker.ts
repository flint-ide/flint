import { createStore } from '../lib/store'
import * as DockerAPI from '../../bindings/flint/dockerapi'
import { docker } from '../wails/models'
import { errorMessage } from '../api/backend'
import { confirmAction, showToast } from './dialog'
import { useTerminal } from './terminal'
import { activeRoot } from './activeRoot'
import { validContainerRef, type ComposeProject, type Container } from '../lib/dockerTree'

interface DockerState {
  containers: Container[]
  projects: ComposeProject[]
  loading: boolean
  error: string | null
  /** The docker CLI is installed: Compose up, down and pull need it. */
  composeAvailable: boolean
  /** Where the engine was found. */
  endpoint: string
  /** The connection is not protected (plain tcp://). */
  warning: string
  /** Container ids / project dirs with an action in flight. */
  busy: ReadonlySet<string>
}

export const useDocker = createStore<DockerState>(() => ({ containers: [], projects: [], loading: false, error: null, composeAvailable: true, endpoint: '', warning: '', busy: new Set() }))

let pending: Promise<void> | null = null

export function refreshDocker(): Promise<void> {
  pending ??= (async () => {
    useDocker.setState({ loading: true })
    try {
      const o = await DockerAPI.Overview()
      useDocker.setState({
        containers: o.containers ?? [], projects: o.projects ?? [], error: null,
        composeAvailable: o.composeAvailable, endpoint: o.endpoint, warning: o.warning,
      })
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

/** Opens a terminal tab attached to a container's log or a shell in it (Engine API streams). */
function openStream(c: Container, kind: 'logs' | 'shell'): void {
  if (!validContainerRef(c.name)) {
    showToast('Unexpected container name', 'error')
    return
  }
  const title = kind === 'logs' ? `Logs ${c.name}` : `Shell ${c.name}`
  useTerminal.getState().create(activeRoot(), { title, docker: { kind, container: c.name } })
}

export const openLogs = (c: Container) => openStream(c, 'logs')
export const openShell = (c: Container) => openStream(c, 'shell')
