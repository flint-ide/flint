import { useEffect, useMemo, useState } from 'react'
import * as DockerAPI from '../../../bindings/flint/dockerapi'
import { BrowserOpenURL, EventsOn } from '../../wails/runtime'
import { composeAction, containerAction, openLogs, openShell, refreshDocker, useDocker } from '../../store/docker'
import { openContextMenu } from '../../store/contextMenu'
import { useEditor } from '../../store/editor'
import { dockerTree, rowClass, statusOf, type ComposeProject, type Container } from '../../lib/dockerTree'
import type { DropdownItem } from '../ui/Dropdown'
import { Icon } from '../ui/Icon'

/** Compose up, down and pull are implemented by the docker CLI's Compose plugin; the rest uses the Engine API. */
const NEEDS_CLI = 'Needs the docker CLI with Compose (install Docker Desktop or docker-compose-plugin)'

/** Containers grouped by Compose project; live while shown (docker events), idle when hidden. */
export function DockerPanel() {
  const { containers, projects, loading, error, busy, composeAvailable, warning } = useDocker()
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
  const tree = useMemo(() => dockerTree(containers, projects), [containers, projects])

  useEffect(() => {
    void refreshDocker()
    DockerAPI.Watch(true)
    const off = EventsOn('docker:changed', () => void refreshDocker())
    return () => {
      off()
      DockerAPI.Watch(false)
    }
  }, [])

  const toggle = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (!next.delete(key)) next.add(key)
      return next
    })

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">Docker</span>
        <button className="icon" title="Refresh" disabled={loading} onClick={() => void refreshDocker()}>↻</button>
      </div>
      <div className="tree scroll">
        {error && <div className="placeholder error">{error}</div>}
        {warning && <div className="placeholder error">{warning}</div>}
        {tree.projects.map(({ project, containers: cs, running }) => (
          <div key={project.dir}>
            <div
              className={rowClass('docker-project')}
              title={project.files.join('\n')}
              onClick={() => toggle(project.dir)}
              onContextMenu={(e) => {
                e.preventDefault()
                openContextMenu(e.clientX, e.clientY, projectMenu(project, composeAvailable))
              }}
            >
              <span className="twisty">{collapsed.has(project.dir) ? '▸' : '▾'}</span>
              <span className="docker-icon"><Icon name="ship" size={14} /></span>
              <span className="file-name">{project.name || project.dir.split('/').pop()}</span>
              <span className="muted path-hint">{cs.length ? `${running}/${cs.length} running` : 'not started'}</span>
              <span className="docker-actions">
                <button className="icon" title={composeAvailable ? 'Up (docker compose up -d)' : NEEDS_CLI} disabled={busy.has(project.dir) || !composeAvailable} onClick={(e) => (e.stopPropagation(), void composeAction(project, 'up'))}>▶</button>
                <button className="icon" title={composeAvailable ? 'Down' : NEEDS_CLI} disabled={busy.has(project.dir) || cs.length === 0 || !composeAvailable} onClick={(e) => (e.stopPropagation(), void composeAction(project, 'down'))}>■</button>
              </span>
            </div>
            {!collapsed.has(project.dir) && cs.map((c) => <ContainerRow key={c.id} c={c} busy={busy.has(c.id)} indent label={c.service} />)}
          </div>
        ))}
        {tree.others.length > 0 && (
          <>
            <div className={rowClass('docker-project')} onClick={() => toggle('')}>
              <span className="twisty">{collapsed.has('') ? '▸' : '▾'}</span>
              <span className="file-name">Other containers</span>
            </div>
            {!collapsed.has('') && tree.others.map((c) => <ContainerRow key={c.id} c={c} busy={busy.has(c.id)} indent label={c.name} />)}
          </>
        )}
        {!error && !composeAvailable && tree.projects.length > 0 && <div className="placeholder muted">{NEEDS_CLI}</div>}
        {!error && !loading && containers.length === 0 && projects.length === 0 && <div className="placeholder muted">No containers or compose files.</div>}
      </div>
    </div>
  )
}

function ContainerRow({ c, busy, indent, label }: { c: Container; busy: boolean; indent: boolean; label: string }) {
  const status = statusOf(c)
  const running = c.state === 'running'
  return (
    <div
      className={rowClass('docker-container', indent ? 'indent' : '')}
      title={`${c.name}\n${c.image}\n${c.status}`}
      onDoubleClick={() => (running ? openLogs(c) : undefined)}
      onContextMenu={(e) => {
        e.preventDefault()
        openContextMenu(e.clientX, e.clientY, containerMenu(c))
      }}
    >
      <span className={`docker-dot ${status}`} />
      <span className="file-name">{label || c.name}</span>
      {c.ports.map((p) => (
        <button key={p.hostPort} className="link docker-port" title={`http://localhost:${p.hostPort} → ${p.container}`} onClick={() => BrowserOpenURL(`http://localhost:${p.hostPort}`)}>
          :{p.hostPort}
        </button>
      ))}
      <span className="muted path-hint">{c.status}</span>
      <span className="docker-actions">
        <button className="icon" title="Logs" onClick={() => openLogs(c)}>≡</button>
        {running && <button className="icon" title="Shell" onClick={() => openShell(c)}><Icon name="terminal" size={14} /></button>}
        <button className="icon" title={running ? 'Restart' : 'Start'} disabled={busy} onClick={() => void containerAction(c, running ? 'restart' : 'start')}>
          {running ? '↻' : '▶'}
        </button>
      </span>
    </div>
  )
}

function containerMenu(c: Container): DropdownItem[] {
  const running = c.state === 'running'
  return [
    { label: 'Logs', onSelect: () => openLogs(c) },
    { label: 'Open Shell', disabled: !running, onSelect: () => openShell(c) },
    { type: 'separator' },
    running
      ? { label: 'Stop', onSelect: () => void containerAction(c, 'stop') }
      : { label: 'Start', onSelect: () => void containerAction(c, 'start') },
    { label: 'Restart', disabled: !running, onSelect: () => void containerAction(c, 'restart') },
    { label: 'Remove…', danger: true, disabled: running, onSelect: () => void containerAction(c, 'rm') },
    { type: 'separator' },
    ...c.ports.map((p) => ({ label: `Open localhost:${p.hostPort}`, onSelect: () => BrowserOpenURL(`http://localhost:${p.hostPort}`) })),
    { label: 'Copy Container ID', onSelect: () => void navigator.clipboard.writeText(c.id).catch(() => undefined) },
  ]
}

function projectMenu(p: ComposeProject, composeAvailable: boolean): DropdownItem[] {
  return [
    { label: 'Up', hint: composeAvailable ? 'compose up -d' : NEEDS_CLI, disabled: !composeAvailable, onSelect: () => void composeAction(p, 'up') },
    { label: 'Restart', onSelect: () => void composeAction(p, 'restart') },
    { label: 'Stop', onSelect: () => void composeAction(p, 'stop') },
    { label: 'Down…', danger: true, hint: composeAvailable ? undefined : NEEDS_CLI, disabled: !composeAvailable, onSelect: () => void composeAction(p, 'down') },
    { label: 'Pull Images', hint: composeAvailable ? undefined : NEEDS_CLI, disabled: !composeAvailable, onSelect: () => void composeAction(p, 'pull') },
    { type: 'separator' },
    ...p.files.map((f) => ({ label: `Open ${f.split('/').pop()}`, onSelect: () => useEditor.getState().openFile(f) })),
  ]
}
