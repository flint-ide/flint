import { Suspense, lazy, useEffect, useState, type CSSProperties } from 'react'
import * as AppAPI from '../../../wailsjs/go/main/AppAPI'
import { EventsOn, WindowIsFullscreen, WindowToggleMaximise } from '../../../wailsjs/runtime/runtime'
import { Workspace } from '../../api/backend'
import { useWorkspace } from '../../store/workspace'
import { useGit } from '../../store/git'
import { useTerminal, toggleTerminal } from '../../store/terminal'
import { useAgentChat } from '../../store/agentChat'
import { useAgent } from '../../store/agent'
import { FileIcon, TabKindIcon } from '../icons/FileIcon'

const BranchesPopup = lazy(() => import('../git/BranchesPopup').then((m) => ({ default: m.BranchesPopup })))

// Fallbacks match AppKit's compact unified title bar (measured: 40pt, buttons at x=12).
const DEFAULT_HEIGHT = 40
const DEFAULT_INSET = 80

/**
 * Title bar laid out around the native macOS window buttons (kept native: the
 * window uses AppKit's compact toolbar style). IntelliJ-like widgets: project,
 * branch, then quick actions on the right. Empty space drags the window.
 */
export function TitleBar() {
  const metrics = useTitlebarMetrics()
  const root = useWorkspace((s) => s.root)

  return (
    <header
      className="titlebar"
      style={{ '--titlebar-height': `${metrics.height}px`, '--traffic-inset': `${metrics.inset}px` } as CSSProperties}
      onDoubleClick={(e) => e.target === e.currentTarget && WindowToggleMaximise()}
    >
      <ProjectWidget />
      {root && <BranchWidget />}
      <span className="titlebar-drag" onDoubleClick={() => WindowToggleMaximise()} />
      <QuickActions />
    </header>
  )
}

/** Height and left inset from AppKit; no inset in full screen (buttons hidden). */
function useTitlebarMetrics(): { height: number; inset: number } {
  const [base, setBase] = useState({ height: DEFAULT_HEIGHT, inset: DEFAULT_INSET })
  const [fullscreen, setFullscreen] = useState(false)

  useEffect(() => {
    AppAPI.Titlebar()
      .then((m) => {
        if (m.height <= 0) return // not macOS
        // Three buttons, 6pt apart, then breathing room before our widgets.
        const inset = Math.round(m.buttonsLeft + 3 * m.buttonWidth + 2 * 6 + 14)
        setBase({ height: Math.round(m.height), inset })
      })
      .catch(() => undefined)
    const check = () => void WindowIsFullscreen().then(setFullscreen).catch(() => undefined)
    window.addEventListener('resize', check)
    const off = EventsOn('wails:window-fullscreen', check)
    return () => {
      window.removeEventListener('resize', check)
      off()
    }
  }, [])

  return { height: base.height, inset: fullscreen ? 12 : base.inset }
}

function ProjectWidget() {
  const root = useWorkspace((s) => s.root)
  const openFolder = useWorkspace((s) => s.openFolder)
  const [open, setOpen] = useState(false)
  const [recent, setRecent] = useState<string[]>([])
  const name = root?.split('/').pop() ?? 'Open folder'

  const toggle = () => {
    if (!open) void Workspace.RecentFolders().then((r) => setRecent((r ?? []).filter((p) => p !== root)))
    setOpen(!open)
  }
  const pick = async () => {
    setOpen(false)
    const path = await Workspace.PickFolder()
    if (path) await openFolder(path)
  }

  return (
    <span className="tb-menu-wrap">
      <button className="tb-widget project" onClick={toggle} title={root ?? 'Open a folder'}>
        <FileIcon name={name} isDir />
        <span className="tb-label">{name}</span>
        <span className="tb-chevron">⌄</span>
      </button>
      {open && (
        <div className="tb-menu" onMouseLeave={() => setOpen(false)}>
          <button onClick={() => void pick()}>
            Open Folder… <kbd>⌘O</kbd>
          </button>
          {recent.length > 0 && <div className="tb-menu-section">Recent</div>}
          {recent.map((p) => (
            <button key={p} onClick={() => (setOpen(false), void openFolder(p))} title={p}>
              <FileIcon name={p.split('/').pop() ?? p} isDir />
              <span>{p.split('/').pop()}</span>
              <span className="muted tb-path">{p.replace(/^\/Users\/[^/]+/, '~')}</span>
            </button>
          ))}
        </div>
      )}
    </span>
  )
}

function BranchWidget() {
  const isRepo = useGit((s) => s.isRepo)
  const status = useGit((s) => s.status)
  const busy = useGit((s) => s.busy)
  const [open, setOpen] = useState(false)
  if (!isRepo || !status) return null
  const name = status.branch || `detached ${status.head.slice(0, 7)}`
  return (
    <>
      <button className="tb-widget branch" onClick={() => setOpen(true)} title="Branches & worktrees">
        <svg className="file-icon" width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <circle cx="5" cy="3.8" r="1.6" fill="none" stroke="currentColor" strokeWidth="1.2" />
          <circle cx="5" cy="12.2" r="1.6" fill="none" stroke="currentColor" strokeWidth="1.2" />
          <circle cx="11" cy="5.5" r="1.6" fill="none" stroke="currentColor" strokeWidth="1.2" />
          <path d="M5 5.4v5.2M11 7.1c0 2.4-6 1.6-6 3.5" fill="none" stroke="currentColor" strokeWidth="1.2" />
        </svg>
        <span className="tb-label">{name}</span>
        {status.ahead > 0 && <span className="ahead">↑{status.ahead}</span>}
        {status.behind > 0 && <span className="behind">↓{status.behind}</span>}
        {status.operation && <span className="warn">{status.operation}</span>}
        {busy && <span className="muted">{busy}…</span>}
        <span className="tb-chevron">⌄</span>
      </button>
      {open && (
        <Suspense fallback={null}>
          <BranchesPopup onClose={() => setOpen(false)} />
        </Suspense>
      )}
    </>
  )
}

function QuickActions() {
  const root = useWorkspace((s) => s.root) ?? ''
  const setQuickOpen = useWorkspace((s) => s.setQuickOpen)
  const preferred = useAgent((s) => s.preferred)
  const agents = useAgentChat((s) => s.agents)
  const starting = useAgentChat((s) => s.starting)
  const agent = agents.includes(preferred) ? preferred : agents[0]

  return (
    <div className="tb-actions">
      <button className="tb-search" onClick={() => setQuickOpen(true)} title="Go to file (⌘P)">
        <svg width="13" height="13" viewBox="0 0 16 16" aria-hidden>
          <circle cx="7" cy="7" r="4.8" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path d="M10.5 10.5L14 14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
        <span>Go to file</span>
        <kbd>⌘P</kbd>
      </button>
      <button className="tb-icon" title="Terminal (⌘J) · new terminal ⇧⌘T" onClick={() => toggleTerminal(root)} onAuxClick={() => useTerminal.getState().create(root)}>
        <TabKindIcon kind="terminal" />
      </button>
      {agent && (
        <button
          className="tb-icon"
          title={`New ${agent} chat`}
          disabled={!!starting}
          onClick={() => void useAgentChat.getState().start(agent, root)}
        >
          <TabKindIcon kind="chat" busy={!!starting} />
        </button>
      )}
    </div>
  )
}
