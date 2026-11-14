// Pure helpers for language-server answers: turning LSP text edits into editor
// changes, naming symbol kinds, filtering an outline and splitting a signature
// around its active parameter. No DOM and no bindings, so they run under node:test.
import type { Text } from '@codemirror/state'

export interface LspPosition {
  line: number
  character: number
}

export interface LspRange {
  start: LspPosition
  end: LspPosition
}

export interface LspTextEdit {
  range: LspRange
  newText: string
}

/** CodeMirror offset → LSP position (0-based line, UTF-16 character). */
export function toLsp(doc: Text, pos: number): LspPosition {
  const line = doc.lineAt(pos)
  return { line: line.number - 1, character: pos - line.from }
}

/** LSP position → CodeMirror offset, clamped to the document. */
export function fromLsp(doc: Text, p: LspPosition): number {
  if (p.line >= doc.lines) return doc.length
  const line = doc.line(Math.max(1, p.line + 1))
  return Math.min(line.from + Math.max(0, p.character), line.to)
}

export interface DocChange {
  from: number
  to: number
  insert: string
}

/**
 * LSP edits as CodeMirror changes: all positions refer to `doc` as it is now, which
 * is what one dispatch of `changes` expects. Servers send \n or \r\n; the editor
 * keeps lines apart itself, so both become \n. Edits that change nothing are dropped
 * (a formatter that returns the whole file must not make a clean buffer dirty).
 */
export function editsToChanges(doc: Text, edits: readonly LspTextEdit[]): DocChange[] {
  const changes: DocChange[] = []
  for (const e of edits) {
    const from = fromLsp(doc, e.range.start)
    const to = Math.max(from, fromLsp(doc, e.range.end))
    const insert = e.newText.replace(/\r\n?/g, '\n')
    if (doc.sliceString(from, to, '\n') === insert) continue
    changes.push({ from, to, insert })
  }
  return changes
}

/** "3 edits in 2 files", for the note after a rename or a code action. */
export function editSummary(files: readonly { path: string; edits: readonly unknown[] }[]): string {
  const paths = new Set(files.map((f) => f.path))
  const edits = files.reduce((n, f) => n + f.edits.length, 0)
  return `${edits} ${edits === 1 ? 'edit' : 'edits'} in ${paths.size} ${paths.size === 1 ? 'file' : 'files'}`
}

// LSP SymbolKind → a short word for outline rows.
const SYMBOL_KINDS: Record<number, string> = {
  1: 'file', 2: 'module', 3: 'namespace', 4: 'package', 5: 'class', 6: 'method', 7: 'property', 8: 'field',
  9: 'constructor', 10: 'enum', 11: 'interface', 12: 'function', 13: 'variable', 14: 'constant', 15: 'string',
  16: 'number', 17: 'boolean', 18: 'array', 19: 'object', 20: 'key', 21: 'null', 22: 'enum member', 23: 'struct',
  24: 'event', 25: 'operator', 26: 'type parameter',
}

export function symbolKindName(kind: number): string {
  return SYMBOL_KINDS[kind] ?? 'symbol'
}

export interface OutlineSymbol {
  name: string
  container: string
  depth: number
}

/**
 * Outline rows matching a query, in document order. Every character of the query
 * must appear in order in the name (case-insensitive); names that contain the query
 * as one piece come first only through the caller's own ranking, so the outline
 * keeps reading top to bottom. An empty query keeps everything.
 */
export function filterSymbols<T extends OutlineSymbol>(symbols: readonly T[], query: string): T[] {
  const q = query.trim().toLowerCase()
  if (!q) return symbols.slice()
  return symbols.filter((s) => subsequence(s.name.toLowerCase(), q))
}

function subsequence(text: string, query: string): boolean {
  let i = 0
  for (const ch of text) {
    if (ch === query[i]) i++
    if (i === query.length) return true
  }
  return false
}

export interface SignatureParts {
  before: string
  active: string
  after: string
}

/** A signature label split around its active parameter (offsets are UTF-16, like JS strings). */
export function signatureParts(label: string, parameters: readonly { start: number; end: number }[], active: number): SignatureParts {
  const p = parameters[active]
  if (!p || p.end <= p.start || p.end > label.length) return { before: label, active: '', after: '' }
  return { before: label.slice(0, p.start), active: label.slice(p.start, p.end), after: label.slice(p.end) }
}

/** Code actions grouped for the menu: quick fixes first, then refactorings, then source actions. */
export function sortCodeActions<T extends { kind: string; preferred: boolean }>(actions: readonly T[]): T[] {
  const rank = (a: T) => (a.preferred ? 0 : a.kind.startsWith('quickfix') ? 1 : a.kind.startsWith('refactor') ? 2 : a.kind.startsWith('source') ? 3 : 2)
  return actions.map((a, i) => ({ a, i })).sort((x, y) => rank(x.a) - rank(y.a) || x.i - y.i).map((x) => x.a)
}
