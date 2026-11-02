import { Suspense, memo, useLayoutEffect } from 'react'
import { lazyPreload } from '../../lib/lazyPreload'
import { useEditor, type ContributedTab, type FileTab, type Tab } from '../../store/editor'
import { useContributions } from '../../lib/contributions'
import { Icon } from '../ui/Icon'
import { usePreview } from '../../store/preview'
import { isImage } from '../../lib/previewFiles'
import { confirmAction } from '../../store/dialog'
import { useTerminal } from '../../store/terminal'
import { useAgentChat } from '../../store/agentChat'
import { useScratch } from '../../store/scratch'
import { isFeatureActive } from '../../lib/activeFeatures'
import { closeRunSession, useRun } from '../../store/run'
import { isDebugging, stopDebug, useDebug } from '../../store/debug'
import { closeProfile } from '../../store/profile'
import { useHttpClient } from '../../store/httpClient'
import { dropDetails } from '../../store/pullRequests'
import { useWorkspace } from '../../store/workspace'
import { activeRoot } from '../../store/activeRoot'
import { labelOf, rootLabels } from '../../lib/workspaceRoots'
import { Welcome } from '../layout/Welcome'
import { ErrorBoundary } from '../layout/ErrorBoundary'
import { FileIcon, TabKindIcon } from '../icons/FileIcon'
import { Dropdown, type DropdownItem } from '../ui/Dropdown'
import { formatKeys } from '../../lib/commands'

// CodeMirror, the SQL console and the agent chat load on first use, keeping startup
// to the layout plus the terminal.
const FileView = lazyPreload(() => import('./FileView').then((m) => ({ default: m.FileView })), { early: true })
const ImageView = lazyPreload(() => import('../preview/ImageView').then((m) => ({ default: m.ImageView })))
const ScratchView = lazyPreload(() => import('./ScratchView').then((m) => ({ default: m.ScratchView })))
const DiffView = lazyPreload(() => import('./DiffView').then((m) => ({ default: m.DiffView })))
const MergeView = lazyPreload(() => import('./MergeView').then((m) => ({ default: m.MergeView })))
const HistoryView = lazyPreload(() => import('./HistoryView').then((m) => ({ default: m.HistoryView })))
const AgentChat = lazyPreload(() => import('../agent/AgentChat').then((m) => ({ default: m.AgentChat })))
const RunView = lazyPreload(() => import('../run/RunView').then((m) => ({ default: m.RunView })))
const DebugView = lazyPreload(() => import('../debug/DebugView').then((m) => ({ default: m.DebugView })))
const ProfileView = lazyPreload(() => import('../profile/ProfileView').then((m) => ({ default: m.ProfileView })))
const SettingsView = lazyPreload(() => import('../settings/SettingsView').then((m) => ({ default: m.SettingsView })))
const HttpResponseView = lazyPreload(() => import('../http/HttpResponseView').then((m) => ({ default: m.HttpResponseView })))
const PullRequestView = lazyPreload(() => import('../github/PullRequestView').then((m) => ({ default: m.PullRequestView })))
const PullRequestDiffView = lazyPreload(() => import('../github/PullRequestDiffView').then((m) => ({ default: m.PullRequestDiffView })))
const CILogView = lazyPreload(() => import('../github/CILogView').then((m) => ({ default: m.CILogView })))
// xterm.js is the biggest dependency; output that arrives first waits in store/terminal.
const TerminalView = lazyPreload(() => import('../terminal/TerminalView').then((m) => ({ default: m.TerminalView })))

/** Closes a tab, asking first when work could be lost. */
export async function requestCloseTab(tab: Tab): Promise<void> {
  switch (tab.kind) {
    case 'file':
    case 'merge':
      // Scratchpads autosave, so only file and merge tabs can lose unsaved edits.
      if (tab.dirty && !(await confirmAction(`Discard unsaved changes in ${tab.title}?`, 'Discard'))) return
      break
    case 'terminal': {
      const term = useTerminal.getState().tabs.find((t) => t.id === tab.termId)
      if (term && term.exitCode === null && !(await confirmAction(`Close ${tab.title}? Programs running in it (e.g. an agent) are stopped.`, 'Close'))) {
        return
      }
      useTerminal.getState().close(tab.termId) // also removes the editor tab
      return
    }
    case 'chat':
      if (useAgentChat.getState().sessions[tab.sessionId]?.chat.busy && !(await confirmAction('The agent is still working. Stop it and close the chat?', 'Stop'))) {
        return
      }
      void useAgentChat.getState().stop(tab.sessionId) // also removes the editor tab
      return
    case 'debug':
      if (isDebugging() && !(await confirmAction('The debugged program is still running. Stop it and close the tab?', 'Stop'))) return
      void stopDebug()
      useEditor.getState().closeTab(tab.id)
      return
    case 'run':
      if (useRun.getState().sessions[tab.runKey]?.running && !(await confirmAction(`${tab.title} is still running. Stop it and close the tab?`, 'Stop'))) {
        return
      }
      void closeRunSession(tab.runKey) // also removes the editor tab
      return
    case 'profile':
      closeProfile(tab.profileId) // also removes the editor tab and the temporary files
      return
    case 'pr':
    case 'prdiff':
      useEditor.getState().closeTab(tab.id)
      dropDetails(tab.root, tab.number) // kept while another tab of the pull request is open
      return
  }
  useEditor.getState().closeTab(tab.id)
}

/** Ids of the terminals whose program exited, one per line. */
function exitedTerminals(tabs: readonly { id: string; exitCode?: number | null }[]): string {
  return tabs.filter((t) => t.exitCode != null).map((t) => t.id).join('\n')
}

export function EditorArea() {
  const tabs = useEditor((s) => s.tabs)
  const activeId = useEditor((s) => s.activeId)
  const setActive = useEditor((s) => s.setActive)
  // Only which terminals exited (a string, so retitles and cwd changes don't re-render the tabs).
  const exited = useTerminal((s) => exitedTerminals(s.tabs))
  // Busy flags and chat transcripts are selected per tab below: streamed agent tokens
  // and run output must not re-render the whole editor area.
  const debugActive = useDebug((s) => s.session?.status === 'running' || s.session?.status === 'starting')

  // WebKit (the macOS webview) keeps focus on an element that becomes hidden, so a
  // hidden editor would keep receiving keystrokes typed "into the terminal". Drop
  // focus from inactive tabs on every switch; the active tab focuses itself.
  useLayoutEffect(() => {
    const focused = document.activeElement
    if (focused instanceof HTMLElement) {
      const panel = focused.closest<HTMLElement>('.tab-panel')
      if (panel && panel.dataset.tabId !== activeId) focused.blur()
    }
  }, [activeId])

  if (tabs.length === 0) return <Welcome />

  return (
    <div className="editor-area">
      <div className="tab-strip">
      <div className="tab-bar" role="tablist">
        {tabs.map((tab) => (
          <div
            key={tab.id}
            role="tab"
            aria-selected={tab.id === activeId}
            className={`tab${tab.id === activeId ? ' active' : ''}${tab.kind === 'terminal' && exited.split('\n').includes(tab.termId) ? ' exited' : ''}`}
            title={tab.kind === 'file' ? tab.path : tab.title}
            onClick={() => setActive(tab.id)}
            onMouseDown={(e) => e.button === 1 && void requestCloseTab(tab)}
          >
            <TabIcon tab={tab} debugActive={debugActive} />
            <span className="tab-title">{tab.title}</span>
            <button
              className={`tab-close${tab.dirty ? ' dirty' : ''}`}
              onClick={(e) => {
                e.stopPropagation()
                void requestCloseTab(tab)
              }}
              aria-label="Close tab"
            />
          </div>
        ))}
      </div>
      <NewTabMenu />
      </div>
      <div className="tab-panels">
        {/* All tabs stay mounted so shells, agents, undo history and scroll survive tab switches. */}
        {tabs.map((tab) => <TabPanel key={tab.id} tab={tab} active={tab.id === activeId} />)}
      </div>
    </div>
  )
}

// memo: a tab's body re-renders only when its tab or visibility changes (or the
// store slice it selects itself), not when the tab bar does.
const TabPanel = memo(function TabPanel({ tab, active }: { tab: Tab; active: boolean }) {
  return (
    <div className="tab-panel" data-tab-id={tab.id} hidden={!active}>
      <ErrorBoundary label={tab.title}>
        <Suspense fallback={null}>
          {tab.kind === 'file' && <FileOrImage tab={tab} active={active} />}
          {tab.kind === 'scratch' && <ScratchView tab={tab} active={active} />}
          {tab.kind === 'diff' && <DiffView tab={tab} />}
          {tab.kind === 'merge' && <MergeView tab={tab} active={active} />}
          {tab.kind === 'history' && <HistoryView tab={tab} active={active} />}
          {tab.kind === 'terminal' && <TerminalTabView termId={tab.termId} active={active} />}
          {tab.kind === 'chat' && <ChatTabView sessionId={tab.sessionId} active={active} />}
          {tab.kind === 'run' && <RunView runKey={tab.runKey} active={active} />}
          {tab.kind === 'debug' && <DebugView />}
          {tab.kind === 'profile' && <ProfileView profileId={tab.profileId} />}
          {tab.kind === 'http' && <HttpResponseView tab={tab} active={active} />}
          {tab.kind === 'settings' && <SettingsView active={active} />}
          {tab.kind === 'pr' && <PullRequestView tab={tab} active={active} />}
          {tab.kind === 'prdiff' && <PullRequestDiffView tab={tab} active={active} />}
          {tab.kind === 'cilog' && <CILogView tab={tab} />}
          {tab.kind === 'contrib' && <ContributedTabView tab={tab} active={active} />}
        </Suspense>
      </ErrorBoundary>
    </div>
  )
})

/** Images open in the image view (an SVG can switch to its source); everything else in the editor. */
function FileOrImage({ tab, active }: { tab: FileTab; active: boolean }) {
  const source = usePreview((s) => !!s.svgSource[tab.path])
  if (isImage(tab.path) && !source) return <ImageView path={tab.path} active={active} />
  return <FileView tab={tab} active={active} />
}

function TerminalTabView({ termId, active }: { termId: string; active: boolean }) {
  const tab = useTerminal((s) => s.tabs.find((t) => t.id === termId))
  return tab ? <TerminalView tab={tab} active={active} /> : null
}

/** A tab of a kind a feature contributed; its view may be gone (an extension that stopped). */
function ContributedTabView({ tab, active }: { tab: ContributedTab; active: boolean }) {
  const kind = useContributions((s) => s.tabKinds.find((k) => k.id === tab.view))
  if (!kind) return <p className="muted empty">{tab.title} is not available.</p>
  return <kind.component tab={tab} active={active} />
}

function ContributedTabIcon({ view }: { view: string }) {
  const icon = useContributions((s) => s.tabKinds.find((k) => k.id === view)?.icon)
  return icon ? <Icon name={icon} /> : null
}

function ChatTabView({ sessionId, active }: { sessionId: string; active: boolean }) {
  const session = useAgentChat((s) => s.sessions[sessionId])
  return session ? <AgentChat session={session} active={active} /> : null
}

function TabIcon({ tab, debugActive }: { tab: Tab; debugActive: boolean }) {
  switch (tab.kind) {
    case 'file':
      return <FileIcon name={tab.title} />
    case 'scratch':
      return <TabKindIcon kind={tab.scratchKind} />
    case 'chat':
      return <ChatTabIcon sessionId={tab.sessionId} />
    case 'run':
      return <RunTabIcon runKey={tab.runKey} />
    case 'debug':
      return <TabKindIcon kind="debug" busy={debugActive} />
    case 'http':
      return <HttpTabIcon path={tab.path} />
    case 'pr':
    case 'prdiff':
    case 'merge':
      return <TabKindIcon kind={tab.kind === 'pr' ? 'pr' : 'diff'} />
    case 'cilog':
      return <TabKindIcon kind="run" />
    case 'contrib':
      return <ContributedTabIcon view={tab.view} />
    default:
      return <TabKindIcon kind={tab.kind} busy={false} />
  }
}

function ChatTabIcon({ sessionId }: { sessionId: string }) {
  const busy = useAgentChat((s) => !!s.sessions[sessionId]?.chat.busy)
  return <TabKindIcon kind="chat" busy={busy} />
}

function RunTabIcon({ runKey }: { runKey: string }) {
  const running = useRun((s) => !!s.sessions[runKey]?.running)
  return <TabKindIcon kind="run" busy={running} />
}

function HttpTabIcon({ path }: { path: string }) {
  const sending = useHttpClient((s) => !!s.files[path]?.pending)
  return <TabKindIcon kind="http" busy={sending} />
}

/** "+" at the end of the tab bar: new terminal, agent chat or SQL console. */
function NewTabMenu() {
  const roots = useWorkspace((s) => s.roots)
  const agents = useAgentChat((s) => s.agents)
  const starting = useAgentChat((s) => s.starting)
  // One folder: terminal and chats open there. Several: the active tab's folder, plus a section per folder.
  const forRoot = (root: string): DropdownItem[] => [
    { label: 'Terminal', key: `t:${root}`, icon: <TabKindIcon kind="terminal" />, onSelect: () => void useTerminal.getState().create(root) },
    ...agents.map(
      (a): DropdownItem => ({
        key: `c:${a}:${root}`,
        label: `${a} chat`,
        icon: <TabKindIcon kind="chat" />,
        disabled: !!starting,
        onSelect: () => void useAgentChat.getState().start(a, root),
      }),
    ),
  ]
  const items = (): DropdownItem[] => {
    const here = activeRoot()
    const head = forRoot(here).map((it, i) => (i === 0 && 'onSelect' in it ? { ...it, kbd: formatKeys('Mod+Shift+T') } : it))
    if (roots.length < 2) return [...head, ...tail]
    const labels = rootLabels(roots)
    return [
      { type: 'section', label: `In ${labelOf(roots, here)}` },
      ...head,
      ...roots.flatMap((r, i): DropdownItem[] => (r === here ? [] : [{ type: 'section', label: `In ${labels[i]}` }, ...forRoot(r)])),
      ...tail,
    ]
  }
  const tail: DropdownItem[] = [
    { type: 'separator' },
    ...(isFeatureActive('database')
      ? [{ label: 'SQL console', kbd: formatKeys('Mod+N'), icon: <TabKindIcon kind="sql" />, onSelect: () => void useScratch.getState().create('sql') }]
      : []),
    { label: 'Markdown note', icon: <TabKindIcon kind="markdown" />, onSelect: () => void useScratch.getState().create('markdown') },
  ]
  return (
    <span className="new-tab-wrap">
      <Dropdown
        align="end"
        items={items}
        trigger={(t) => (
          <button {...t} className="icon new-tab" title={`New terminal (${formatKeys('Mod+Shift+T')}), agent chat or SQL console`}>＋</button>
        )}
      />
      {starting && <span className="muted starting">starting {starting}…</span>}
    </span>
  )
}
