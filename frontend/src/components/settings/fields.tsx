import { useEffect, useState, type ReactNode } from 'react'

/** A labelled settings row with an optional hint under the label. */
export function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="settings-row">
      <div className="settings-label">
        <span>{label}</span>
        {hint && <span className="muted">{hint}</span>}
      </div>
      <div className="settings-control">{children}</div>
    </div>
  )
}

/** A number input that saves valid values as you type and resets to the saved one on blur. */
export function NumberField(props: { value: number; min: number; max: number; step: number; suffix?: string; onCommit: (v: number) => void }) {
  const { value, min, max, step, suffix, onCommit } = props
  const [draft, setDraft] = useState(String(value))
  useEffect(() => setDraft(String(value)), [value])
  const parse = (s: string) => {
    const n = Number(s)
    return s.trim() !== '' && Number.isFinite(n) && n >= min && n <= max ? n : null
  }
  const invalid = parse(draft) === null
  return (
    <span className="number-field">
      <input
        type="number"
        className={invalid ? 'invalid' : ''}
        value={draft}
        min={min}
        max={max}
        step={step}
        onChange={(e) => {
          setDraft(e.target.value)
          const n = parse(e.target.value)
          if (n !== null && n !== value) onCommit(n)
        }}
        onBlur={() => setDraft(String(value))}
      />
      {suffix && <span className="muted">{suffix}</span>}
      {invalid && <span className="error">{min}–{max}</span>}
    </span>
  )
}

/** A text input that saves on Enter or blur when valid. */
export function TextField(props: { value: string; list?: string; valid: (v: string) => boolean; onCommit: (v: string) => void }) {
  const { value, list, valid, onCommit } = props
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])
  const commit = () => {
    if (draft !== value && valid(draft)) onCommit(draft)
    else setDraft(value)
  }
  return (
    <input
      className={`settings-text${valid(draft) ? '' : ' invalid'}`}
      value={draft}
      list={list}
      spellCheck={false}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit()
        if (e.key === 'Escape') setDraft(value)
      }}
    />
  )
}

/**
 * A text input (or textarea, one value per line) whose value Go checks before
 * it is saved: on blur or Enter, check returns '' to save or the reason it
 * would be refused, shown under the field while the draft is kept.
 */
export function CheckedField(props: {
  value: string
  placeholder?: string
  multiline?: boolean
  list?: string
  check: (draft: string) => Promise<string>
  onCommit: (draft: string) => void
}) {
  const { value, placeholder, multiline, list, check, onCommit } = props
  const [draft, setDraft] = useState(value)
  const [error, setError] = useState('')
  useEffect(() => {
    setDraft(value)
    setError('')
  }, [value])
  const commit = async () => {
    const next = multiline ? draft : draft.trim()
    if (next === value) {
      setError('')
      return
    }
    const reason = await check(next).catch((err: unknown) => String(err))
    setError(reason)
    if (!reason) onCommit(next)
  }
  const shared = {
    value: draft,
    placeholder,
    spellCheck: false,
    onChange: (e: { target: { value: string } }) => setDraft(e.target.value),
    onBlur: () => void commit(),
  }
  return (
    <div className="settings-control settings-control-stack">
      {multiline ? (
        <textarea className={`settings-textarea${error ? ' invalid' : ''}`} {...shared} />
      ) : (
        <input
          className={`settings-text${error ? ' invalid' : ''}`}
          list={list}
          {...shared}
          onKeyDown={(e) => {
            if (e.key === 'Enter') void commit()
            if (e.key === 'Escape') setDraft(value)
          }}
        />
      )}
      {error && <span className="error">{error}</span>}
    </div>
  )
}
