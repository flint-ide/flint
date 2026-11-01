import { Suspense, lazy, useEffect, useRef } from 'react'
import { EventsOn } from './wails/runtime'
import { useWorkspace } from './store/workspace'
import { useEditor } from './store/editor'
import { useDatabase } from './store/database'
import { useScratch } from './store/scratch'
import { subscribeTerminalEvents, toggleTerminal, useTerminal } from './store/terminal'
import { subscribeSearchEvents } from './store/search'
import { subscribeFsEvents } from './store/fsEvents'
import { subscribeQuit } from './store/quit'
import { subscribeSessionAutosave } from './store/session'
import { subscribeGitEvents } from './store/git'
import { subscribeLspEvents } from './store/lsp'
import { subscribeAgentChatEvents } from './store/agentChat'
import { subscribeRunEvents, useRun } from './store/run'
import { subscribeDebugEvents } from './store/debug'
import { subscribeAutoSave } from './store/settings'
import { subscribeEditorContext } from './store/editorContext'
import { activeRoot } from './store/activeRoot'
import { openDropped } from './store/drop'
import { refreshTrust, useTrust } from './store/trust'
import { TrustBanner } from './components/layout/TrustBanner'
import { copyDiagnostics, reportIssue, showLogs } from './store/diagnostics'
import { subscribeAlerts } from './store/alerts'
import { openToolCheck, openToolCheckOnFirstRun, useToolCheck } from './store/toolCheck'
import { checkForUpdates, subscribeUpdates } from './store/update'
import type { DroppedPath } from './lib/drop'
import { UsagesModal } from './components/lsp/UsagesModal'
import { showToast } from './store/dialog'
import { errorMessage } from './api/backend'
import { handleGlobalKey, setCommandErrorHandler } from './lib/commands'
import { registerAppCommands } from './commands/appCommands'
import { ActivityBar } from './components/layout/ActivityBar'
import { Splitter } from './components/layout/Splitter'
import { Overlays } from './components/layout/Overlays'
import { ErrorBoundary } from './components/layout/ErrorBoundary'
import { StatusBar } from './components/layout/StatusBar'
import { TitleBar } from './components/layout/TitleBar'
import { EditorArea } from './components/editor/EditorArea'
import { PANELS } from './panels'
import { resolvePanel } from './panels/registry'
import { usePersistentNumber } from './lib/usePersistentNumber'

const ACTIVITY_BAR = 44

const RecentProjects = lazy(() => import('./components/RecentProjects').then((m) => ({ default: m.RecentProjects })))
const QuickOpen = lazy(() => import('./components/QuickOpen').then((m) => ({ default: m.QuickOpen })))
const RunConfigDialog = lazy(() => import('./components/run/RunConfigDialog').then((m) => ({ default: m.RunConfigDialog })))
const CommandPalette = lazy(() => import('./components/CommandPalette').then((m) => ({ default: m.CommandPalette })))
const ToolCheck = lazy(() => import('./components/layout/ToolCheck').then((m) => ({ default: m.ToolCheck })))

export default function App() {
  const sidebar = useWorkspace((s) => s.sidebar)
  // An unknown id (a panel that was removed or renamed) shows the Files panel.
  const panel = resolvePanel(PANELS, sidebar)
  const focusToken = useWorkspace((s) => (panel ? (s.panelFocus[panel.id] ?? 0) : 0))
  const sidebarVisible = useWorkspace((s) => s.sidebarVisible)
  const quickOpen = useWorkspace((s) => s.quickOpen)
  const recentOpen = useWorkspace((s) => s.recentOpen)
  const paletteOpen = useWorkspace((s) => s.paletteOpen)
  const toolCheckOpen = useToolCheck((s) => s.open)
  const root = useWorkspace((s) => s.root)
  const error = useWorkspace((s) => s.error)
  const editingRun = useRun((s) => s.editing)
  const ready = useWorkspace((s) => s.ready)
  const [sidebarWidth, setSidebarWidth] = usePersistentNumber('flint.sidebarWidth', 280)
  const workbench = useRef<HTMLDivElement>(null)

  useEffect(() => {
    subscribeSearchEvents()
    subscribeTerminalEvents()
    subscribeFsEvents()
    subscribeQuit()
    subscribeGitEvents()
    subscribeLspEvents()
    subscribeAgentChatEvents()
    subscribeRunEvents()
    subscribeDebugEvents()
    subscribeSessionAutosave()
    subscribeAutoSave()
    subscribeAlerts()
    subscribeUpdates()
    subscribeEditorContext()
    // Native File menu: Open Folder… / Open Recent ▸ <folder>.
    EventsOn('menu:open-recent', () => useWorkspace.getState().setRecentOpen(true))
    EventsOn('menu:open-folder', (path: string) => void useWorkspace.getState().openFolder(path))
    EventsOn('menu:add-project', () => void useWorkspace.getState().pickRoot())
    EventsOn('menu:settings', () => useEditor.getState().openSettings()) // app menu ▸ Settings… (⌘,)
    EventsOn('menu:report-issue', () => void reportIssue())
    EventsOn('menu:copy-diagnostics', () => void copyDiagnostics())
    EventsOn('menu:show-logs', showLogs)
    EventsOn('menu:check-tools', openToolCheck)
    EventsOn('menu:check-updates', () => void checkForUpdates())
    EventsOn('files:dropped', (dropped: DroppedPath[]) => void openDropped(dropped))
    void useDatabase.getState().loadConnections()
    void useScratch.getState().load()
    void useWorkspace.getState().init()
    void openToolCheckOnFirstRun()
  }, [])

  useEffect(() => {
    if (!error) return
    showToast(error, 'error')
    useWorkspace.getState().clearError()
  }, [error])

  const roots = useWorkspace((s) => s.roots)
  useEffect(() => void refreshTrust(roots), [roots])
  const rootTrusted = useTrust((s) => (root ? s.trusted[root] : undefined))

  // AI-first: a shell in the project is the first tab once the folder is known —
  // only in a trusted folder: a shell prompt runs git and version-manager hooks
  // that a hostile repository could abuse (Restricted Mode).
  useEffect(() => {
    if (ready && rootTrusted === true && !useEditor.getState().tabs.some((t) => t.kind === 'terminal')) {
      useTerminal.getState().create(root ?? '')
    }
  }, [ready, root, rootTrusted])

  useGlobalShortcuts()

  const onSidebarResize = (ratio: number) => {
    const width = workbench.current?.getBoundingClientRect().width ?? 1200
    setSidebarWidth(Math.min(600, Math.max(180, ratio * width - ACTIVITY_BAR)))
  }

  return (
    <div className="app">
      <TitleBar />
      <div className="workbench" ref={workbench}>
        <ActivityBar onTerminal={() => toggleTerminal(activeRoot())} />
        {sidebarVisible && (
          <>
            <aside className="sidebar" style={{ width: sidebarWidth }}>
              <ErrorBoundary label="Sidebar">
              {panel && (
                // key: switching panels unmounts the previous one, as separate elements did.
                <Suspense key={panel.id} fallback={null}>
                  <panel.component focusToken={focusToken} />
                </Suspense>
              )}
              </ErrorBoundary>
            </aside>
            <Splitter direction="horizontal" onResize={onSidebarResize} />
          </>
        )}
        {/* Files, terminals and agent chats share one tab area. */}
        <main className="main">
          <TrustBanner />
          <EditorArea />
        </main>
      </div>
      <StatusBar />
      {quickOpen && (
        <Suspense fallback={null}>
          <QuickOpen />
        </Suspense>
      )}
      {recentOpen && (
        <Suspense fallback={null}>
          <RecentProjects />
        </Suspense>
      )}
      {editingRun && (
        <Suspense fallback={null}>
          <RunConfigDialog key={editingRun.replaces ?? editingRun.config.id} editing={editingRun} />
        </Suspense>
      )}
      {paletteOpen && (
        <Suspense fallback={null}>
          <CommandPalette />
        </Suspense>
      )}
      {toolCheckOpen && (
        <Suspense fallback={null}>
          <ToolCheck />
        </Suspense>
      )}
      <UsagesModal />
      <Overlays />
    </div>
  )
}

/**
 * Registers the app's commands and runs their shortcuts from one capturing keydown
 * listener, so they fire before editors and terminals see the keys (see lib/commands.ts).
 */
function useGlobalShortcuts() {
  useEffect(() => {
    setCommandErrorHandler((err) => showToast(errorMessage(err), 'error'))
    const unregister = registerAppCommands()
    const onKey = (e: KeyboardEvent) => void handleGlobalKey(e)
    window.addEventListener('keydown', onKey, true)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      unregister()
    }
  }, [])
}
