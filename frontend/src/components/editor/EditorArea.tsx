import { Suspense, lazy, useLayoutEffect, useState } from 'react'
import { useEditor, type Tab } from '../../store/editor'
import { confirmAction } from '../../store/dialog'
import { useTerminal } from '../../store/terminal'
import { useAgentChat } from '../../store/agentChat'
import { useScratch } from '../../store/scratch'
import { useWorkspace } from '../../store/workspace'
import { Welcome } from '../layout/Welcome'
import { ErrorBoundary } from '../layout/ErrorBoundary'
import { FileIcon, TabKindIcon } from '../icons/FileIcon'
import { TerminalView } from '../terminal/TerminalView'

// CodeMirror, the SQL console and the agent chat load on first use, keeping startup
// to the layout plus the terminal.
const FileView = lazy(() => import('./FileView').then((m) => ({ default: m.FileView })))
const ScratchView = lazy(() => import('./ScratchView').then((m) => ({ default: m.ScratchView })))
const DiffView = lazy(() => import('./DiffView').then((m) => ({ default: m.DiffView })))
const AgentChat = lazy(() => import('../agent/AgentChat').then((m) => ({ default: m.AgentChat })))

/** Closes a tab, asking first when work could be lost. */
export async function requestCloseTab(tab: Tab): Promise<void> {
  switch (tab.kind) {
    case 'file':
      // Scratchpads autosave, so only file tabs can lose unsaved edits.
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
  }
  useEditor.getState().closeTab(tab.id)
}

export function EditorArea() {
  const tabs = useEditor((s) => s.tabs)
  const activeId = useEditor((s) => s.activeId)
  const setActive = useEditor((s) => s.setActive)
  const terminals = useTerminal((s) => s.tabs)
  const sessions = useAgentChat((s) => s.sessions)

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
            className={`tab${tab.id === activeId ? ' active' : ''}${tab.kind === 'terminal' && terminals.find((t) => t.id === tab.termId)?.exitCode != null ? ' exited' : ''}`}
            title={tab.kind === 'file' ? tab.path : tab.title}
            onClick={() => setActive(tab.id)}
            onMouseDown={(e) => e.button === 1 && void requestCloseTab(tab)}
          >
            <TabIcon tab={tab} busy={tab.kind === 'chat' && !!sessions[tab.sessionId]?.chat.busy} />
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
        {tabs.map((tab) => {
          const active = tab.id === activeId
          return (
            <div key={tab.id} className="tab-panel" data-tab-id={tab.id} hidden={!active}>
              <ErrorBoundary label={tab.title}>
                <Suspense fallback={null}>
                  {tab.kind === 'file' && <FileView tab={tab} active={active} />}
                  {tab.kind === 'scratch' && <ScratchView tab={tab} active={active} />}
                  {tab.kind === 'diff' && <DiffView tab={tab} />}
                  {tab.kind === 'terminal' && <TerminalTabView termId={tab.termId} active={active} />}
                  {tab.kind === 'chat' && sessions[tab.sessionId] && <AgentChat session={sessions[tab.sessionId]} active={active} />}
                </Suspense>
              </ErrorBoundary>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function TerminalTabView({ termId, active }: { termId: string; active: boolean }) {
  const tab = useTerminal((s) => s.tabs.find((t) => t.id === termId))
  return tab ? <TerminalView tab={tab} active={active} /> : null
}

function TabIcon({ tab, busy }: { tab: Tab; busy: boolean }) {
  switch (tab.kind) {
    case 'file':
      return <FileIcon name={tab.title} />
    case 'scratch':
      return <TabKindIcon kind={tab.scratchKind} />
    default:
      return <TabKindIcon kind={tab.kind} busy={busy} />
  }
}

/** "+" at the end of the tab bar: new terminal, agent chat or SQL console. */
function NewTabMenu() {
  const [open, setOpen] = useState(false)
  const root = useWorkspace((s) => s.root) ?? ''
  const agents = useAgentChat((s) => s.agents)
  const starting = useAgentChat((s) => s.starting)
  const close = () => setOpen(false)
  return (
    <span className="new-tab-wrap">
      <button className="icon new-tab" title="New terminal (⇧⌘T), agent chat or SQL console" onClick={() => setOpen(!open)}>＋</button>
      {open && (
        <div className="new-tab-menu" onMouseLeave={close}>
          <button onClick={() => (useTerminal.getState().create(root), close())}>
            <TabKindIcon kind="terminal" /> Terminal <kbd>⇧⌘T</kbd>
          </button>
          {agents.map((a) => (
            <button key={a} disabled={!!starting} onClick={() => (void useAgentChat.getState().start(a, root), close())}>
              <TabKindIcon kind="chat" /> {a} chat
            </button>
          ))}
          <button onClick={() => (void useScratch.getState().create('sql'), close())}>
            <TabKindIcon kind="sql" /> SQL console <kbd>⌘N</kbd>
          </button>
        </div>
      )}
      {starting && <span className="muted starting">starting {starting}…</span>}
    </span>
  )
}
