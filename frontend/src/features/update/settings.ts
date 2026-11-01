// The "checkForUpdates" key of settings.json (Go: internal/features/update/module.go).
import { defineSection } from '../../lib/settingsSections'

declare module '../../lib/settingsSections' {
  interface FeatureSettings {
    /** Ask GitHub once a day whether a newer Flint release exists. */
    checkForUpdates: boolean
  }
}

export const updateSettings = defineSection('checkForUpdates', true, (v) => (typeof v === 'boolean' ? v : true))
