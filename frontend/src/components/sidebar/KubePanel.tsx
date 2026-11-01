import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  deletePod, describe, loadContexts, openContextTerminal, openLogs, openShell, portForward, refreshOverview,
  restartWorkload, scaleWorkload, selectContext, selectNamespace, setKubeQuery, setKubeView, useAsCurrent, useKube,
} from '../../store/kube'
import { KubeApps } from './KubeApps'
import { loginProfile, refreshAws, refreshProfile, useAws } from '../../store/aws'
import { openContextMenu } from '../../store/contextMenu'
import { profileStatus } from '../../lib/aws'
import {
  age, isAuthError, matches, podTone, resourceRef, shortContext, workloadTone,
  type KubePod, type KubeService, type KubeWorkload,
} from '../../lib/kube'
import { Select } from '../ui/Select'
import type { DropdownItem } from '../ui/Dropdown'
import { useSettings } from '../../store/settings'
import { refreshMs } from '../../lib/toolSettings'
import { Icon } from '../ui/Icon'

/** One Kubernetes context at a time (picked here, the global current context untouched): workloads, pods, services, jobs. */
export function KubePanel() {
  const { contexts, contextsError, context, namespaces, namespace, overview, loading, error, query, busy, view } = useKube()
  const awsProfiles = useAws((s) => s.profiles)
  const awsRefreshes = useAws((s) => s.refreshes)
  const [now, setNow] = useState(Date.now)
  const [closed, setClosed] = useState<ReadonlySet<string>>(new Set())
  // While shown and the window is visible, the namespace is re-read this often (Settings ▸ Refresh).
  const pollMs = useSettings((s) => refreshMs(s.settings.refresh, 'kubernetesSeconds'))

  useEffect(() => {
    void loadContexts()
    void refreshAws()
  }, [])

  useEffect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState !== 'visible') return
      setNow(Date.now())
      void refreshOverview()
    }, pollMs)
    return () => clearInterval(timer)
  }, [pollMs])

  const ctx = contexts.find((c) => c.name === context)
  const aws = ctx?.awsProfile ? awsProfiles.find((p) => p.name === ctx.awsProfile) : undefined
  const awsStatus = aws ? profileStatus(aws, now) : null

  const shown = useMemo(() => {
    const ov = overview
    return {
      workloads: ov?.workloads?.filter((w) => matches(query, w.name, w.kind)) ?? [],
      pods: ov?.pods?.filter((p) => matches(query, p.name, p.status, p.owner)) ?? [],
      services: ov?.services?.filter((s) => matches(query, s.name, s.type)) ?? [],
      jobs: ov?.jobs?.filter((j) => matches(query, j.name, j.status)) ?? [],
    }
  }, [overview, query])

  const toggle = (key: string) =>
    setClosed((prev) => {
      const next = new Set(prev)
      if (!next.delete(key)) next.add(key)
      return next
    })

  const section = (key: string, title: string, count: number, rows: ReactNode) =>
    count === 0 ? null : (
      <div key={key}>
        <div className="tree-row static kube-section" onClick={() => toggle(key)}>
          <span className="twisty">{closed.has(key) ? '▸' : '▾'}</span>
          <span className="file-name">{title}</span>
          <span className="badge">{count}</span>
        </div>
        {!closed.has(key) && rows}
      </div>
    )

  const menu = (items: DropdownItem[]) => (e: React.MouseEvent) => {
    e.preventDefault()
    openContextMenu(e.clientX, e.clientY, items)
  }

  const workloadMenu = (w: KubeWorkload): DropdownItem[] => [
    { label: 'Logs', hint: resourceRef(w.kind, w.name), onSelect: () => openLogs(resourceRef(w.kind, w.name)) },
    { label: 'Describe', onSelect: () => describe(w.kind, w.name) },
    { label: 'Rollout Restart', onSelect: () => void restartWorkload(w) },
    ...(w.kind === 'DaemonSet'
      ? []
      : ([
          { type: 'separator' },
          { label: `Scale to ${w.desired + 1}`, onSelect: () => void scaleWorkload(w, w.desired + 1) },
          ...(w.desired > 0 ? [{ label: `Scale to ${w.desired - 1}`, onSelect: () => void scaleWorkload(w, w.desired - 1) }] : []),
          ...(w.desired > 1 ? [{ label: 'Scale to 0', danger: true, onSelect: () => void scaleWorkload(w, 0) }] : []),
        ] as DropdownItem[])),
  ]

  const podMenu = (p: KubePod): DropdownItem[] => [
    { label: 'Logs', onSelect: () => openLogs(`pod/${p.name}`) },
    ...(p.containers.length > 1
      ? p.containers.map((c) => ({ label: `Shell: ${c}`, onSelect: () => openShell(p, c) }) as DropdownItem)
      : [{ label: 'Shell', onSelect: () => openShell(p) } as DropdownItem]),
    { label: 'Describe', onSelect: () => describe('Pod', p.name) },
    ...p.ports.map((port) => ({ label: `Port Forward :${port}`, onSelect: () => portForward(`pod/${p.name}`, port) }) as DropdownItem),
    { type: 'separator' },
    { label: 'Delete Pod', danger: true, onSelect: () => void deletePod(p) },
  ]

  const serviceMenu = (s: KubeService): DropdownItem[] => [
    ...s.ports.map((port) => ({ label: `Port Forward :${port.port}`, hint: `→ ${port.target}`, onSelect: () => portForward(`service/${s.name}`, port.port) }) as DropdownItem),
    { label: 'Describe', onSelect: () => describe('Service', s.name) },
  ]

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">Kubernetes</span>
        <button className="icon" title="Terminal in this context and namespace" disabled={!context} onClick={() => void openContextTerminal()}><Icon name="terminal" size={14} /></button>
        <button className="icon" title="Refresh" disabled={loading} onClick={() => (setNow(Date.now()), void loadContexts(), void refreshOverview())}>↻</button>
      </div>
      <div className="search-form kube-pickers">
        <Select
          block
          searchable
          value={context}
          placeholder="Context"
          options={contexts.map((c) => ({ value: c.name, label: shortContext(c.name), hint: [c.current && 'current', c.awsProfile && `aws:${c.awsProfile}`].filter(Boolean).join(' · ') || undefined }))}
          onChange={(name) => void selectContext(name)}
        />
        <div className="kube-row">
          {namespaces.length > 0 ? (
            <Select block searchable label="ns" value={namespace} options={namespaces.map((n) => ({ value: n, label: n }))} onChange={selectNamespace} />
          ) : (
            <input className="kube-ns" value={namespace} spellCheck={false} title="Namespace" onChange={(e) => useKube.setState({ namespace: e.target.value })} onKeyDown={(e) => e.key === 'Enter' && selectNamespace(namespace)} />
          )}
          {ctx && !ctx.current && (
            <button className="icon" title="Make this the kubeconfig's current context (all terminals and tools)" onClick={() => void useAsCurrent()}>★</button>
          )}
        </div>
        {aws && awsStatus && (
          <div className="kube-aws" title={awsStatus.title}>
            <span className="muted">AWS {aws.name}</span>
            <span className={`aws-status ${awsStatus.tone}`}>{awsStatus.text}</span>
            {awsRefreshes[aws.name] === 'running' ? (
              <span className="muted">refreshing…</span>
            ) : awsStatus.tone === 'bad' || awsStatus.tone === 'warn' ? (
              aws.refreshable ? (
                <button className="link" title="Refresh the AWS credentials (no terminal)" onClick={() => void refreshProfile(aws).then(() => refreshOverview())}>Refresh</button>
              ) : aws.login?.length ? (
                <button className="link" onClick={() => loginProfile(aws)}>Log In</button>
              ) : null
            ) : null}
          </div>
        )}
        <div className="kube-row">
          <div className="segmented kube-view" role="radiogroup" aria-label="View">
            {(['apps', 'resources'] as const).map((v) => (
              <button key={v} role="radio" aria-checked={view === v} className={view === v ? 'on' : ''} onClick={() => setKubeView(v)}>
                {v === 'apps' ? 'Apps' : 'Resources'}
              </button>
            ))}
          </div>
          <div className="search-input kube-filter">
            <input value={query} placeholder="Filter" spellCheck={false} onChange={(e) => setKubeQuery(e.target.value)} />
          </div>
        </div>
        {overview && overview.partial?.length > 0 && <div className="muted kube-partial">Not shown (no access): {overview.partial.join(', ')}</div>}
      </div>
      <div className="tree scroll">
        {contextsError && <div className="placeholder error">{contextsError}</div>}
        {error && (
          <div className="placeholder error kube-error">
            {error}
            {aws && isAuthError(error) && (aws.refreshable || aws.login?.length) ? (
              <button className="link" onClick={() => (aws.refreshable ? void refreshProfile(aws).then(() => refreshOverview()) : loginProfile(aws))}>
                Refresh AWS credentials ({aws.name})
              </button>
            ) : null}
          </div>
        )}
        {!overview && !error && context && <div className="placeholder muted">{loading ? 'Loading…' : ''}</div>}
        {overview && view === 'apps' && <KubeApps overview={overview} />}
        {view === 'resources' && section('workloads', 'Workloads', shown.workloads.length, shown.workloads.map((w) => (
          <div key={`${w.kind}/${w.name}`} className="tree-row static kube-item" onContextMenu={menu(workloadMenu(w))} onDoubleClick={() => openLogs(resourceRef(w.kind, w.name))} title={`${w.kind} ${w.name}\n${w.ready}/${w.desired} ready, ${w.updated} updated\nDouble-click: logs`}>
            <span className="kube-kind">{w.kind === 'Deployment' ? 'D' : w.kind === 'StatefulSet' ? 'S' : 'DS'}</span>
            <span className="file-name">{w.name}</span>
            <span className="muted path-hint">{age(w.created, now)}</span>
            <span className={`aws-status ${workloadTone(w)}`}>{busy.has(`${w.kind}/${w.name}`) ? '…' : `${w.ready}/${w.desired}`}</span>
          </div>
        )))}
        {view === 'resources' && section('pods', 'Pods', shown.pods.length, shown.pods.map((p) => (
          <div key={p.name} className="tree-row static kube-item" onContextMenu={menu(podMenu(p))} onDoubleClick={() => openLogs(`pod/${p.name}`)} title={`${p.name}\n${p.status}, ${p.ready}/${p.total} ready, ${p.restarts} restarts${p.node ? `\nnode ${p.node}` : ''}\nDouble-click: logs`}>
            <span className={`kube-dot ${podTone(p)}`}>●</span>
            <span className="file-name">{p.name}</span>
            <span className="muted path-hint">{[p.restarts ? `↺${p.restarts}` : '', age(p.created, now)].filter(Boolean).join(' ')}</span>
            <span className={`aws-status ${podTone(p)}`}>{busy.has(`Pod/${p.name}`) ? '…' : p.status === 'Running' ? `${p.ready}/${p.total}` : p.status}</span>
          </div>
        )))}
        {view === 'resources' && section('services', 'Services', shown.services.length, shown.services.map((s) => (
          <div key={s.name} className="tree-row static kube-item" onContextMenu={menu(serviceMenu(s))} title={`${s.type} ${s.clusterIP}\n${s.ports.map((p) => `${p.port} → ${p.target}/${p.protocol}`).join('\n')}`}>
            <span className="kube-kind">SVC</span>
            <span className="file-name">{s.name}</span>
            <span className="muted path-hint">{s.ports.map((p) => p.port).join(', ')}</span>
            <span className="aws-status muted">{s.type}</span>
          </div>
        )))}
        {view === 'resources' && section('jobs', 'Jobs', shown.jobs.length, shown.jobs.map((j) => (
          <div key={`${j.kind}/${j.name}`} className="tree-row static kube-item" onContextMenu={menu([{ label: 'Describe', onSelect: () => describe(j.kind, j.name) }])} title={j.schedule ? `schedule ${j.schedule}` : j.name}>
            <span className="kube-kind">{j.kind === 'CronJob' ? 'CJ' : 'J'}</span>
            <span className="file-name">{j.name}</span>
            <span className="muted path-hint">{j.schedule ?? age(j.created, now)}</span>
            <span className={`aws-status ${/fail/.test(j.status) ? 'bad' : j.status === 'running' ? 'warn' : 'muted'}`}>{j.status}</span>
          </div>
        )))}
        {overview && !error && view === 'resources' && shown.workloads.length + shown.pods.length + shown.services.length + shown.jobs.length === 0 && (
          <div className="placeholder muted">{query ? 'Nothing matches.' : `Namespace ${namespace} is empty.`}</div>
        )}
      </div>
    </div>
  )
}
