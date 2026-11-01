// Settings sections owned by feature modules (Go: internal/settings/section.go).
// A feature adds its key to settings.json's type by augmenting FeatureSettings
// and registers the key's default and coercion here, so lib/settings.ts never
// lists it:
//
//   declare module '../../lib/settingsSections' {
//     interface FeatureSettings { docker: DockerSettings }
//   }
//   export const dockerSection = defineSection('docker', DEFAULT_DOCKER, coerceDocker)
//
// Go validates and normalizes on save; coerce only makes untrusted JSON (the
// backend's answer, the localStorage cache) safe to read, like coerceSettings.
// Framework-free, so it is testable with node:test.

/** The keys feature modules own, by key (augmented by each feature's settings module). */
// eslint-disable-next-line @typescript-eslint/no-empty-interface, @typescript-eslint/no-empty-object-type
export interface FeatureSettings {}

export type SectionKey = keyof FeatureSettings & string

export interface SettingsSection<K extends SectionKey = SectionKey> {
  key: K
  /** Used while the key is missing or invalid. */
  defaults: FeatureSettings[K]
  /** Untrusted JSON → a valid value (the default for anything unusable). */
  coerce: (v: unknown) => FeatureSettings[K]
}

export function defineSection<K extends SectionKey>(key: K, defaults: FeatureSettings[K], coerce: (v: unknown) => FeatureSettings[K]): SettingsSection<K> {
  return { key, defaults, coerce }
}

// Registered sections by key; the type is erased here (each section is consistent with itself).
let sections = new Map<string, SettingsSection>()
const listeners = new Set<() => void>()

/**
 * Registers sections (feature installation does it before the first render).
 * Keys must be unique and must not be core keys; registering the same section
 * object again is a no-op. Listeners hear about every change.
 */
export function registerSections(list: readonly SettingsSection[], coreKeys: readonly string[]): () => void {
  const added: SettingsSection[] = []
  for (const s of list) {
    if (coreKeys.includes(s.key)) throw new Error(`settings section "${s.key}" is a core key`)
    const have = sections.get(s.key)
    if (have === s) continue
    if (have) throw new Error(`settings section "${s.key}" registered twice`)
    added.push(s)
  }
  if (added.length === 0) return () => {}
  const next = new Map(sections)
  for (const s of added) next.set(s.key, s)
  sections = next
  listeners.forEach((l) => l())
  return () => {
    const rest = new Map(sections)
    for (const s of added) if (rest.get(s.key) === s) rest.delete(s.key)
    sections = rest
    listeners.forEach((l) => l())
  }
}

/** Calls listener after sections were registered or removed. */
export function onSectionsChanged(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function sectionKeys(): string[] {
  return [...sections.keys()]
}

/** The registered sections' values from untrusted JSON (defaults for missing keys). */
export function coerceSections(v: Record<string, unknown>): FeatureSettings {
  const out: Record<string, unknown> = {}
  for (const s of sections.values()) out[s.key] = s.key in v ? s.coerce(v[s.key]) : s.defaults
  return out as unknown as FeatureSettings
}
