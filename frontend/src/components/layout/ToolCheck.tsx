import { useMemo } from 'react'
import { Button, FormActions } from '@flint-ide/ui'
import { BrowserOpenURL, ClipboardSetText } from '../../wails/runtime'
import { closeToolCheck, recheckTools, useToolCheck } from '../../store/toolCheck'
import { showToast } from '../../store/dialog'
import { groupTools, lookedFor, statusLabel, summarize, toolTone, type ToolStatus } from '../../lib/toolCheck'
import { Modal } from './Modal'

/**
 * Which external tools are installed, what each enables, and how to install
 * the missing ones. Nothing is installed from here: commands are copied for
 * the user to run in a terminal.
 */
export function ToolCheck() {
  const tools = useToolCheck((s) => s.tools)
  const checking = useToolCheck((s) => s.checking)
  const error = useToolCheck((s) => s.error)
  const groups = useMemo(() => groupTools(tools ?? []), [tools])
  const summary = useMemo(() => (tools ? summarize(tools) : null), [tools])

  return (
    <Modal title="Tools" onClose={closeToolCheck} className="tool-check">
      <p className="muted tool-check-intro">
        Flint uses the tools already on your computer. Only Git is required; each of the others turns on the features listed next to it.
      </p>
      <div className="tool-check-list">
        {!tools && !error && <div className="muted tool-check-empty">Checking…</div>}
        {error && <div className="error tool-check-empty">{error}</div>}
        {groups.map((g) => (
          <section key={g.id}>
            <h3 className="tool-group-title">{g.title}</h3>
            {g.tools.map((t) => <ToolRow key={t.id} tool={t} />)}
          </section>
        ))}
      </div>
      <p className="muted tool-check-note">
        Flint looks for tools on your login shell&apos;s PATH. If a tool you installed still shows as missing, check that it runs in a new terminal window, then restart Flint.
      </p>
      <FormActions>
        <span className="fui-grow">
          {summary && <span className={`tool-check-status ${summary.tone}`}>{summary.text}</span>}
        </span>
        <Button variant="plain" onClick={() => void recheckTools()} disabled={checking}>{checking ? 'Checking…' : 'Recheck'}</Button>
        <Button variant="primary" autoFocus onClick={closeToolCheck}>Done</Button>
      </FormActions>
    </Modal>
  )
}

function ToolRow({ tool: t }: { tool: ToolStatus }) {
  const tone = toolTone(t)
  const copy = async () => {
    if (await ClipboardSetText(t.install)) showToast(`Copied: ${t.install}`)
  }
  return (
    <div className="tool-row">
      <span className={`tool-dot ${tone}`} />
      <div className="tool-main">
        <div className="tool-head">
          <span className="tool-name">{t.name}</span>
          {t.required && <span className="muted">required</span>}
          <span className="spacer" />
          <span className={`tool-check-status ${tone}`} title={t.detail || (t.state === 'missing' ? `Looked for ${lookedFor(t.binaries)}` : '')}>
            {statusLabel(t)}
          </span>
        </div>
        <div className="tool-feature muted">
          {t.feature}
          {' · '}
          <Button variant="link" size="sm" className="tool-docs" onClick={() => BrowserOpenURL(t.docsUrl)}>Docs</Button>
        </div>
        {t.state === 'error' && t.detail && <div className="tool-detail selectable">{t.detail}</div>}
        {t.state !== 'ok' && (
          <div className="tool-install">
            <code className="selectable">{t.install}</code>
            <Button variant="plain" size="sm" onClick={() => void copy()}>Copy</Button>
          </div>
        )}
      </div>
    </div>
  )
}
