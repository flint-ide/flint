import { createStore } from '../lib/store'
import * as KubeAPI from '../../bindings/flint/kubeapi'
import { errorMessage } from '../api/backend'
import {
  describeCommand, diagnosePrompt, execCommand, fluxReconcileCommand, logsCommand, portForwardCommand, resourceRef, rolloutHistoryCommand,
  rolloutStatusCommand, shortContext, type KubeApp, type KubeContext, type KubeKustomization, type KubeOverview, type KubePod, type KubeWorkload,
} from '../lib/kube'
import { shellCommand } from '../lib/shellArgs'
import { useTerminal } from './terminal'
import { activeRoot } from './activeRoot'
import { confirmAction, showToast } from './dialog'
import { askAgent } from '../lib/askAgent'

/** Remembered between launches: the context shown and each context's namespace. */
const STORAGE_KEY = 'flint.kube'

export type KubeView = 'apps' | 'resources'

interface Saved {
  context: string
  namespaces: Record<string, string>
  view: KubeView
}

function readSaved(): Saved {
  try {
    const v = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
    return {
      context: typeof v.context === 'string' ? v.context : '',
      namespaces: v.namespaces && typeof v.namespaces === 'object' ? v.namespaces : {},
      view: v.view === 'resources' ? 'resources' : 'apps',
    }
  } catch {
    return { context: '', namespaces: {}, view: 'apps' }
  }
}

function writeSaved(s: Saved): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(s))
  } catch {
    // private mode / storage full: the choice just isn't remembered
  }
}

interface KubeState {
  contexts: KubeContext[]
  contextsError: string | null
  context: string
  namespaces: string[]
  namespace: string
  overview: KubeOverview | null
  loading: boolean
  error: string | null
  query: string
  /** Rows with an action running ("Deployment/api"). */
  busy: ReadonlySet<string>
  /** Apps (grouped, for debugging deploys) or the flat resource lists. */
  view: KubeView
}

const saved = readSaved()

export const useKube = createStore<KubeState>(() => ({
  contexts: [], contextsError: null, context: saved.context, namespaces: [], namespace: '',
  overview: null, loading: false, error: null, query: '', busy: new Set(), view: saved.view,
}))

export const setKubeQuery = (query: string) => useKube.setState({ query })

export function setKubeView(view: KubeView): void {
  useKube.setState({ view })
  writeSaved({ ...readSaved(), view })
}

export const selectedContext = (): KubeContext | undefined => {
  const { contexts, context } = useKube.getState()
  return contexts.find((c) => c.name === context)
}

/** Reads the kubeconfig; keeps the chosen context when it still exists, else the current one. */
export async function loadContexts(): Promise<void> {
  try {
    const contexts = (await KubeAPI.Contexts()) ?? []
    const { context } = useKube.getState()
    const keep = contexts.some((c) => c.name === context) ? context : (contexts.find((c) => c.current)?.name ?? contexts[0]?.name ?? '')
    useKube.setState({ contexts, contextsError: null })
    if (keep !== context || !useKube.getState().namespace) await selectContext(keep)
  } catch (err) {
    useKube.setState({ contextsError: errorMessage(err), contexts: [] })
  }
}

/** Shows another context (the kubeconfig's current context is left alone). */
export async function selectContext(name: string): Promise<void> {
  const ctx = useKube.getState().contexts.find((c) => c.name === name)
  const namespace = readSaved().namespaces[name] || ctx?.namespace || 'default'
  useKube.setState({ context: name, namespace, namespaces: [], overview: null, error: null })
  writeSaved({ ...readSaved(), context: name })
  if (!name) return
  void refreshOverview()
  try {
    const namespaces = (await KubeAPI.Namespaces(name)) ?? []
    if (useKube.getState().context === name) useKube.setState({ namespaces })
  } catch {
    // listing namespaces may be forbidden: the context's own namespace still works
  }
}

export function selectNamespace(namespace: string): void {
  const { context } = useKube.getState()
  const s = readSaved()
  writeSaved({ ...s, namespaces: { ...s.namespaces, [context]: namespace } })
  useKube.setState({ namespace, overview: null, error: null })
  void refreshOverview()
}

let inFlight: Promise<void> | null = null

/** Re-reads the shown namespace; an answer for a context or namespace switched away from is dropped. */
export function refreshOverview(): Promise<void> {
  inFlight ??= (async () => {
    const { context, namespace } = useKube.getState()
    if (!context) return
    useKube.setState({ loading: true })
    try {
      const overview = await KubeAPI.Overview(context, namespace)
      const now = useKube.getState()
      if (now.context === context && now.namespace === namespace) useKube.setState({ overview, error: null })
    } catch (err) {
      const now = useKube.getState()
      if (now.context === context && now.namespace === namespace) useKube.setState({ error: errorMessage(err) })
    } finally {
      useKube.setState({ loading: false })
      inFlight = null
    }
  })()
  return inFlight
}

async function act(key: string, run: () => Promise<void>, done: string): Promise<void> {
  useKube.setState((s) => ({ busy: new Set(s.busy).add(key) }))
  try {
    await run()
    showToast(done)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  } finally {
    useKube.setState((s) => {
      const busy = new Set(s.busy)
      busy.delete(key)
      return { busy }
    })
    void refreshOverview()
  }
}

export async function restartWorkload(w: KubeWorkload): Promise<void> {
  const { context, namespace } = useKube.getState()
  if (!(await confirmAction(`Restart all pods of ${w.kind} ${w.name} in ${shortContext(context)}?`, 'Restart'))) return
  await act(`${w.kind}/${w.name}`, () => KubeAPI.RolloutRestart(context, namespace, w.kind, w.name), `Restarting ${w.name}`)
}

export async function scaleWorkload(w: KubeWorkload, replicas: number): Promise<void> {
  const { context, namespace } = useKube.getState()
  if (replicas === 0 && !(await confirmAction(`Scale ${w.name} to 0? All its pods stop.`, 'Scale to 0'))) return
  await act(`${w.kind}/${w.name}`, () => KubeAPI.Scale(context, namespace, w.kind, w.name, replicas), `${w.name} scaled to ${replicas}`)
}

export async function deletePod(p: KubePod): Promise<void> {
  const { context, namespace } = useKube.getState()
  if (!(await confirmAction(`Delete pod ${p.name}?${p.owner ? ` ${p.owner} starts a new one.` : ''}`, 'Delete'))) return
  await act(`Pod/${p.name}`, () => KubeAPI.DeletePod(context, namespace, p.name), `Deleting ${p.name}`)
}

/** Runs a kubectl command in a terminal tab named title. */
function runInTerminal(title: string, argv: string[]): void {
  useTerminal.getState().create(activeRoot(), { title, input: shellCommand(argv) })
}

export function openLogs(target: string): void {
  const { context, namespace } = useKube.getState()
  runInTerminal(`logs: ${target.split('/').pop()}`, logsCommand(context, namespace, target))
}

export function openShell(p: KubePod, container?: string): void {
  const { context, namespace } = useKube.getState()
  runInTerminal(`sh: ${p.name}`, execCommand(context, namespace, p.name, container))
}

export function describe(kind: string, name: string): void {
  const { context, namespace } = useKube.getState()
  runInTerminal(`describe: ${name}`, describeCommand(context, namespace, resourceRef(kind, name)))
}

export function portForward(target: string, port: number): void {
  const { context, namespace } = useKube.getState()
  runInTerminal(`:${port} → ${target.split('/').pop()}`, portForwardCommand(context, namespace, target, port))
}

/** A shell whose kubectl works in the shown context and namespace (KUBECONFIG override, nothing global changes). */
export async function openContextTerminal(): Promise<void> {
  const { context, namespace } = useKube.getState()
  try {
    const env = await KubeAPI.TerminalEnv(context, namespace)
    useTerminal.getState().create(activeRoot(), { env: (env ?? {}) as Record<string, string>, tag: `k8s:${shortContext(context).split(' ')[0]}/${namespace}` })
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Makes the shown context the kubeconfig's current one (for every terminal and tool). */
export async function useAsCurrent(): Promise<void> {
  const { context } = useKube.getState()
  try {
    await KubeAPI.UseContext(context)
    showToast(`Current context: ${shortContext(context)}`)
    await loadContexts()
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export function openRolloutStatus(w: KubeWorkload): void {
  const { context, namespace } = useKube.getState()
  runInTerminal(`rollout: ${w.name}`, rolloutStatusCommand(context, namespace, resourceRef(w.kind, w.name)))
}

export function openRolloutHistory(w: KubeWorkload): void {
  const { context, namespace } = useKube.getState()
  runInTerminal(`history: ${w.name}`, rolloutHistoryCommand(context, namespace, resourceRef(w.kind, w.name)))
}

/** Rolls a workload back to its previous revision (after asking). */
export async function undoRollout(w: KubeWorkload): Promise<void> {
  const { context, namespace } = useKube.getState()
  const from = w.revision ? ` from revision ${w.revision}` : ''
  if (!(await confirmAction(`Roll ${w.kind} ${w.name} back${from} to the previous revision in ${shortContext(context)}?`, 'Undo Rollout'))) return
  await act(`${w.kind}/${w.name}`, () => KubeAPI.RolloutUndo(context, namespace, w.kind, w.name), `Rolling back ${w.name}`)
}

/** `flux reconcile kustomization … --with-source` in a terminal. */
export function reconcileFlux(k: KubeKustomization): void {
  const { context } = useKube.getState()
  runInTerminal(`flux: ${k.name}`, fluxReconcileCommand(context, k))
}

/**
 * Gathers the app's state and failing pods' logs (redacted in Go) and puts a
 * diagnosis request in the agent chat's draft: it is reviewed before anything
 * reaches the AI provider.
 */
export async function diagnoseWithAI(app: KubeApp): Promise<void> {
  const { context, namespace } = useKube.getState()
  const key = `app:${app.key}`
  useKube.setState((s) => ({ busy: new Set(s.busy).add(key) }))
  try {
    const diagnostics = await KubeAPI.Diagnose(context, namespace, app.key)
    const id = await askAgent({ prompt: diagnosePrompt(context, namespace, app.name, diagnostics), root: activeRoot() })
    if (id) showToast('Diagnosis ready in the agent chat — review it and press Enter to send')
  } catch (err) {
    showToast(errorMessage(err), 'error')
  } finally {
    useKube.setState((s) => {
      const busy = new Set(s.busy)
      busy.delete(key)
      return { busy }
    })
  }
}
