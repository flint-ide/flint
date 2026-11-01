// The Docker panel's tree and container-name check; pure.
import type { docker } from '../wails/models'

export type Container = docker.Container
export type ComposeProject = docker.ComposeProject

export interface ProjectNode {
  project: ComposeProject
  /** Containers by service, in service order. */
  containers: Container[]
  running: number
}

/** Compose projects (with their containers) and the containers outside any. */
export function dockerTree(containers: readonly Container[], projects: readonly ComposeProject[]): { projects: ProjectNode[]; others: Container[] } {
  const nodes: ProjectNode[] = projects.map((project) => ({ project, containers: [], running: 0 }))
  const byDir = new Map(nodes.map((n) => [n.project.dir, n]))
  const others: Container[] = []
  for (const c of containers) {
    const node = c.project ? byDir.get(c.workingDir) : undefined
    if (!node) {
      others.push(c)
      continue
    }
    node.containers.push(c)
    if (c.state === 'running') node.running++
  }
  for (const n of nodes) n.containers.sort((a, b) => a.service.localeCompare(b.service) || a.name.localeCompare(b.name))
  others.sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running') || a.name.localeCompare(b.name))
  return { projects: nodes, others }
}

/** Status dot: running, unhealthy, paused, or stopped. */
export function statusOf(c: Pick<Container, 'state' | 'status'>): 'running' | 'unhealthy' | 'paused' | 'stopped' {
  if (c.state === 'running') return /\bunhealthy\b/.test(c.status) ? 'unhealthy' : 'running'
  if (c.state === 'paused') return 'paused'
  return 'stopped'
}

// Docker names and ids: [a-zA-Z0-9][a-zA-Z0-9_.-]* — anything else is refused before it
// reaches Go (which checks again), so nothing odd ends up in a request path.
const NAME = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$/

/**
 * Class of a panel row. `.tree-row` is position:absolute (the virtualized file
 * tree places its rows itself); rows laid out in the sidebar flow need `static`,
 * or they all stack at the window's top-left over the title bar.
 */
export const rowClass = (...extra: string[]): string => ['tree-row', 'static', ...extra.filter(Boolean)].join(' ')

/** Whether name is a container id or name the engine requests accept. */
export const validContainerRef = (name: string): boolean => NAME.test(name)
