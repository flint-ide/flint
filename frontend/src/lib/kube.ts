// Kubernetes labels and kubectl command lines, pure so they are testable.
import type { kube } from '../wails/models'

export type KubeContext = kube.Context
export type KubeOverview = kube.Overview
export type KubePod = kube.Pod
export type KubeWorkload = kube.Workload
export type KubeService = kube.Service

/**
 * A readable context name: EKS ARNs become "cluster (account)", GKE names
 * "cluster (project)"; anything else stays.
 */
export function shortContext(name: string): string {
  const eks = /^arn:aws:eks:[^:]+:(\d+):cluster\/(.+)$/.exec(name)
  if (eks) return `${eks[2]} (${eks[1]})`
  const gke = /^gke_([^_]+)_[^_]+_(.+)$/.exec(name)
  if (gke) return `${gke[2]} (${gke[1]})`
  return name
}

/** "40s", "12m", "3h", "5d" since an RFC 3339 time. */
export function age(created: string, now: number): string {
  const t = Date.parse(created)
  if (Number.isNaN(t)) return ''
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86_400) return `${Math.floor(s / 3600)}h`
  return `${Math.floor(s / 86_400)}d`
}

/** kubectl with an explicit context and namespace: the global current context doesn't matter. */
function kubectl(context: string, namespace: string, ...args: string[]): string[] {
  return ['kubectl', `--context=${context}`, ...(namespace ? [`--namespace=${namespace}`] : []), ...args]
}

/** `target` is "pod/name", "deployment/name"… */
export const logsCommand = (context: string, namespace: string, target: string) =>
  kubectl(context, namespace, 'logs', '-f', '--tail=200', '--all-containers', '--prefix', target)

/** A shell in a pod: bash when the image has it, else sh. */
export const execCommand = (context: string, namespace: string, pod: string, container?: string) =>
  kubectl(context, namespace, 'exec', '-it', pod, ...(container ? ['-c', container] : []), '--', 'sh', '-c', 'command -v bash >/dev/null && exec bash || exec sh')

export const describeCommand = (context: string, namespace: string, target: string) => kubectl(context, namespace, 'describe', target)

export const portForwardCommand = (context: string, namespace: string, target: string, port: number) =>
  kubectl(context, namespace, 'port-forward', target, `${port}:${port}`)

/** "pod/api-1", "deployment/api" from a kind as the API returns it. */
export const resourceRef = (kind: string, name: string) => `${kind.toLowerCase()}/${name}`

export type Tone = 'ok' | 'warn' | 'bad' | 'muted'

export function podTone(p: Pick<KubePod, 'status' | 'healthy'>): Tone {
  if (p.healthy) return 'ok'
  if (/BackOff|Err|Error|Failed|OOMKilled|Evicted|Invalid/i.test(p.status)) return 'bad'
  if (p.status === 'Completed' || p.status === 'Succeeded') return 'muted'
  return 'warn'
}

export function workloadTone(w: Pick<KubeWorkload, 'ready' | 'desired'>): Tone {
  if (w.desired === 0) return 'muted'
  if (w.ready >= w.desired) return 'ok'
  return w.ready === 0 ? 'bad' : 'warn'
}

/** Kubectl failures that mean the cluster login (e.g. AWS) expired. */
export const isAuthError = (message: string) =>
  /expired|ExpiredToken|InvalidClientTokenId|Unauthorized|You must be logged in|get-token|credentials|SSO session/i.test(message)

/** Matches what the filter box searches. */
export function matches(query: string, ...fields: (string | undefined)[]): boolean {
  const q = query.trim().toLowerCase()
  return !q || fields.some((f) => f?.toLowerCase().includes(q))
}
