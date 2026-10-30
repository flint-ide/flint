import { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react'
import { EventsOn } from '../wailsjs/runtime/runtime'
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
import { FileTree } from './components/sidebar/FileTree'
import { SearchPanel } from './components/sidebar/SearchPanel'
import { ScratchPanel } from './components/sidebar/ScratchPanel'
import { usePersistentNumber } from './lib/usePersistentNumber'

const ACTIVITY_BAR = 44

const DatabasePanel = lazy(() => import('./components/sidebar/DatabasePanel').then((m) => ({ default: m.DatabasePanel })))
const CommitPanel = lazy(() => import('./components/sidebar/CommitPanel').then((m) => ({ default: m.CommitPanel })))
const RecentProjects = lazy(() => import('./components/RecentProjects').then((m) => ({ default: m.RecentProjects })))
const QuickOpen = lazy(() => import('./components/QuickOpen').then((m) => ({ default: m.QuickOpen })))
const RunConfigDialog = lazy(() => import('./components/run/RunConfigDialog').then((m) => ({ default: m.RunConfigDialog })))
const CommandPalette = lazy(() => import('./components/CommandPalette').then((m) => ({ default: m.CommandPalette })))

export default function App() {
  const sidebar = useWorkspace((s) => s.sidebar)
  const sidebarVisible = useWorkspace((s) => s.sidebarVisible)
  const quickOpen = useWorkspace((s) => s.quickOpen)
  const recentOpen = useWorkspace((s) => s.recentOpen)
  const paletteOpen = useWorkspace((s) => s.paletteOpen)
  const root = useWorkspace((s) => s.root)
  const error = useWorkspace((s) => s.error)
  const editingRun = useRun((s) => s.editing)
  const ready = useWorkspace((s) => s.ready)
  const [sidebarWidth, setSidebarWidth] = usePersistentNumber('flint.sidebarWidth', 280)
  const [searchFocus, setSearchFocus] = useState(0)
  const [commitFocus, setCommitFocus] = useState(0)
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
    // Native File menu: Open Folder… / Open Recent ▸ <folder>.
    EventsOn('menu:open-recent', () => useWorkspace.getState().setRecentOpen(true))
    EventsOn('menu:open-folder', (path: string) => void useWorkspace.getState().openFolder(path))
    void useDatabase.getState().loadConnections()
    void useScratch.getState().load()
    void useWorkspace.getState().init()
  }, [])

  useEffect(() => {
    if (!error) return
    showToast(error, 'error')
    useWorkspace.getState().clearError()
  }, [error])

  // AI-first: a shell in the project is the first tab once the folder is known.
  useEffect(() => {
    if (ready && !useEditor.getState().tabs.some((t) => t.kind === 'terminal')) {
      useTerminal.getState().create(root ?? '')
    }
  }, [ready, root])

  // Stable, so the commands are registered once rather than on every render.
  const focusSearch = useCallback(() => setSearchFocus((n) => n + 1), [])
  const focusCommit = useCallback(() => setCommitFocus((n) => n + 1), [])
  useGlobalShortcuts(focusSearch, focusCommit)

  const onSidebarResize = (ratio: number) => {
    const width = workbench.current?.getBoundingClientRect().width ?? 1200
    setSidebarWidth(Math.min(600, Math.max(180, ratio * width - ACTIVITY_BAR)))
  }

  return (
    <div className="app">
      <TitleBar />
      <div className="workbench" ref={workbench}>
        <ActivityBar onTerminal={() => toggleTerminal(root ?? '')} />
        {sidebarVisible && (
          <>
            <aside className="sidebar" style={{ width: sidebarWidth }}>
              <ErrorBoundary label="Sidebar">
              {sidebar === 'files' && <FileTree />}
              {sidebar === 'search' && <SearchPanel focusToken={searchFocus} />}
              {sidebar === 'git' && (
                <Suspense fallback={null}>
                  <CommitPanel focusToken={commitFocus} />
                </Suspense>
              )}
              {sidebar === 'database' && (
                <Suspense fallback={null}>
                  <DatabasePanel />
                </Suspense>
              )}
              {sidebar === 'scratch' && <ScratchPanel />}
              </ErrorBoundary>
            </aside>
            <Splitter direction="horizontal" onResize={onSidebarResize} />
          </>
        )}
        {/* Files, terminals and agent chats share one tab area. */}
        <main className="main">
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
      <UsagesModal />
      <Overlays />
    </div>
  )
}

/**
 * Registers the app's commands and runs their shortcuts from one capturing keydown
 * listener, so they fire before editors and terminals see the keys (see lib/commands.ts).
 */
function useGlobalShortcuts(focusSearch: () => void, focusCommit: () => void) {
  useEffect(() => {
    setCommandErrorHandler((err) => showToast(errorMessage(err), 'error'))
    const unregister = registerAppCommands({ focusSearch, focusCommit })
    const onKey = (e: KeyboardEvent) => void handleGlobalKey(e)
    window.addEventListener('keydown', onKey, true)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      unregister()
    }
  }, [focusSearch, focusCommit])
}
