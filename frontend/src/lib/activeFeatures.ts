// Which feature modules are installed (features/install.ts keeps it): core views that
// work with or without a built-in extension (a SQL console without the database
// feature) read it here, without importing the feature list.
import { createStore } from './store'

export const useActiveFeatures = createStore<{ ids: ReadonlySet<string> }>(() => ({ ids: new Set<string>() }))

/** Whether a feature module (e.g. 'database') is installed, i.e. its backend runs. */
export function isFeatureActive(id: string): boolean {
  return useActiveFeatures.getState().ids.has(id)
}
