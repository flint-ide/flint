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
import { Dropdown, type DropdownItem } from '../ui/Dropdown'
import { Git } from '../../store/git'
import { RunWidget } from '../run/RunWidget'
import { activeRoot } from '../../store/activeRoot'
import { rootLabels } from '../../lib/workspaceRoots'

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
      {root && <RunWidget />}
      <QuickActions />
    </header>
  )
}

const FULLSCREEN_CHECK_MS = 100

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
    // A live resize fires every frame: ask AppKit once the window settles, not per event.
    let timer: ReturnType<typeof setTimeout> | undefined
    const check = () => {
      clearTimeout(timer)
      timer = setTimeout(() => void WindowIsFullscreen().then(setFullscreen).catch(() => undefined), FULLSCREEN_CHECK_MS)
    }
    window.addEventListener('resize', check)
    const off = EventsOn('wails:window-fullscreen', check)
    return () => {
      clearTimeout(timer)
      window.removeEventListener('resize', check)
      off()
    }
  }, [])

  return { height: base.height, inset: fullscreen ? 12 : base.inset }
}

function ProjectWidget() {
  const root = useWorkspace((s) => s.root)
  const roots = useWorkspace((s) => s.roots)
  const openFolder = useWorkspace((s) => s.openFolder)
  const [recent, setRecent] = useState<string[]>([])
  const name = root?.split('/').pop() ?? 'Open folder'
  const labels = rootLabels(roots)

  const pick = async () => {
    const path = await Workspace.PickFolder()
    if (path) await openFolder(path)
  }

  const items = (): DropdownItem[] => [
    { label: 'Open Folder…', kbd: '⌘O', icon: <FileIcon name="folder" isDir open />, onSelect: () => void pick() },
    { label: 'Add Project to Workspace…', kbd: '⇧⌘O', disabled: !root, onSelect: () => void useWorkspace.getState().pickRoot() },
    { label: 'Recent Projects…', onSelect: () => useWorkspace.getState().setRecentOpen(true) },
    ...(roots.length > 1 ? [{ type: 'section', label: 'Workspace' } as DropdownItem] : []),
    ...(roots.length > 1
      ? roots.map(
          (r, i): DropdownItem => ({
            key: `ws:${r}`,
            label: labels[i],
            hint: i === 0 ? 'main project' : r.replace(/^\/Users\/[^/]+/, '~'),
            icon: <FileIcon name={labels[i]} isDir />,
            onSelect: () => void Workspace.RevealInFinder(r),
            action: i === 0 ? undefined : { label: '✕', title: 'Remove from workspace', onClick: () => void useWorkspace.getState().removeRoot(r) },
          }),
        )
      : []),
    ...(recent.length ? [{ type: 'section', label: 'Recent · ⌥ adds to workspace' } as DropdownItem] : []),
    ...recent.map(
      (p): DropdownItem => ({
        key: p,
        label: p.split('/').pop() ?? p,
        hint: p.replace(/^\/Users\/[^/]+/, '~'),
        icon: <FileIcon name={p.split('/').pop() ?? p} isDir />,
        // ⌘-click / ⌘Enter opens it in a new window, ⌥ attaches it to this workspace.
        onSelect: (e) => void (e.metaKey ? Git.OpenWindow(p) : e.altKey && root ? useWorkspace.getState().addRoot(p) : openFolder(p)),
        action: {
          label: '✕',
          title: 'Remove from recent',
          onClick: () => {
            void Workspace.ForgetRecent(p)
            setRecent((r) => r.filter((x) => x !== p))
          },
        },
      }),
    ),
  ]

  return (
    <Dropdown
      minWidth={320}
      onOpen={() => void Workspace.RecentFolders().then((r) => setRecent((r ?? []).filter((p) => !roots.includes(p))))}
      items={items}
      trigger={(t) => (
        <button {...t} className="tb-widget project" title={roots.length ? roots.join('\n') : 'Open a folder'}>
          <FileIcon name={name} isDir />
          <span className="tb-label">{name}</span>
          {roots.length > 1 && <span className="tb-extra">+{roots.length - 1}</span>}
          <span className="tb-chevron">⌄</span>
        </button>
      )}
    />
  )
}

function BranchWidget() {
  const isRepo = useGit((s) => s.isRepo)
  const status = useGit((s) => s.status)
  const busy = useGit((s) => s.busy)
  // In the workspace store so palette commands (New Branch…, New Worktree…) can open it.
  const popup = useWorkspace((s) => s.branchesPopup)
  const setPopup = useWorkspace((s) => s.setBranchesPopup)
  if (!isRepo || !status) return null
  const name = status.branch || `detached ${status.head.slice(0, 7)}`
  return (
    <>
      <button className="tb-widget branch" onClick={() => setPopup('list')} title="Branches & worktrees">
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
      {popup && (
        <Suspense fallback={null}>
          <BranchesPopup key={popup} initialMode={popup} onClose={() => setPopup(null)} />
        </Suspense>
      )}
    </>
  )
}

function QuickActions() {
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
      <button className="tb-icon" title="Terminal (⌘J) · new terminal ⇧⌘T" onClick={() => toggleTerminal(activeRoot())} onAuxClick={() => useTerminal.getState().create(activeRoot())}>
        <TabKindIcon kind="terminal" />
      </button>
      {agent && (
        <button
          className="tb-icon"
          title={`New ${agent} chat`}
          disabled={!!starting}
          onClick={() => void useAgentChat.getState().start(agent, activeRoot())}
        >
          <TabKindIcon kind="chat" busy={!!starting} />
        </button>
      )}
    </div>
  )
}
