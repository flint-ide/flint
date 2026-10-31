// Kubernetes labels and kubectl command lines, pure so they are testable.
import type { kube } from '../wails/models'

export type KubeContext = kube.Context
export type KubeOverview = kube.Overview
export type KubePod = kube.Pod
export type KubeWorkload = kube.Workload
export type KubeService = kube.Service
export type KubeIngress = kube.Ingress
export type KubeApp = kube.App
export type KubeEvent = kube.Event
export type KubeKustomization = kube.Kustomization

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

export const rolloutStatusCommand = (context: string, namespace: string, target: string) => kubectl(context, namespace, 'rollout', 'status', target)

export const rolloutHistoryCommand = (context: string, namespace: string, target: string) => kubectl(context, namespace, 'rollout', 'history', target)

/** `flux reconcile` of a Kustomization and its source, in the Kustomization's namespace. */
export const fluxReconcileCommand = (context: string, k: Pick<KubeKustomization, 'name' | 'namespace'>) =>
  ['flux', `--context=${context}`, 'reconcile', 'kustomization', k.name, `--namespace=${k.namespace}`, '--with-source']

/** The rollout badge of a workload: "rev 7 ✓", "rolling out", "failed"… */
export function rolloutBadge(w: Pick<KubeWorkload, 'rollout' | 'revision' | 'rolloutMessage'>): { text: string; tone: Tone; title: string } {
  const rev = w.revision ? `rev ${w.revision}` : ''
  switch (w.rollout) {
    case 'complete':
      return { text: rev ? `${rev} ✓` : '✓', tone: 'ok', title: 'Rolled out' }
    case 'progressing':
      return { text: 'rolling out', tone: 'warn', title: w.rolloutMessage || 'Rollout in progress' }
    case 'failed':
      return { text: 'failed', tone: 'bad', title: w.rolloutMessage || 'Rollout failed' }
    case 'paused':
      return { text: 'paused', tone: 'warn', title: 'Rollout paused' }
    case 'scaled-down':
      return { text: 'scaled to 0', tone: 'muted', title: 'No replicas' }
  }
  return { text: w.rollout, tone: 'muted', title: '' }
}

export const healthTone = (health: string): Tone => (health === 'ok' ? 'ok' : health === 'bad' ? 'bad' : 'warn')

const SOURCE_LABELS: Record<string, string> = { flux: 'Flux', argo: 'Argo CD', helm: 'Helm', label: 'label', workload: '' }
export const appSourceLabel = (source: string) => SOURCE_LABELS[source] ?? source

/** "abc123" from Flux's "main@sha1:abc123def…" (branch kept when present). */
export function shortRevision(revision: string): string {
  const m = /^(?:(.+)@)?(?:sha1|sha256):([0-9a-f]{7})[0-9a-f]*$/.exec(revision)
  return m ? (m[1] ? `${m[1]}@${m[2]}` : m[2]) : revision
}

/**
 * The agent prompt for an unhealthy app: read-only investigation with the
 * cluster pinned, the fix proposed where it belongs (manifests in the repo),
 * the gathered diagnostics attached.
 */
export function diagnosePrompt(context: string, namespace: string, app: string, diagnostics: string): string {
  return [
    `The Kubernetes app "${app}" in namespace ${namespace} (context ${context}) is unhealthy. Find the root cause.`,
    '',
    `- Investigate with read-only kubectl only, always with --context=${context} --namespace=${namespace} (get, describe, logs, events, rollout history).`,
    '- Do not change the cluster (no apply, edit, delete, scale, rollout restart/undo) unless I ask.',
    '- If the fix belongs in manifests in this repository (Kustomize, Helm, Flux), find them and propose the change there.',
    '- Start with the root cause in one or two sentences, then the evidence, then the fix.',
    '',
    'What Flint gathered (secrets redacted):',
    '',
    diagnostics.trim(),
  ].join('\n')
}
