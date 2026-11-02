// The "extensions" part of settings.json (Go: internal/features/extensions/settings.go):
//
//   "extensions": { "enabled": { "<id>": false }, "agentTools": { "<id>": false }, "<id>": { …its settings } }
//
// Kept as the flat object it is in the file, so values of extensions that
// aren't installed survive every save. Framework-free.
import { defineSection } from '../../lib/settingsSections'

declare module '../../lib/settingsSections' {
  interface FeatureSettings {
    /** Installed extensions: on or off, tools offered, and their own settings by id. */
    extensions: ExtensionsSettings
  }
}

export type ExtensionsSettings = Readonly<Record<string, unknown>>

/** An extension id: publisher.name (Go: manifest.ValidID). */
const ID = /^[a-z0-9][a-z0-9-]{0,30}\.[a-z0-9][a-z0-9-]{0,30}$/

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

function flags(v: unknown): Record<string, boolean> {
  if (!isObject(v)) return {}
  return Object.fromEntries(Object.entries(v).filter(([id, on]) => ID.test(id) && typeof on === 'boolean')) as Record<string, boolean>
}

/** Keeps what Go keeps: the two flag maps and an object per valid id. */
export function coerceExtensions(v: unknown): ExtensionsSettings {
  if (!isObject(v)) return {}
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(v)) {
    if (key === 'enabled' || key === 'agentTools') {
      const f = flags(value)
      if (Object.keys(f).length) out[key] = f
    } else if (ID.test(key) && isObject(value)) out[key] = value
  }
  return out
}

/** On unless turned off. */
export function extensionEnabled(s: ExtensionsSettings, id: string): boolean {
  return flags(s.enabled)[id] !== false
}

/** Offered unless turned off. */
export function toolsOffered(s: ExtensionsSettings, id: string): boolean {
  return flags(s.agentTools)[id] !== false
}

/** The section with a flag set; true removes the entry (the default). */
export function withFlag(s: ExtensionsSettings, map: 'enabled' | 'agentTools', id: string, on: boolean): ExtensionsSettings {
  const rest = Object.fromEntries(Object.entries(flags(s[map])).filter(([k]) => k !== id))
  const next = on ? rest : { ...rest, [id]: false }
  const out: Record<string, unknown> = { ...s }
  if (Object.keys(next).length) out[map] = next
  else delete out[map]
  return out
}

/** The extension's stored settings object. */
export function storedValues(s: ExtensionsSettings, id: string): Readonly<Record<string, unknown>> {
  const v = s[id]
  return isObject(v) ? v : {}
}

/** The section with one of an extension's settings replaced. */
export function withValue(s: ExtensionsSettings, id: string, key: string, value: unknown): ExtensionsSettings {
  return { ...s, [id]: { ...storedValues(s, id), [key]: value } }
}

export const extensionsSettings = defineSection('extensions', {}, coerceExtensions)
