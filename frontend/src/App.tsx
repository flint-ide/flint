import { Suspense, lazy, useEffect, useRef, useState } from 'react'
import { Workspace } from './api/backend'
import { useWorkspace } from './store/workspace'
import { useEditor } from './store/editor'
import { useDatabase } from './store/database'
import { useScratch } from './store/scratch'
import { useTerminal, subscribeTerminalEvents } from './store/terminal'
import { subscribeSearchEvents } from './store/search'
import { subscribeFsEvents } from './store/fsEvents'
import { subscribeQuit } from './store/quit'
import { subscribeGitEvents, useGit } from './store/git'
import { showToast } from './store/dialog'
import { ActivityBar } from './components/layout/ActivityBar'
import { Splitter } from './components/layout/Splitter'
import { Overlays } from './components/layout/Overlays'
import { StatusBar } from './components/layout/StatusBar'
import { EditorArea, requestCloseTab } from './components/editor/EditorArea'
import { TerminalPanel } from './components/terminal/TerminalPanel'
import { FileTree } from './components/sidebar/FileTree'
import { SearchPanel } from './components/sidebar/SearchPanel'
import { ScratchPanel } from './components/sidebar/ScratchPanel'
import { usePersistentNumber } from './lib/usePersistentNumber'

const ACTIVITY_BAR = 44

const DatabasePanel = lazy(() => import('./components/sidebar/DatabasePanel').then((m) => ({ default: m.DatabasePanel })))
const CommitPanel = lazy(() => import('./components/sidebar/CommitPanel').then((m) => ({ default: m.CommitPanel })))
const QuickOpen = lazy(() => import('./components/QuickOpen').then((m) => ({ default: m.QuickOpen })))

export default function App() {
  const sidebar = useWorkspace((s) => s.sidebar)
  const sidebarVisible = useWorkspace((s) => s.sidebarVisible)
  const quickOpen = useWorkspace((s) => s.quickOpen)
  const root = useWorkspace((s) => s.root)
  const error = useWorkspace((s) => s.error)
  const terminalOpen = useTerminal((s) => s.open)
  const [sidebarWidth, setSidebarWidth] = usePersistentNumber('flint.sidebarWidth', 280)
  const [editorRatio, setEditorRatio] = usePersistentNumber('flint.editorRatio', 0.5)
  const [searchFocus, setSearchFocus] = useState(0)
  const [commitFocus, setCommitFocus] = useState(0)
  const workbench = useRef<HTMLDivElement>(null)

  useEffect(() => {
    subscribeSearchEvents()
    subscribeTerminalEvents()
    subscribeFsEvents()
    subscribeQuit()
    subscribeGitEvents()
    void useDatabase.getState().loadConnections()
    void useScratch.getState().load()
    void useWorkspace.getState().init()
  }, [])

  useEffect(() => {
    if (!error) return
    showToast(error, 'error')
    useWorkspace.getState().clearError()
  }, [error])

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
      <header className="titlebar" style={{ '--wails-draggable': 'drag' } as React.CSSProperties}>
        <span className="title">{root ? root.split('/').pop() : 'flint'}</span>
        {root && <span className="muted title-path">{root}</span>}
      </header>
      <div className="workbench" ref={workbench}>
        <ActivityBar onTerminal={() => useTerminal.getState().toggle()} terminalOpen={terminalOpen} />
        {sidebarVisible && (
          <>
            <aside className="sidebar" style={{ width: sidebarWidth }}>
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
            </aside>
            <Splitter direction="horizontal" onResize={onSidebarResize} />
          </>
        )}
        <main className="main">
          <div className="main-editor" style={{ flex: terminalOpen ? `${editorRatio} 1 0` : '1 1 0' }}>
            <EditorArea />
          </div>
          {/* Kept mounted while hidden so shells (and running agents) survive toggling. */}
          <Splitter direction="horizontal" onResize={(r) => setEditorRatio(Math.min(0.85, Math.max(0.15, r)))} hidden={!terminalOpen} />
          <div className="main-terminal" style={{ flex: `${1 - editorRatio} 1 0` }} hidden={!terminalOpen}>
            <TerminalPanel />
          </div>
        </main>
      </div>
      <StatusBar />
      {quickOpen && (
        <Suspense fallback={null}>
          <QuickOpen />
        </Suspense>
      )}
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
        if (key === 'b' && !e.shiftKey) return ws.toggleSidebar(), true
        // ⌘K / ⇧⌘K like IntelliJ; inside the terminal ⌘K keeps clearing the screen.
        if (key === 'k' && !inTerminal(e)) {
          if (e.shiftKey) return void useGit.getState().remote('push'), true
          return ws.setSidebar('git'), focusCommit(), true
        }
        if (key === 'j' && !e.shiftKey) return useTerminal.getState().toggle(), true
        if (key === 't' && e.shiftKey) return useTerminal.getState().create(ws.root ?? ''), true
        if (key === 'n' && !e.shiftKey) return void useScratch.getState().create('sql'), true
        if (key === 'o' && !e.shiftKey) {
          void Workspace.PickFolder().then((p) => (p ? ws.openFolder(p) : undefined))
          return true
        }
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

function inTerminal(e: KeyboardEvent): boolean {
  return e.target instanceof HTMLElement && !!e.target.closest('.terminal-view')
}
