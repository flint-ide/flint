import { useMemo, useState } from 'react'
import {
  describe, openLogs, openRolloutHistory, openRolloutStatus, openShell, portForward, reconcileFlux, restartWorkload,
  scaleWorkload, undoRollout, deletePod, diagnoseWithAI, useKube,
} from './store'
import { openContextMenu } from '../../store/contextMenu'
import {
  appSourceLabel, healthTone, matches, podTone, resourceRef, rolloutBadge, shortRevision,
  type KubeApp, type KubeOverview, type KubePod, type KubeWorkload,
} from './kube'
import type { DropdownItem } from '../../components/ui/Dropdown'

/** Problems listed under an app before "n more". */
const MAX_PROBLEMS = 6

/**
 * Apps of the namespace (Flux Kustomization, Argo CD, Helm release, app label
 * or a lone workload), worst first: is the deploy rolled out, what is broken
 * and where — ingress → service → workload → pods, with warning events.
 */
export function KubeApps({ overview }: { overview: KubeOverview }) {
  const query = useKube((s) => s.query)
  const busy = useKube((s) => s.busy)
  // Unhealthy apps start open, healthy ones closed; a click decides for good.
  const [chosen, setChosen] = useState<Readonly<Record<string, boolean>>>({})
  const [moreProblems, setMoreProblems] = useState<ReadonlySet<string>>(new Set())

  const apps = useMemo(
    () => (overview.apps ?? []).filter((a) => matches(query, a.name, ...a.workloads, ...a.services, ...a.ingresses)),
    [overview.apps, query],
  )
  const byName = useMemo(() => {
    const workloads = new Map(overview.workloads.map((w) => [`${w.kind}/${w.name}`, w]))
    const services = new Map(overview.services.map((s) => [s.name, s]))
    const ingresses = new Map((overview.ingresses ?? []).map((i) => [i.name, i]))
    const pods = new Map<string, KubePod[]>()
    for (const p of overview.pods) if (p.owner) pods.set(p.owner, [...(pods.get(p.owner) ?? []), p])
    return { workloads, services, ingresses, pods }
  }, [overview])

  const isOpen = (a: KubeApp) => chosen[a.key] ?? a.health !== 'ok'
  const flip = (set: typeof setMoreProblems, key: string) =>
    set((prev) => {
      const next = new Set(prev)
      if (!next.delete(key)) next.add(key)
      return next
    })
  const menu = (items: DropdownItem[]) => (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    openContextMenu(e.clientX, e.clientY, items)
  }

  const workloadMenu = (w: KubeWorkload, app: KubeApp): DropdownItem[] => [
    ...(app.health !== 'ok' ? [{ label: 'Diagnose with AI', hint: 'agent chat', onSelect: () => void diagnoseWithAI(app) } as DropdownItem, { type: 'separator' } as DropdownItem] : []),
    { label: 'Rollout Status', hint: 'kubectl rollout status', onSelect: () => openRolloutStatus(w) },
    { label: 'Rollout History', onSelect: () => openRolloutHistory(w) },
    { label: 'Logs', onSelect: () => openLogs(resourceRef(w.kind, w.name)) },
    { label: 'Describe', onSelect: () => describe(w.kind, w.name) },
    { type: 'separator' },
    { label: 'Rollout Restart', onSelect: () => void restartWorkload(w) },
    { label: 'Undo Rollout', hint: 'previous revision', danger: true, onSelect: () => void undoRollout(w) },
    ...(w.kind === 'DaemonSet' ? [] : ([
      { label: `Scale to ${w.desired + 1}`, onSelect: () => void scaleWorkload(w, w.desired + 1) },
      ...(w.desired > 0 ? [{ label: `Scale to ${w.desired - 1}`, onSelect: () => void scaleWorkload(w, w.desired - 1) }] : []),
    ] as DropdownItem[])),
  ]

  const podMenu = (p: KubePod): DropdownItem[] => [
    { label: 'Logs', onSelect: () => openLogs(`pod/${p.name}`) },
    { label: 'Shell', onSelect: () => openShell(p) },
    { label: 'Describe', onSelect: () => describe('Pod', p.name) },
    ...p.ports.map((port) => ({ label: `Port Forward :${port}`, onSelect: () => portForward(`pod/${p.name}`, port) }) as DropdownItem),
    { type: 'separator' },
    { label: 'Delete Pod', danger: true, onSelect: () => void deletePod(p) },
  ]

  if (apps.length === 0) return <div className="placeholder muted">{query ? 'Nothing matches.' : 'No workloads in this namespace.'}</div>

  return (
    <>
      {apps.map((a) => {
        const open = isOpen(a)
        // A pod's own problem (and its warning events) is shown under the pod; the summary keeps the rest.
        const shownPods = a.workloads.flatMap((ref) => byName.pods.get(ref.slice(ref.indexOf('/') + 1)) ?? []).filter((p) => p.problem)
        const listed = a.problems
          .map((text, i) => ({ text, critical: i < a.critical }))
          .filter((p) => !shownPods.some((pod) => p.text.startsWith(`${pod.name}: `) || p.text.startsWith(`Pod ${pod.name}: `)))
        const problems = moreProblems.has(a.key) ? listed : listed.slice(0, MAX_PROBLEMS)
        const k = a.kustomization
        return (
          <div key={a.key} className="kube-app">
            <div className="tree-row static kube-app-row" onClick={() => setChosen((c) => ({ ...c, [a.key]: !open }))} title={a.problems.join('\n') || 'Healthy'}>
              <span className="twisty">{open ? '▾' : '▸'}</span>
              <span className={`kube-dot ${healthTone(a.health)}`}>●</span>
              <span className="file-name">{a.name}</span>
              {appSourceLabel(a.source) && <span className="kube-source">{appSourceLabel(a.source)}</span>}
              <span className="muted path-hint">{a.problems.length ? `${a.problems.length} issue${a.problems.length === 1 ? '' : 's'}` : 'healthy'}</span>
              {a.health !== 'ok' && (
                <button
                  className="icon kube-ai"
                  title="Diagnose with AI: gathers the state and failing pods' logs (secrets redacted) into the agent chat for you to review and send"
                  disabled={busy.has(`app:${a.key}`)}
                  onClick={(e) => (e.stopPropagation(), void diagnoseWithAI(a))}
                >
                  {busy.has(`app:${a.key}`) ? '…' : '✦ AI'}
                </button>
              )}
            </div>
            {open && (
              <div className="kube-app-body">
                {k && (
                  <div className="tree-row static kube-item" onContextMenu={menu([{ label: 'Reconcile', hint: 'flux reconcile --with-source', onSelect: () => reconcileFlux(k) }, { label: 'Describe', onSelect: () => describe('kustomizations.kustomize.toolkit.fluxcd.io', k.name) }])} title={`${k.namespace}/${k.name}\n${k.message ?? ''}`}>
                    <span className="kube-kind">KS</span>
                    <span className="file-name">{k.name}</span>
                    <span className="muted path-hint">{k.revision ? shortRevision(k.revision) : k.namespace}</span>
                    <span className={`aws-status ${k.suspended ? 'warn' : k.ready === 'True' ? 'ok' : k.ready === 'False' ? 'bad' : 'warn'}`}>{k.suspended ? 'suspended' : k.ready === 'True' ? 'ready' : k.ready === 'False' ? 'not ready' : 'reconciling'}</span>
                  </div>
                )}
                {problems.map((p, i) => (
                  <div key={i} className={`kube-problem ${p.critical ? 'bad' : 'warn'}`}>{p.text}</div>
                ))}
                {listed.length > MAX_PROBLEMS && (
                  <button className="link kube-more" onClick={() => flip(setMoreProblems, a.key)}>
                    {moreProblems.has(a.key) ? 'Show less' : `${listed.length - MAX_PROBLEMS} more…`}
                  </button>
                )}
                {a.ingresses.map((name) => {
                  const ing = byName.ingresses.get(name)
                  if (!ing) return null
                  return ing.routes.map((r, i) => (
                    <div key={`${name}-${i}`} className="tree-row static kube-item" onContextMenu={menu([{ label: 'Describe', onSelect: () => describe('Ingress', name) }])} title={`Ingress ${name}${ing.class ? ` (${ing.class})` : ''}${ing.address ? `\n${ing.address}` : ''}`}>
                      <span className="kube-kind">ING</span>
                      <span className="file-name">{`${r.host}${r.path ?? ''}`}</span>
                      <span className="muted path-hint">{`→ ${r.service}:${r.port}`}</span>
                      <span className="aws-status muted">{ing.tls ? '🔒' : 'http'}</span>
                    </div>
                  ))
                })}
                {a.services.map((name) => {
                  const s = byName.services.get(name)
                  if (!s) return null
                  return (
                    <div key={name} className="tree-row static kube-item" onContextMenu={menu([...s.ports.map((port) => ({ label: `Port Forward :${port.port}`, onSelect: () => portForward(`service/${s.name}`, port.port) }) as DropdownItem), { label: 'Describe', onSelect: () => describe('Service', s.name) }])} title={`${s.type} ${s.clusterIP}\n${s.ready} of ${s.backends} backing pods ready`}>
                      <span className="kube-kind">SVC</span>
                      <span className="file-name">{s.name}</span>
                      <span className="muted path-hint">{s.ports.map((p) => p.port).join(', ')}</span>
                      <span className={`aws-status ${s.backends === 0 ? 'muted' : s.ready === 0 ? 'bad' : s.ready < s.backends ? 'warn' : 'ok'}`}>{`${s.ready}/${s.backends}`}</span>
                    </div>
                  )
                })}
                {a.workloads.map((ref) => {
                  const w = byName.workloads.get(ref)
                  if (!w) return null
                  const badge = rolloutBadge(w)
                  return (
                    <div key={ref}>
                      <div className="tree-row static kube-item" onContextMenu={menu(workloadMenu(w, a))} onDoubleClick={() => openRolloutStatus(w)} title={`${w.kind} ${w.name}\n${badge.title}\n${w.images.join('\n')}`}>
                        <span className="kube-kind">{w.kind === 'Deployment' ? 'D' : w.kind === 'StatefulSet' ? 'STS' : 'DS'}</span>
                        <span className="file-name">{w.name}</span>
                        <span className="muted path-hint">{`${w.ready}/${w.desired} · ${w.images.map((i) => i.split('/').pop()).join(', ')}`}</span>
                        <span className={`aws-status ${badge.tone}`}>{busy.has(ref) ? '…' : badge.text}</span>
                      </div>
                      {(byName.pods.get(w.name) ?? []).map((p) => (
                        <div key={p.name}>
                          <div className="tree-row static kube-item kube-pod" onContextMenu={menu(podMenu(p))} onDoubleClick={() => openLogs(`pod/${p.name}`)} title={`${p.name}${p.problem ? `\n${p.problem}` : ''}\nDouble-click: logs`}>
                            <span className={`kube-dot ${podTone(p)}`}>●</span>
                            <span className="file-name">{p.name}</span>
                            <span className="muted path-hint">{[p.old && 'old revision', p.restarts ? `↺${p.restarts}` : ''].filter(Boolean).join(' · ')}</span>
                            <span className={`aws-status ${podTone(p)}`}>{p.status === 'Running' ? `${p.ready}/${p.total}` : p.status}</span>
                          </div>
                          {p.problem && <div className="kube-problem bad kube-pod-problem">{p.problem}</div>}
                        </div>
                      ))}
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )
      })}
    </>
  )
}
