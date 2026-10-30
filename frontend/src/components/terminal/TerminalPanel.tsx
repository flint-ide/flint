import { Suspense, lazy, useEffect, useMemo, useState } from 'react'
import { useTerminal } from '../../store/terminal'
import { useWorkspace } from '../../store/workspace'
import { chatTabId, useAgentChat } from '../../store/agentChat'
import { TerminalView } from './TerminalView'

const AgentChat = lazy(() => import('../agent/AgentChat').then((m) => ({ default: m.AgentChat })))

/** Right dock: shells and agent conversations, side by side with the editor. */
export function TerminalPanel() {
  const { tabs, activeId, open, create, close, setActive } = useTerminal()
  const root = useWorkspace((s) => s.root)
  const ready = useWorkspace((s) => s.ready)
  // Select stable references; deriving a new array inside the selector would re-render forever.
  const order = useAgentChat((s) => s.order)
  const sessions = useAgentChat((s) => s.sessions)
  const chats = useMemo(() => order.map((id) => sessions[id]).filter(Boolean), [order, sessions])
  const agents = useAgentChat((s) => s.agents)
  const starting = useAgentChat((s) => s.starting)
  const startChat = useAgentChat((s) => s.start)
  const stopChat = useAgentChat((s) => s.stop)
  const [menu, setMenu] = useState(false)

  // Always have a shell ready in the project folder (once we know which folder that is).
  useEffect(() => {
    if (open && ready && tabs.length === 0 && chats.length === 0) create(root ?? '')
  }, [open, ready, tabs.length, chats.length, root, create])

  const closeChat = (id: string) => {
    void stopChat(id)
    if (activeId === chatTabId(id)) setActive(tabs[tabs.length - 1]?.id ?? '')
  }

  return (
    <div className="terminal-panel">
      <div className="tab-bar terminal-tabs">
        {tabs.map((t) => (
          <div
            key={t.id}
            className={`tab${t.id === activeId ? ' active' : ''}${t.exitCode !== null ? ' exited' : ''}`}
            onClick={() => setActive(t.id)}
            title={t.cwd}
          >
            <span className="tab-icon terminal" />
            <span className="tab-title">{t.title}{t.exitCode !== null ? ` (exit ${t.exitCode})` : ''}</span>
            <button className="tab-close" aria-label="Close terminal" onClick={(e) => (e.stopPropagation(), close(t.id))} />
          </div>
        ))}
        {chats.map((c) => (
          <div
            key={c.id}
            className={`tab${chatTabId(c.id) === activeId ? ' active' : ''}`}
            onClick={() => setActive(chatTabId(c.id))}
            title={`${c.agent} in ${c.cwd}`}
          >
            <span className={`tab-icon agent${c.chat.busy ? ' busy' : ''}`} />
            <span className="tab-title">✦ {c.agent}</span>
            <button className="tab-close" aria-label="Close conversation" onClick={(e) => (e.stopPropagation(), closeChat(c.id))} />
          </div>
        ))}
        <span className="new-tab-wrap">
          <button className="icon new-tab" title="New terminal or agent chat" onClick={() => setMenu(!menu)}>＋</button>
          {menu && (
            <div className="new-tab-menu" onMouseLeave={() => setMenu(false)}>
              <button onClick={() => (create(root ?? ''), setMenu(false))}>❯_ Terminal</button>
              {agents.map((a) => (
                <button key={a} disabled={!!starting} onClick={() => (void startChat(a, root ?? ''), setMenu(false))}>
                  ✦ {a} chat
                </button>
              ))}
              {agents.length === 0 && <span className="muted">No agents installed</span>}
            </div>
          )}
          {starting && <span className="muted starting">starting {starting}…</span>}
        </span>
      </div>
      <div className="tab-panels">
        {tabs.map((t) => (
          <div key={t.id} className="tab-panel" hidden={t.id !== activeId}>
            <TerminalView tab={t} active={t.id === activeId} visible={open} />
          </div>
        ))}
        {chats.map((c) => (
          <div key={c.id} className="tab-panel" hidden={chatTabId(c.id) !== activeId}>
            <Suspense fallback={null}>
              <AgentChat session={c} active={chatTabId(c.id) === activeId} />
            </Suspense>
          </div>
        ))}
      </div>
    </div>
  )
}
