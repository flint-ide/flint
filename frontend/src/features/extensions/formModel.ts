// A form view of an extension (Go: internal/extensions/host/richviews.go) as
// the form component uses it. A field is a property of the settings schema
// (schema.ts: the same types, checks and controls as a generated Settings
// page) plus how to draw it. The checks here only spare a round trip: Go
// checks the submitted values again before the extension sees them.
// Framework-free.
import { checkValue, type SchemaProperty } from './schema'

export interface FormField {
  property: SchemaProperty
  /** The value to show; Go made it valid, and '' for a secret. */
  value: unknown
  multiline?: boolean
  secret?: boolean
  required?: boolean
  placeholder?: string
}

export type FormValues = Readonly<Record<string, unknown>>

/** The values a form starts with: each field's value when the schema accepts it, else its default. */
export function initialValues(fields: readonly FormField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) {
    const p = f.property
    out[p.key] = f.secret ? '' : f.value !== undefined && f.value !== null && checkValue(p, f.value) === '' ? f.value : p.default
  }
  return out
}

/** Why a field's value can't be submitted ('' when it can). */
export function fieldError(f: FormField, v: unknown): string {
  if (f.required && (v === '' || v === undefined || v === null || (Array.isArray(v) && v.length === 0))) return 'required'
  return checkValue(f.property, v)
}

/** The fields whose values can't be submitted, with the reason. */
export function validateForm(fields: readonly FormField[], values: FormValues): Record<string, string> {
  const out: Record<string, string> = {}
  for (const f of fields) {
    const why = fieldError(f, values[f.property.key])
    if (why) out[f.property.key] = why
  }
  return out
}

/** What is sent: the fields' values only, in the fields' order. */
export function submitValues(fields: readonly FormField[], values: FormValues): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) out[f.property.key] = values[f.property.key]
  return out
}

/** A number field's draft as a value: the number, or the text itself when it isn't one (so the check refuses it). */
export function parseNumber(draft: string): unknown {
  const s = draft.trim()
  const n = Number(s)
  return s !== '' && Number.isFinite(n) ? n : draft
}

/** Whether the form differs from what it was loaded with. */
export function isDirty(fields: readonly FormField[], values: FormValues): boolean {
  const start = initialValues(fields)
  return fields.some((f) => JSON.stringify(values[f.property.key]) !== JSON.stringify(start[f.property.key]))
}

/** A field's label: its title, else its id. */
export const fieldLabel = (f: FormField) => f.property.title || f.property.key
