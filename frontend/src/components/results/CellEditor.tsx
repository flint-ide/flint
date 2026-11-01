import { useState, type CSSProperties } from 'react'
import { validateCell, type CellValue, type TargetColumn } from '../../lib/rowEdits'
import { formatKeys } from '../../lib/commands'

interface Props {
  column: TargetColumn
  /** Current value; undefined for a new row's unset (DEFAULT) cell. */
  initial: CellValue | undefined
  style: CSSProperties
  onCommit: (value: CellValue) => void
  onCancel: () => void
}

const BOOL_TEXT: Record<string, string> = { t: 'true', true: 'true', f: 'false', false: 'false' }

/**
 * In-place editor over a grid cell, by column kind: text and numbers in an input, JSON in
 * a textarea (⌘↵ commits), booleans in a select; nullable columns get a NULL toggle.
 * Enter commits, Escape cancels, leaving the editor commits when the value is valid.
 */
export function CellEditor({ column, initial, style, onCommit, onCancel }: Props) {
  const kind = column.kind
  const [text, setText] = useState(initial ?? '')
  const [isNull, setNull] = useState(initial === null)
  const [error, setError] = useState('')

  const commit = (value: CellValue = isNull ? null : text): boolean => {
    const err = validateCell(kind, value, column)
    if (err) {
      setError(err)
      return false
    }
    onCommit(value)
    return true
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    e.stopPropagation() // the grid's own shortcuts (⌘C, ⌘⌫) don't apply while editing
    if (e.key === 'Escape') {
      e.preventDefault()
      onCancel()
    } else if (e.key === 'Enter' && (kind !== 'json' || e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      commit()
    }
  }

  const onBlur = (e: React.FocusEvent) => {
    if (e.currentTarget.contains(e.relatedTarget as Node | null)) return
    if (!commit()) onCancel()
  }

  const change = (value: string) => {
    setText(value)
    setError('')
  }

  let field: React.ReactNode
  if (kind === 'bool') {
    const value = isNull ? 'NULL' : (BOOL_TEXT[text.trim().toLowerCase()] ?? text)
    field = (
      <select
        autoFocus
        value={value}
        onChange={(e) => {
          const v = e.target.value
          commit(v === 'NULL' ? null : v)
        }}
      >
        <option value="true">true</option>
        <option value="false">false</option>
        {!column.notNull && <option value="NULL">NULL</option>}
        {!['true', 'false', 'NULL'].includes(value) && <option value={value}>{value}</option>}
      </select>
    )
  } else if (kind === 'json') {
    field = (
      <textarea
        autoFocus
        spellCheck={false}
        disabled={isNull}
        placeholder={isNull ? 'NULL' : 'JSON'}
        value={isNull ? '' : text}
        onChange={(e) => change(e.target.value)}
      />
    )
  } else {
    field = (
      <input
        autoFocus
        spellCheck={false}
        inputMode={kind === 'number' ? 'decimal' : undefined}
        disabled={isNull}
        placeholder={isNull ? 'NULL' : initial === undefined ? 'DEFAULT' : ''}
        value={isNull ? '' : text}
        onChange={(e) => change(e.target.value)}
        onFocus={(e) => e.currentTarget.select()}
      />
    )
  }

  return (
    <div className={`cell-editor kind-${kind}`} style={style} onKeyDown={onKeyDown} onBlur={onBlur}>
      <div className="cell-editor-row">
        {field}
        {kind !== 'bool' && !column.notNull && (
          <button
            className={`null-toggle${isNull ? ' active' : ''}`}
            title={isNull ? 'Use a value instead of NULL' : 'Set to NULL'}
            onMouseDown={(e) => e.preventDefault()} // keep focus in the field
            onClick={() => {
              setNull((n) => !n)
              setError('')
            }}
          >
            NULL
          </button>
        )}
        {kind === 'json' && (
          <button className="primary" onMouseDown={(e) => e.preventDefault()} onClick={() => commit()} title={`Apply (${formatKeys('Mod+Enter')})`}>
            OK
          </button>
        )}
      </div>
      {error && <div className="cell-editor-error">{error}</div>}
    </div>
  )
}
