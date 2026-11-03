// Debuggers and test runners added by extensions (manifest
// contributes.debuggers / testRunners), by the language of a file. Go
// (internal/dap, internal/testrun) owns the real lists and refuses what this
// side would offer by mistake; here they only decide which menu items and
// commands to show. Flint's own tools win for their languages: Go debugging,
// Go and JavaScript/TypeScript tests.
import { extensionLanguageFor } from './extensionLanguages'

export interface ToolSpec {
  readonly label: string
  readonly languages?: readonly string[] | null
}

/** An extension as far as its tools go (host.Info). */
export interface ToolSource {
  readonly id: string
  readonly enabled: boolean
  readonly state: string
  readonly debuggers?: readonly ToolSpec[] | null
  readonly testRunners?: readonly ToolSpec[] | null
}

/** Flint's own languages by file extension (internal/lsp/languages.go). */
const builtinLanguages: Readonly<Record<string, string>> = {
  '.go': 'go',
  '.ts': 'typescript', '.tsx': 'typescript', '.mts': 'typescript', '.cts': 'typescript',
  '.js': 'typescript', '.jsx': 'typescript', '.mjs': 'typescript', '.cjs': 'typescript',
  '.py': 'python', '.pyi': 'python',
  '.rs': 'rust',
}

const builtinDebug = ['go']
const builtinTests = ['go', 'typescript']

/** The language id of a file: Flint's own first, then an extension's. */
export function fileLanguage(path: string): string | undefined {
  const base = path.slice(path.lastIndexOf('/') + 1)
  const dot = base.lastIndexOf('.')
  const builtin = dot > 0 ? builtinLanguages[base.slice(dot).toLowerCase()] : undefined
  return builtin ?? extensionLanguageFor(path)?.id
}

let debuggers: ReadonlyMap<string, ToolSpec> = new Map()
let testRunners: ReadonlyMap<string, ToolSpec> = new Map()

/** The first tool listed for each language, Flint's own languages left out. */
function byLanguage(tools: readonly ToolSpec[], builtin: readonly string[]): ReadonlyMap<string, ToolSpec> {
  const out = new Map<string, ToolSpec>()
  for (const t of tools) {
    for (const l of t.languages ?? []) {
      if (!out.has(l) && !builtin.includes(l)) out.set(l, t)
    }
  }
  return out
}

/** Replaces the tools from the extensions (all of them; disabled and broken ones are skipped). */
export function setExtensionTools(list: readonly ToolSource[]): void {
  const active = list.filter((i) => i.enabled && i.state !== 'invalid')
  debuggers = byLanguage(active.flatMap((i) => i.debuggers ?? []), builtinDebug)
  testRunners = byLanguage(active.flatMap((i) => i.testRunners ?? []), builtinTests)
}

/** The extension debugger for a file, if its language has one. */
export function extensionDebuggerFor(path: string): ToolSpec | undefined {
  const lang = fileLanguage(path)
  return lang ? debuggers.get(lang) : undefined
}

/** The extension test runner for a file, if its language has one. */
export function extensionTestRunnerFor(path: string): ToolSpec | undefined {
  const lang = fileLanguage(path)
  return lang ? testRunners.get(lang) : undefined
}
