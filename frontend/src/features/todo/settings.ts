// The "todoExclude" key of settings.json (Go: internal/features/todo/module.go).
import { validExclude } from '../../lib/settings'
import { defineSection } from '../../lib/settingsSections'

declare module '../../lib/settingsSections' {
  interface FeatureSettings {
    /** Extra folders / globs the TODO list skips, gitignore-style. */
    todoExclude: string[]
  }
}

export function coerceTodoExclude(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((p): p is string => typeof p === 'string' && validExclude(p)) : []
}

export const todoSettings = defineSection('todoExclude', Object.freeze([]) as unknown as string[], coerceTodoExclude)
