// The Docker panel's tree and the shell commands it types into terminals; pure.
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

// Docker names and ids: [a-zA-Z0-9][a-zA-Z0-9_.-]* — anything else is refused
// rather than quoted, so nothing typed into a shell can carry metacharacters.
const NAME = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/

/** `docker exec` into a container with bash when it has it, else sh. */
export function shellCommand(name: string): string | null {
  if (!NAME.test(name)) return null
  return `docker exec -it ${name} sh -c 'command -v bash >/dev/null && exec bash || exec sh'`
}

/** Follows a container's log from its last lines. */
export function logsCommand(name: string, tail = 500): string | null {
  if (!NAME.test(name)) return null
  return `docker logs -f --tail ${Math.max(0, Math.floor(tail))} ${name}`
}
