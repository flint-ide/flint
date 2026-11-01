import { useMemo } from 'react'
import { Modal } from '../layout/Modal'
import { applyImport, closeVSCodeImport, previewImport, useVSCodeImport } from '../../store/vscodeImport'
import { useSettings } from '../../store/settings'
import { listCommands } from '../../lib/commands'
import { commandTitle, groupByKind, importedKeymap, keybindingNote, type ImportItem } from '../../lib/vscodeImport'

/**
 * Preview of "Import from VS Code…": what would change and what is skipped
 * (and why). Nothing is written until Import is clicked.
 */
export function ImportVSCodeDialog() {
  const sources = useVSCodeImport((s) => s.sources)
  const plan = useVSCodeImport((s) => s.plan)
  const busy = useVSCodeImport((s) => s.busy)
  const error = useVSCodeImport((s) => s.error)
  const keymap = useSettings((s) => s.settings.keymap)
  const commands = useMemo(() => listCommands(), [])
  const nextKeymap = useMemo(() => importedKeymap(plan?.items ?? [], keymap), [plan, keymap])
  const items = useMemo(() => groupByKind(plan?.items ?? []), [plan])
  const skipped = useMemo(() => groupByKind(plan?.skipped ?? []), [plan])

  const describe = (it: ImportItem) =>
    it.kind === 'keybinding'
      ? { value: keybindingNote(it, commands, nextKeymap), target: commandTitle(it.target, commands) }
      : { value: it.value, target: it.target }

  return (
    <Modal title="Import from VS Code" onClose={closeVSCodeImport} className="tool-check vscode-import">
      <p className="muted tool-check-intro">
        Flint reads your editor&apos;s settings.json, keybindings.json and color theme. Nothing changes until you click Import; afterwards everything
        can be changed in Settings.
      </p>
      {sources && sources.length === 0 && <p className="tool-check-empty">No VS Code, Cursor or VSCodium settings were found in your home folder.</p>}
      {sources && sources.length > 1 && (
        <div className="segmented import-sources" role="radiogroup" aria-label="Import from">
          {sources.map((s) => (
            <button
              key={s.id}
              role="radio"
              aria-checked={plan?.source.id === s.id}
              className={plan?.source.id === s.id ? 'on' : ''}
              disabled={busy}
              title={s.userDir}
              onClick={() => void previewImport(s.id)}
            >
              {s.name}
            </button>
          ))}
        </div>
      )}
      <div className="tool-check-list">
        {busy && !plan && <div className="muted tool-check-empty">Reading…</div>}
        {plan && items.length === 0 && <div className="muted tool-check-empty">Nothing to import from {plan.source.name}.</div>}
        {items.map((g) => (
          <section key={g.kind}>
            <h3 className="tool-group-title">{g.title}</h3>
            {g.entries.map((it) => {
              const d = describe(it)
              return (
                <div key={`${it.name}→${it.target}`} className="tool-row import-row">
                  <span className="tool-dot ok" />
                  <span className="import-name selectable" title={it.name}>{it.name}</span>
                  <span className="import-value selectable">{d.value}</span>
                  <span className="import-target muted">{d.target}</span>
                </div>
              )
            })}
          </section>
        ))}
        {skipped.length > 0 && (
          <details className="import-skipped">
            <summary className="tool-group-title">Skipped ({plan?.skipped.length})</summary>
            {skipped.map((g) => (
              <section key={g.kind}>
                <h3 className="tool-group-title">{g.title}</h3>
                {g.entries.map((s, i) => (
                  <div key={`${s.name}-${i}`} className="tool-row import-row">
                    <span className="tool-dot" />
                    <span className="import-name selectable" title={s.name}>{s.name}</span>
                    <span className="import-reason muted">{s.reason}</span>
                  </div>
                ))}
              </section>
            ))}
          </details>
        )}
      </div>
      {error && <p className="error tool-check-note">{error}</p>}
      <div className="form-actions">
        <span className="spacer" />
        <button onClick={closeVSCodeImport}>Cancel</button>
        <button className="primary" autoFocus disabled={busy || !plan || plan.items.length === 0} onClick={() => void applyImport()}>
          {busy && plan ? 'Importing…' : 'Import'}
        </button>
      </div>
    </Modal>
  )
}
