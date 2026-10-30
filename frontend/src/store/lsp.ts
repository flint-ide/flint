import { createStore } from '../lib/store'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { lsp } from '../../wailsjs/go/models'

// Event payload, not part of a bound method signature, so Wails doesn't generate it.
export interface Diagnostic {
  range: lsp.Range
  severity: number // 1 error, 2 warning, 3 info, 4 hint
  message: string
  source: string
}

/** Language-server state per language, for the status bar. */
interface LspState {
  statuses: Record<string, lsp.Status>
  /** Usages popup content (null = closed). */
  usages: { symbol: string; locations: lsp.Location[] } | null
  showUsages: (symbol: string, locations: lsp.Location[]) => void
  closeUsages: () => void
}

export const useLsp = createStore<LspState>((set) => ({
  statuses: {},
  usages: null,
  showUsages: (symbol, locations) => set({ usages: { symbol, locations } }),
  closeUsages: () => set({ usages: null }),
}))

// Diagnostics live outside React state: editors subscribe per file and apply them
// directly, so a chatty server doesn't re-render the whole app.
const diagnostics = new Map<string, Diagnostic[]>()
const listeners = new Map<string, Set<() => void>>()

export function currentDiagnostics(path: string): Diagnostic[] {
  return diagnostics.get(path) ?? []
}

export function onDiagnostics(path: string, fn: () => void): () => void {
  const set = listeners.get(path) ?? new Set()
  set.add(fn)
  listeners.set(path, set)
  return () => {
    set.delete(fn)
    if (set.size === 0) listeners.delete(path)
  }
}

/** Counts errors and warnings across all files (status bar). */
export function diagnosticTotals(): { errors: number; warnings: number } {
  let errors = 0
  let warnings = 0
  for (const list of diagnostics.values()) {
    for (const d of list) {
      if (d.severity === 1) errors++
      else if (d.severity === 2) warnings++
    }
  }
  return { errors, warnings }
}

let subscribed = false
export function subscribeLspEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('lsp:diagnostics', (msg: { path: string; diagnostics: Diagnostic[] }) => {
    if (msg.diagnostics.length) diagnostics.set(msg.path, msg.diagnostics)
    else diagnostics.delete(msg.path)
    listeners.get(msg.path)?.forEach((fn) => fn())
  })
  EventsOn('lsp:status', (st: lsp.Status) => {
    useLsp.setState((s) => ({ statuses: { ...s.statuses, [st.language]: st } }))
  })
}
