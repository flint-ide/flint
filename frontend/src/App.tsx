import { Suspense, lazy, useEffect, useRef, useState } from 'react'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { useWorkspace } from './store/workspace'
import { useEditor } from './store/editor'
import { useDatabase } from './store/database'
import { useScratch } from './store/scratch'
import { subscribeTerminalEvents, toggleTerminal, useTerminal } from './store/terminal'
import { subscribeSearchEvents } from './store/search'
import { subscribeFsEvents } from './store/fsEvents'
import { subscribeQuit } from './store/quit'
import { subscribeGitEvents, useGit } from './store/git'
import { subscribeLspEvents } from './store/lsp'
import { subscribeAgentChatEvents } from './store/agentChat'
import { attachSelectionToAgent } from './lib/agentContext'
import { UsagesModal } from './components/lsp/UsagesModal'
import { showToast } from './store/dialog'
import { ActivityBar } from './components/layout/ActivityBar'
import { Splitter } from './components/layout/Splitter'
import { Overlays } from './components/layout/Overlays'
import { ErrorBoundary } from './components/layout/ErrorBoundary'
import { StatusBar } from './components/layout/StatusBar'
import { TitleBar } from './components/layout/TitleBar'
import { EditorArea, requestCloseTab } from './components/editor/EditorArea'
import { FileTree } from './components/sidebar/FileTree'
import { SearchPanel } from './components/sidebar/SearchPanel'
import { ScratchPanel } from './components/sidebar/ScratchPanel'
import { usePersistentNumber } from './lib/usePersistentNumber'

const ACTIVITY_BAR = 44

const DatabasePanel = lazy(() => import('./components/sidebar/DatabasePanel').then((m) => ({ default: m.DatabasePanel })))
const CommitPanel = lazy(() => import('./components/sidebar/CommitPanel').then((m) => ({ default: m.CommitPanel })))
const RecentProjects = lazy(() => import('./components/RecentProjects').then((m) => ({ default: m.RecentProjects })))
const QuickOpen = lazy(() => import('./components/QuickOpen').then((m) => ({ default: m.QuickOpen })))

export default function App() {
  const sidebar = useWorkspace((s) => s.sidebar)
  const sidebarVisible = useWorkspace((s) => s.sidebarVisible)
  const quickOpen = useWorkspace((s) => s.quickOpen)
  const recentOpen = useWorkspace((s) => s.recentOpen)
  const root = useWorkspace((s) => s.root)
  const error = useWorkspace((s) => s.error)
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

  useGlobalShortcuts(
    () => setSearchFocus((n) => n + 1),
    () => setCommitFocus((n) => n + 1),
  )

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
      <UsagesModal />
      <Overlays />
    </div>
  )
}

/** App-wide ⌘ shortcuts, captured before editors and terminals see them. */
function useGlobalShortcuts(focusSearch: () => void, focusCommit: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey || e.altKey || e.ctrlKey) return
      const key = e.key.toLowerCase()
      const ws = useWorkspace.getState()
      const handled = (() => {
        if (key === 'p' && !e.shiftKey) return ws.setQuickOpen(true), true
        if (key === 'f' && e.shiftKey) return ws.setSidebar('search'), focusSearch(), true
        if (key === 'b' && !e.shiftKey && !inEditor(e)) return ws.toggleSidebar(), true
        // ⌘L: hand the editor selection (or current file) to the agent chat.
        if (key === 'l' && !e.shiftKey) return void attachSelectionToAgent(), true
        // ⌘K / ⇧⌘K like IntelliJ; inside the terminal ⌘K keeps clearing the screen.
        if (key === 'k' && !inTerminal(e)) {
          if (e.shiftKey) return void useGit.getState().remote('push'), true
          return ws.setSidebar('git'), focusCommit(), true
        }
        if (key === 'j' && !e.shiftKey) return toggleTerminal(ws.root ?? ''), true
        // ⌘[ / ⌘] like IntelliJ's Back / Forward (e.g. file opened from the terminal → back).
        if (key === '[' && !e.shiftKey) return useEditor.getState().back(), true
        if (key === ']' && !e.shiftKey) return useEditor.getState().forward(), true
        if (key === 't' && e.shiftKey) return useTerminal.getState().create(ws.root ?? ''), true
        if (key === 'n' && !e.shiftKey) return void useScratch.getState().create('sql'), true
        // ⌘O shows Recent Projects (the native File menu sends the same event).
        if (key === 'o' && !e.shiftKey) return ws.setRecentOpen(true), true
        if (key === 'w' && !e.shiftKey) {
          const { tabs, activeId } = useEditor.getState()
          const tab = tabs.find((t) => t.id === activeId)
          if (tab) void requestCloseTab(tab)
          return true
        }
        return false
      })()
      if (handled) {
        e.preventDefault()
        e.stopPropagation()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [focusSearch, focusCommit])
}

// ⌘B in the editor means "go to declaration" (IntelliJ), not "toggle sidebar".
function inEditor(e: KeyboardEvent): boolean {
  return e.target instanceof HTMLElement && !!e.target.closest('.cm-editor')
}

function inTerminal(e: KeyboardEvent): boolean {
  return e.target instanceof HTMLElement && !!e.target.closest('.terminal-view')
}
