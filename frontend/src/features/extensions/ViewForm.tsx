import { useEffect, useId, useMemo, useRef, useState, type FormEvent } from 'react'
import type { Form, FormResult } from '../../../bindings/flint/internal/extensions/host/models'
import { errorMessage } from '../../api/backend'
import { Select } from '../../components/ui/Select'
import { fieldError, fieldLabel, initialValues, parseNumber, submitValues, validateForm, type FormField } from './formModel'
import { linesToList, type SchemaProperty } from './schema'

interface Props {
  form: Form
  /** Sends the values; Go checks them again and asks the extension. */
  onSubmit: (values: Record<string, unknown>) => Promise<FormResult>
  /** After the extension accepted the values: the view is loaded again. */
  onAccepted: () => void
}

/**
 * A form view of an extension: fields of the settings schema's types, checked
 * like a generated Settings page (schema.ts), and a submit button. Reasons a
 * value was refused, by Flint or by the extension, appear under the fields.
 * A secret field is a password input whose value Flint never gets back.
 */
export function ViewForm({ form, onSubmit, onAccepted }: Props) {
  const fields = form.fields as unknown as FormField[]
  // The same form again (a refresh that changed nothing) keeps what was typed.
  const shape = useMemo(() => JSON.stringify(fields), [fields])
  const [values, setValues] = useState(() => initialValues(fields))
  const [lists, setLists] = useState<Record<string, string>>({})
  const [touched, setTouched] = useState<ReadonlySet<string>>(new Set())
  const [refused, setRefused] = useState<Readonly<Record<string, string>>>({})
  const [problem, setProblem] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const root = useRef<HTMLFormElement>(null)
  const id = useId()

  useEffect(() => {
    setValues(initialValues(fields))
    setLists({})
    setTouched(new Set())
    setRefused({})
    setProblem('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shape])

  const set = (key: string, value: unknown) => {
    setValues((v) => ({ ...v, [key]: value }))
    setRefused((r) => (r[key] ? Object.fromEntries(Object.entries(r).filter(([k]) => k !== key)) : r))
    setNote('')
  }
  const touch = (key: string) => setTouched((t) => (t.has(key) ? t : new Set(t).add(key)))

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    const invalid = validateForm(fields, values)
    setTouched(new Set(fields.map((f) => f.property.key)))
    setProblem('')
    setNote('')
    if (Object.keys(invalid).length > 0) {
      const first = fields.find((f) => invalid[f.property.key])
      root.current?.querySelector<HTMLElement>(`[data-field="${CSS.escape(first?.property.key ?? '')}"]`)?.focus()
      return
    }
    setBusy(true)
    try {
      const res = await onSubmit(submitValues(fields, values))
      const errors: Record<string, string> = {}
      for (const [k, v] of Object.entries(res.errors ?? {})) if (v) errors[k] = v
      setRefused(errors)
      setProblem(res.error ?? '')
      if (Object.keys(errors).length === 0 && !res.error) {
        setNote(res.message || 'Done.')
        // A secret that was sent is not kept in the page.
        setValues((v) => ({ ...v, ...Object.fromEntries(fields.filter((f) => f.secret).map((f) => [f.property.key, ''])) }))
        setTouched(new Set())
        onAccepted()
      }
    } catch (err) {
      setProblem(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="ext-form" ref={root} noValidate onSubmit={(e) => void submit(e)}>
      {form.description && <p className="muted ext-form-description">{form.description}</p>}
      {fields.map((f) => {
        const key = f.property.key
        const local = touched.has(key) ? fieldError(f, values[key]) : ''
        const why = refused[key] || local
        const control = `${id}-${key}`
        return (
          <div key={key} className="ext-form-field">
            <label htmlFor={control} id={`${control}-label`}>
              {fieldLabel(f)}
              {f.required && <span className="ext-form-required" aria-hidden="true"> *</span>}
            </label>
            <FieldControl
              field={f}
              id={control}
              value={values[key]}
              list={lists[key]}
              invalid={why !== ''}
              describedBy={`${control}-hint`}
              onChange={(v) => set(key, v)}
              onList={(text) => {
                setLists((l) => ({ ...l, [key]: text }))
                set(key, linesToList(text))
              }}
              onBlur={() => touch(key)}
            />
            <div id={`${control}-hint`} className="ext-form-hint">
              {why && <span className="error" role="alert">{capital(why)}</span>}
              {f.property.description && <span className="muted">{f.property.description}</span>}
            </div>
          </div>
        )
      })}
      {problem && <div className="error ext-form-problem" role="alert">{problem}</div>}
      <div className="ext-form-actions">
        <button type="submit" className="primary" disabled={busy}>
          {busy ? 'Sending…' : form.submit}
        </button>
        {note && <span className="muted" role="status">{note}</span>}
      </div>
    </form>
  )
}

const capital = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

interface ControlProps {
  field: FormField
  id: string
  value: unknown
  /** An array field's text as typed (lines), until it is loaded again. */
  list: string | undefined
  invalid: boolean
  describedBy: string
  onChange: (v: unknown) => void
  onList: (text: string) => void
  onBlur: () => void
}

/** The control of one field, by the property's type as on a generated Settings page. */
function FieldControl({ field, id, value, list, invalid, describedBy, onChange, onList, onBlur }: ControlProps) {
  const p: SchemaProperty = field.property
  const shared = { id, 'data-field': p.key, 'aria-invalid': invalid || undefined, 'aria-describedby': describedBy, 'aria-required': field.required || undefined, onBlur }
  const cls = invalid ? ' invalid' : ''
  switch (p.type) {
    case 'boolean':
      return <input type="checkbox" {...shared} checked={value === true} onChange={(e) => onChange(e.target.checked)} />
    case 'integer':
    case 'number':
      return (
        <span className="number-field">
          <input
            type="number"
            {...shared}
            className={cls.trim()}
            value={typeof value === 'number' || typeof value === 'string' ? String(value) : ''}
            min={p.minimum ?? undefined}
            max={p.maximum ?? undefined}
            step={p.type === 'integer' ? 1 : 'any'}
            onChange={(e) => onChange(parseNumber(e.target.value))}
          />
        </span>
      )
    case 'array':
      return (
        <textarea
          {...shared}
          className={`settings-textarea${cls}`}
          value={list ?? (Array.isArray(value) ? value.join('\n') : '')}
          placeholder={field.placeholder || 'One per line'}
          spellCheck={false}
          onChange={(e) => onList(e.target.value)}
        />
      )
  }
  if (p.enum?.length) {
    return (
      <span data-field={p.key} tabIndex={-1} className="ext-form-select">
        <Select
          block
          value={typeof value === 'string' ? value : ''}
          title={fieldLabel(field)}
          options={p.enum.map((v) => ({ value: v, label: v }))}
          onChange={(v) => {
            onChange(v)
            onBlur()
          }}
        />
      </span>
    )
  }
  const text = typeof value === 'string' ? value : ''
  if (field.multiline) {
    return <textarea {...shared} className={`settings-textarea${cls}`} value={text} placeholder={field.placeholder} onChange={(e) => onChange(e.target.value)} />
  }
  return (
    <input
      {...shared}
      type={field.secret ? 'password' : 'text'}
      autoComplete={field.secret ? 'off' : undefined}
      className={`settings-text${cls}`}
      value={text}
      placeholder={field.placeholder}
      spellCheck={false}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}
