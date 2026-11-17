import { useTerminal } from '../../store/terminal'
import {
  attachBackgroundTerminal, closeBackgroundTerminals, endBackgroundTerminal, useBackgroundTerminals,
} from '../../store/backgroundTerminals'
import { backgroundTerminalLabel, unclaimedTerminals } from '../../lib/backgroundTerminals'
import { Modal } from './Modal'

/**
 * Terminals the background host runs that no tab shows (the session file was
 * lost, or they belong to another project): attach one in a tab, or end it.
 */
export function BackgroundTerminals() {
  const live = useBackgroundTerminals((s) => s.live)
  const open = useTerminal((s) => s.tabs)
  const list = unclaimedTerminals(live, open.map((t) => t.id))

  return (
    <Modal title="Background Terminals" onClose={closeBackgroundTerminals} className="tool-check">
      <p className="muted tool-check-intro">
        These terminals kept running in the background after Flint closed, and no tab shows them. Attach opens one in a tab with its recent output; End stops its shell.
      </p>
      <div className="tool-check-list">
        {list.length === 0 && <div className="muted tool-check-empty">Every background terminal is open in a tab.</div>}
        {list.map((t, i) => (
          <div key={t.id} className="tool-row">
            <span className={`tool-dot ${t.running ? 'ok' : 'warn'}`} />
            <div className="tool-main">
              <div className="tool-head">
                <span className="tool-name selectable">{backgroundTerminalLabel(t, i)}</span>
                <span className="spacer" />
                <button onClick={() => attachBackgroundTerminal(t.id)}>Attach</button>
                <button onClick={() => void endBackgroundTerminal(t.id)}>End</button>
              </div>
            </div>
          </div>
        ))}
      </div>
      <div className="form-actions">
        <span className="spacer" />
        <button className="primary" autoFocus onClick={closeBackgroundTerminals}>Done</button>
      </div>
    </Modal>
  )
}
