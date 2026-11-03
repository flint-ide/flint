// Pure part of the extensions repository view: how each repository entry
// relates to what is installed. No React, no bindings: tested with node:test.

export interface CatalogEntryLike {
  readonly id: string
  readonly version: string
  readonly error: string
}

export interface InstalledLike {
  readonly id: string
  readonly version: string
}

/**
 * available: not installed; installed: same version; update: the repository's is newer;
 * newer: a newer version (e.g. a local build) is installed; broken: can't be installed.
 */
export type CatalogStatus = 'available' | 'installed' | 'update' | 'newer' | 'broken'

export interface CatalogRow<E extends CatalogEntryLike> {
  readonly entry: E
  readonly status: CatalogStatus
  /** The installed version ("" when not installed). */
  readonly installedVersion: string
}

/** Compares dotted versions numerically ("1.10.0" > "1.9"); a pre-release or build suffix is ignored. */
export function compareVersions(a: string, b: string): number {
  const parts = (v: string) => v.replace(/^v/, '').split(/[-+]/)[0].split('.').map((p) => Number.parseInt(p, 10) || 0)
  const x = parts(a)
  const y = parts(b)
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0)
    if (d !== 0) return Math.sign(d)
  }
  return 0
}

export function catalogRows<E extends CatalogEntryLike>(entries: readonly E[], installed: readonly InstalledLike[]): readonly CatalogRow<E>[] {
  const versions = new Map(installed.map((i) => [i.id, i.version]))
  return entries.map((entry) => {
    const have = versions.get(entry.id)
    const installedVersion = have ?? ''
    if (entry.error) return { entry, status: 'broken', installedVersion }
    if (have === undefined) return { entry, status: 'available', installedVersion }
    const order = compareVersions(entry.version, have)
    return { entry, status: order > 0 ? 'update' : order < 0 ? 'newer' : 'installed', installedVersion }
  })
}
