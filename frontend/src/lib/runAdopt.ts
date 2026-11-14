// A run Flint's backend started by itself, for an agent's run_tests or
// run_configuration tool (Go: RunAdopt in api_mcp_actions.go). The event
// arrives before the run's first output, and the run tab shows it like a run
// started from the UI. The payload is checked here, so a malformed event
// opens nothing.

import type { TestRequest } from './runTree'

export interface AdoptedConfig {
  id: string
  name: string
  type: string
  cwd?: string
}

export type RunAdoption =
  | { kind: 'tests'; id: string; title: string; root: string; request: TestRequest }
  | { kind: 'config'; id: string; title: string; root: string; config: AdoptedConfig }

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const text = (v: unknown): v is string => typeof v === 'string' && v !== ''
const strings = (v: unknown): v is string[] => Array.isArray(v) && v.every((x) => typeof x === 'string')

/** The adoption an event describes, or null when it isn't one. */
export function parseRunAdoption(v: unknown): RunAdoption | null {
  if (!isObject(v) || !text(v.id) || !text(v.root) || typeof v.title !== 'string') return null
  const base = { id: v.id, title: v.title, root: v.root }
  if (v.kind === 'tests') {
    const r = v.request
    if (!isObject(r) || (r.lang !== 'go' && r.lang !== 'js' && r.lang !== 'ext') || !strings(r.dirs) || r.dirs.length === 0) return null
    const request: TestRequest = { lang: r.lang, dirs: r.dirs }
    if (text(r.file)) request.file = r.file
    if (strings(r.tests) && r.tests.length > 0) request.tests = r.tests
    return { kind: 'tests', ...base, request }
  }
  if (v.kind === 'config') {
    const c = v.config
    if (!isObject(c) || !text(c.id) || !text(c.name) || !text(c.type)) return null
    return { kind: 'config', ...base, config: c as unknown as AdoptedConfig }
  }
  return null
}
