// An extension's settings schema (its manifest's contributes.settings, checked
// by Go: internal/extensions/manifest/schema.go) as the generated Settings page
// uses it: the value to show and whether a draft may be saved. The rules are
// Go's, so a value this page saves is one the extension receives unchanged.
// Framework-free.

/** One setting, as host.Info carries it. */
export interface SchemaProperty {
  key: string
  type: string // string, integer, number, boolean, array
  title?: string
  description?: string
  default: unknown
  enum?: string[]
  minimum?: number | null
  maximum?: number | null
  maxLength?: number
  pattern?: string
  format?: string
}

const HOST = /^(\*\.)?[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*(:\d{1,5})?$/
const MAX_STRING = 4096
const MAX_ITEMS = 200

function validURI(s: string): boolean {
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== ''
  } catch {
    return false
  }
}

function checkString(p: SchemaProperty, s: string): string {
  const limit = p.maxLength || MAX_STRING
  if ([...s].length > limit) return `at most ${limit} characters`
  if (p.enum?.length && !p.enum.includes(s)) return `one of ${p.enum.join(', ')}`
  if (p.pattern) {
    try {
      if (!new RegExp(`^(?:${p.pattern})$`, 'u').test(s)) return `must match ${p.pattern}`
    } catch {
      // A Go regexp JavaScript can't read: Go checks it on use.
    }
  }
  if (p.format === 'hostname' && s !== '' && !HOST.test(s)) return 'a host name'
  if (p.format === 'uri' && s !== '' && !validURI(s)) return 'an http or https address'
  return ''
}

/** Why v is not a valid value ('' when it is). */
export function checkValue(p: SchemaProperty, v: unknown): string {
  switch (p.type) {
    case 'boolean':
      return typeof v === 'boolean' ? '' : 'true or false'
    case 'integer':
    case 'number': {
      if (typeof v !== 'number' || !Number.isFinite(v)) return 'a number'
      if (p.type === 'integer' && !Number.isInteger(v)) return 'a whole number'
      const min = p.minimum ?? -Infinity
      const max = p.maximum ?? Infinity
      return v < min || v > max ? `between ${p.minimum ?? '…'} and ${p.maximum ?? '…'}` : ''
    }
    case 'string':
      return typeof v === 'string' ? checkString(p, v) : 'text'
    case 'array': {
      if (!Array.isArray(v) || v.some((x) => typeof x !== 'string')) return 'a list of text'
      if (v.length > MAX_ITEMS) return `at most ${MAX_ITEMS} entries`
      for (const s of v as string[]) {
        const why = checkString(p, s)
        if (why) return `${s}: ${why}`
      }
      return ''
    }
  }
  return 'unsupported type'
}

/** The value to show: the stored one when valid, else the default. */
export function currentValue(p: SchemaProperty, stored: Readonly<Record<string, unknown>>): unknown {
  const v = stored[p.key]
  return v !== undefined && checkValue(p, v) === '' ? v : p.default
}

/** A list setting edited as lines: blank lines are dropped. */
export function linesToList(text: string): string[] {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l !== '')
}
