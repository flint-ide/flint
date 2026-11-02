// Built-in extensions (optional feature modules) switched on or off: settings.json's
//
//   "features": { "enabled": { "<feature id>": false } }
//
// (Go: internal/feature/toggles.go). Go reads it once at launch and builds only the
// features that are on, so a change applies after a restart; the frontend installs the
// halves of the features Go reports running (AppAPI.Features). Framework-free.
import type { FeatureModule } from './features'

export interface FeatureToggles {
  enabled: Readonly<Record<string, boolean>>
}

export const DEFAULT_TOGGLES: FeatureToggles = Object.freeze({ enabled: Object.freeze({}) })

/** A feature id (Go: feature.idPattern). */
const ID = /^[a-z][a-z0-9-]{0,39}$/

/** Keeps what Go keeps: boolean flags of valid ids. */
export function coerceToggles(v: unknown): FeatureToggles {
  const o = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {}
  const e = typeof o.enabled === 'object' && o.enabled !== null && !Array.isArray(o.enabled) ? (o.enabled as Record<string, unknown>) : {}
  return { enabled: Object.fromEntries(Object.entries(e).filter(([id, on]) => ID.test(id) && typeof on === 'boolean')) as Record<string, boolean> }
}

/** Whether the settings switch the module on: required modules always, optional ones unless off. */
export function featureOn(t: FeatureToggles, m: Pick<FeatureModule, 'id' | 'optional'>): boolean {
  return !m.optional || t.enabled[m.id] !== false
}

/** The toggles with one feature switched; on removes the entry (the default). */
export function withFeature(t: FeatureToggles, id: string, on: boolean): FeatureToggles {
  const rest = Object.fromEntries(Object.entries(t.enabled).filter(([k]) => k !== id))
  return { enabled: on ? rest : { ...rest, [id]: false } }
}

/**
 * The ids to install before Go answers: what it ran last time (cached), else what the
 * settings ask for. Required modules are always in, whatever the cache says.
 */
export function initialActive(mods: readonly FeatureModule[], cached: readonly string[] | null, t: FeatureToggles): Set<string> {
  const ids = mods.filter((m) => (cached ? !m.optional || cached.includes(m.id) : featureOn(t, m))).map((m) => m.id)
  return new Set(ids)
}

/** Optional features whose switch differs from what runs: they change at the next restart. */
export function pendingRestart(mods: readonly FeatureModule[], active: ReadonlySet<string>, t: FeatureToggles): string[] {
  return mods.filter((m) => m.optional && featureOn(t, m) !== active.has(m.id)).map((m) => m.id)
}
