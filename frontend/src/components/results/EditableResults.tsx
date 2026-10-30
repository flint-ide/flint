import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Database, editBatch, errorMessage, type Driver, type EditTarget, type QueryResult } from '../../api/backend'
import { addRow, buildBatch, changeCount, editability, noEdits, toggleDelete, type StagedEdits } from '../../lib/rowEdits'
import { confirmAction, showToast } from '../../store/dialog'
import { Modal } from '../layout/Modal'
import { ResultsGrid } from './ResultsGrid'

interface Props {
  result: QueryResult
  summary: ReactNode
  connId: string
  database: string
  driver: Driver | undefined
  readOnly: boolean
  /** Called after changes were applied (the console re-runs its query). */
  onApplied: () => void
}

type Lookup = { target: EditTarget | null; error: string } | null

/**
 * A result grid whose rows can be edited when it comes from one Postgres table with a
 * primary key: changes are staged in the grid and applied with Submit as one transaction.
 */
export function EditableResults({ result, summary, connId, database, driver, readOnly, onApplied }: Props) {
  const [lookup, setLookup] = useState<Lookup>(null)
  const [edits, setEdits] = useState<StagedEdits>(noEdits)
  const [preview, setPreview] = useState<string | null>(null)

  // A new result drops staged edits and looks up its source table (off the critical path:
  // the rows are already on screen).
  useEffect(() => {
    setEdits(noEdits)
    setLookup(null)
    setPreview(null)
    if (driver !== 'postgres' || !result.tableOid) return
    let cancelled = false
    Database.EditTarget(connId, database, result.tableOid)
      .then((target) => !cancelled && setLookup({ target, error: '' }))
      .catch((err) => !cancelled && setLookup({ target: null, error: errorMessage(err) }))
    return () => {
      cancelled = true
    }
  }, [result, connId, database, driver])

  const target = lookup?.target ?? null
  const ability = useMemo(() => (target ? editability(result, target, readOnly) : null), [result, target, readOnly])
  const blocked = driver !== 'postgres' ? '' : (result.editBlocked || lookup?.error || (ability && !ability.editable ? ability.reason : ''))
  const count = changeCount(edits)

  const openPreview = async () => {
    if (!target) return
    try {
      setPreview(await Database.PreviewEdits(connId, database, editBatch(buildBatch(result, target, edits))))
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  }

  const revert = async () => {
    if (await confirmAction(`Discard ${count} staged change${count === 1 ? '' : 's'}?`, 'Revert')) setEdits(noEdits)
  }

  const actions = (selectedRow: number | null): ReactNode => {
    if (!ability?.editable || !target) {
      return blocked ? <span className="muted edit-blocked" title={`Rows can’t be edited: ${blocked}`}>read-only</span> : null
    }
    const deleted = selectedRow !== null && !!edits.deleted[selectedRow]
    return (
      <>
        <span className="edit-target" title={`Double-click a cell (or ↵) to edit ${target.schema}.${target.table}; ⌘⌫ deletes the selected row`}>
          ✎ {target.table}
        </span>
        <button className="link" onClick={() => setEdits((e) => addRow(e))} title="Add a row">+ Row</button>
        <button
          className="link"
          disabled={selectedRow === null}
          onClick={() => selectedRow !== null && setEdits((e) => toggleDelete(result, e, selectedRow))}
          title="Delete the selected row (⌘⌫)"
        >
          {deleted ? 'Restore Row' : '− Row'}
        </button>
        {count > 0 && (
          <>
            <span className="edit-count">{count} change{count === 1 ? '' : 's'}</span>
            <button className="link" onClick={() => void revert()}>Revert</button>
            <button className="primary small" onClick={() => void openPreview()}>Submit…</button>
          </>
        )}
      </>
    )
  }

  return (
    <>
      <ResultsGrid
        result={result}
        summary={summary}
        actions={actions}
        editing={ability?.editable ? { columns: ability.columns, edits, onChange: setEdits } : undefined}
      />
      {preview !== null && target && (
        <SubmitDialog
          sql={preview}
          table={`${target.schema}.${target.table}`}
          onClose={() => setPreview(null)}
          onApply={async () => {
            const res = await Database.ApplyEdits(connId, database, editBatch(buildBatch(result, target, edits)))
            setPreview(null)
            setEdits(noEdits)
            showToast(`Applied ${res.statements} statement${res.statements === 1 ? '' : 's'} (${res.affected} row${res.affected === 1 ? '' : 's'})`)
            onApplied()
          }}
        />
      )}
    </>
  )
}

interface SubmitProps {
  sql: string
  table: string
  onClose: () => void
  onApply: () => Promise<void>
}

/** Shows the generated SQL and applies it on confirmation. */
function SubmitDialog({ sql, table, onClose, onApply }: SubmitProps) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const apply = async () => {
    setBusy(true)
    setError('')
    try {
      await onApply()
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <Modal title={`Submit changes to ${table}`} onClose={onClose} className="submit-edits">
      <pre className="sql-preview selectable">{sql}</pre>
      <p className="muted hint">
        Runs in one transaction; values are sent as bind parameters (inlined above only for reading). An UPDATE or DELETE
        that doesn’t hit exactly one row rolls everything back.
      </p>
      {error && <div className="form-error selectable">{error}</div>}
      <div className="form-actions">
        <button className="link" onClick={() => void navigator.clipboard.writeText(sql).then(() => showToast('Copied SQL'))}>
          Copy SQL
        </button>
        <span className="spacer" />
        <button onClick={onClose}>Cancel</button>
        <button className="primary" autoFocus disabled={busy} onClick={() => void apply()}>
          {busy ? 'Applying…' : 'Apply'}
        </button>
      </div>
    </Modal>
  )
}
