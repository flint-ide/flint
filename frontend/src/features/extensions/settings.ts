// The "extensions" part of settings.json (Go: internal/features/extensions/settings.go):
//
//   "extensions": { "enabled": { "<id>": false }, "agentTools": { "<id>": false },
//                   "decorations": { "<set id>": false }, "indexUrl": "https://…/index.json", "indexPublicKey": "<base64>",
//                   "<id>": { …its settings } }
//
// Kept as the flat object it is in the file, so values of extensions that
// aren't installed survive every save. Framework-free.
import { defineSection } from '../../lib/settingsSections'
import { coerceToggles, DEFAULT_TOGGLES, type FeatureToggles } from '../../lib/featureToggles'

declare module '../../lib/settingsSections' {
  interface FeatureSettings {
    /** Installed extensions: on or off, tools offered, and their own settings by id. */
    extensions: ExtensionsSettings
    /** Built-in extensions (optional features) switched off; applied at the next start. */
    features: FeatureToggles
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

/** A decoration set's id: "<extension id>.<name>" (Go: validSetID). */
const isSetId = (id: string) => {
  const parts = id.split('.')
  return id.length <= 128 && parts.length >= 3 && parts.slice(2).join('.') !== '' && ID.test(`${parts[0]}.${parts[1]}`)
}

function setFlags(v: unknown): Record<string, boolean> {
  if (!isObject(v)) return {}
  return Object.fromEntries(Object.entries(v).filter(([id, on]) => isSetId(id) && typeof on === 'boolean')) as Record<string, boolean>
}

/** The signed extension index Flint downloads when asked ("" in settings: this one). Go: index.DefaultURL. */
export const DEFAULT_INDEX_URL = 'https://github.com/flint-ide/extensions/releases/latest/download/index.json'

/** An index address Go accepts (index.CheckURL): https, or http on this machine; "" is the default. */
export function validIndexUrl(v: string): boolean {
  if (v === '') return true
  if (v.length > 2000) return false
  let u: URL
  try {
    u = new URL(v)
  } catch {
    return false
  }
  if (u.username || u.password || !u.hostname) return false
  if (u.protocol === 'https:') return true
  return u.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(u.hostname)
}

/** An Ed25519 public key in base64 (Go: index.ParsePublicKey); "" is the key built into Flint. */
export function validIndexKey(v: string): boolean {
  return v === '' || /^[A-Za-z0-9+/]{43}=$/.test(v)
}

/** extensions.indexUrl and extensions.indexPublicKey ("" when unset or invalid). */
export function indexSettings(s: ExtensionsSettings): { url: string; publicKey: string } {
  const text = (v: unknown) => (typeof v === 'string' ? v.trim() : '')
  const url = text(s.indexUrl)
  const publicKey = text(s.indexPublicKey)
  return { url: validIndexUrl(url) ? url : '', publicKey: validIndexKey(publicKey) ? publicKey : '' }
}

/** The section with one of the two index settings replaced; "" removes it (the default). */
export function withIndexSetting(s: ExtensionsSettings, key: 'indexUrl' | 'indexPublicKey', value: string): ExtensionsSettings {
  const out: Record<string, unknown> = { ...s }
  if (value) out[key] = value
  else delete out[key]
  return out
}

/** Keeps what Go keeps: the flag maps, the index's address and key, and an object per valid id. */
export function coerceExtensions(v: unknown): ExtensionsSettings {
  if (!isObject(v)) return {}
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(v)) {
    if (key === 'enabled' || key === 'agentTools') {
      const f = flags(value)
      if (Object.keys(f).length) out[key] = f
    } else if (key === 'decorations') {
      const f = setFlags(value)
      if (Object.keys(f).length) out[key] = f
    } else if (ID.test(key) && isObject(value)) out[key] = value
  }
  const index = indexSettings(v)
  if (index.url) out.indexUrl = index.url
  if (index.publicKey) out.indexPublicKey = index.publicKey
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

/** Whether a decoration set is on: the user's choice, else the manifest's default. */
export function decorationSetEnabled(s: ExtensionsSettings, set: string, byDefault: boolean): boolean {
  return setFlags(s.decorations)[set] ?? byDefault
}

/** The section with a decoration set turned on or off; the manifest's default removes the entry. */
export function withDecorationSet(s: ExtensionsSettings, set: string, on: boolean, byDefault: boolean): ExtensionsSettings {
  const rest = Object.fromEntries(Object.entries(setFlags(s.decorations)).filter(([k]) => k !== set))
  const next = on === byDefault ? rest : { ...rest, [set]: on }
  const out: Record<string, unknown> = { ...s }
  if (Object.keys(next).length) out.decorations = next
  else delete out.decorations
  return out
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

/** Built-in extensions switched off (lib/featureToggles.ts; Go: internal/feature/toggles.go). */
export const featureToggleSettings = defineSection('features', DEFAULT_TOGGLES, coerceToggles)
