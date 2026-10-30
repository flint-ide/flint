import { useEffect } from 'react'
import { useTerminal } from '../../store/terminal'
import { useWorkspace } from '../../store/workspace'
import { TerminalView } from './TerminalView'

/** Terminal dock: tabs of shells where agents (claude, codex) do the actual coding. */
export function TerminalPanel() {
  const { tabs, activeId, open, create, close, setActive } = useTerminal()
  const root = useWorkspace((s) => s.root)
  const ready = useWorkspace((s) => s.ready)

  // Always have a shell ready in the project folder (once we know which folder that is).
  useEffect(() => {
    if (open && ready && tabs.length === 0) create(root ?? '')
  }, [open, ready, tabs.length, root, create])

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
            <button
              className="tab-close"
              aria-label="Close terminal"
              onClick={(e) => {
                e.stopPropagation()
                close(t.id)
              }}
            />
          </div>
        ))}
        <button className="icon new-tab" title="New terminal" onClick={() => create(root ?? '')}>＋</button>
      </div>
      <div className="tab-panels">
        {tabs.map((t) => (
          <div key={t.id} className="tab-panel" hidden={t.id !== activeId}>
            <TerminalView tab={t} active={t.id === activeId} visible={open} />
          </div>
        ))}
      </div>
    </div>
  )
}
