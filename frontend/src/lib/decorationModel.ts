// Editor decorations as data (docs/design/extensions.md, "Editor decorations"):
// what a decoration set publishes for a document, how its positions land in
// the document as it is now, and how a quick fix's edits become one change.
// Lines are 1-based; columns are UTF-16 code units from the line's start, as
// in LSP and in CodeMirror. Framework-free: lib/editorDecorations.ts draws
// them.

export type DecorationKind = 'diagnostic' | 'gutter' | 'inline' | 'highlight'
export type Severity = 'info' | 'ok' | 'warning' | 'error'

/** One of the contributor's own commands, with its arguments. */
export interface CommandRef {
  id: string
  args?: unknown
}

/** A quick fix: asks the contributor for edits, or runs `command`. */
export interface DecorationAction {
  id: string
  label: string
  command?: CommandRef
}

export interface EditorDecoration {
  kind: DecorationKind
  /** The contributor's own name for it, sent back with a quick fix. */
  id?: string
  line: number
  /** Last line of the range (= line for one line). */
  endLine: number
  from: number
  /** End column on endLine; -1 is the end of that line. */
  to: number
  severity: Severity
  /** Hover text (a diagnostic's message). */
  message?: string
  /** An inline decoration's text after the line. */
  text?: string
  /** A gutter marker's glyph (GUTTER_ICONS); unknown names show a dot. */
  icon?: string
  /** Runs when an inline decoration is clicked. */
  command?: CommandRef
  actions?: readonly DecorationAction[]
}

/** Replaces the text from (line, from) to (endLine, to) with text. */
export interface TextEdit {
  line: number
  from: number
  endLine: number
  to: number
  text: string
}

/** The quick fix the user chose, and where its decoration is now. */
export interface QuickFixRequest {
  set: string
  path: string
  /** The document's version when the user chose: edits apply only to it. */
  version: number
  action: string
  /** The decoration's id, when it has one. */
  decoration: string
  line: number
  endLine: number
  from: number
  to: number
}

/** The part of a CodeMirror Text these functions read. */
export interface DocLike {
  readonly lines: number
  line(n: number): { from: number; to: number }
}

/** Limits, the same as Go's (internal/extensions/host/decorations.go). */
export const MAX_DECORATIONS = 5000
export const MAX_INLINE_TEXT = 200
export const MAX_MESSAGE = 2000
export const MAX_ACTIONS = 10
export const MAX_EDITS = 1000

const KINDS: readonly string[] = ['diagnostic', 'gutter', 'inline', 'highlight']
const SEVERITIES: readonly string[] = ['info', 'ok', 'warning', 'error']

/** Glyphs a gutter marker may show, by icon name. */
export const GUTTER_ICONS: Readonly<Record<string, string>> = { dot: '●', check: '✓', cross: '✕', alert: '!', info: 'i', arrow: '→' }

/** Severities from most to least urgent: the one a line's gutter marker shows. */
export const SEVERITY_RANK: Readonly<Record<Severity, number>> = { error: 3, warning: 2, info: 1, ok: 0 }

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const int = (v: unknown, fallback: number): number => (typeof v === 'number' && Number.isFinite(v) ? Math.trunc(v) : fallback)
const text = (v: unknown, limit: number): string => (typeof v === 'string' ? (v.length > limit ? v.slice(0, limit) : v) : '')

function coerceCommand(v: unknown): CommandRef | undefined {
  if (!isObject(v) || typeof v.id !== 'string' || !v.id) return undefined
  return v.args === undefined || v.args === null ? { id: v.id } : { id: v.id, args: v.args }
}

function coerceActions(v: unknown): DecorationAction[] {
  if (!Array.isArray(v)) return []
  const out: DecorationAction[] = []
  for (const a of v) {
    if (out.length === MAX_ACTIONS) break
    if (!isObject(a)) continue
    const id = text(a.id, 64)
    const label = text(a.label, 60)
    if (!id || !label) continue
    const command = coerceCommand(a.command)
    out.push(command ? { id, label, command } : { id, label })
  }
  return out
}

/**
 * What a publish carries, as decorations: unknown kinds and entries without
 * a line are dropped, everything else gets a valid value, at most
 * MAX_DECORATIONS. Go checked an extension's already; this keeps the editor
 * safe whoever publishes.
 */
export function coerceDecorations(raw: unknown): EditorDecoration[] {
  if (!Array.isArray(raw)) return []
  const out: EditorDecoration[] = []
  for (const v of raw) {
    if (out.length === MAX_DECORATIONS) break
    if (!isObject(v) || typeof v.kind !== 'string' || !KINDS.includes(v.kind)) continue
    const line = int(v.line, 0)
    if (line < 1) continue
    const kind = v.kind as DecorationKind
    const endLine = Math.max(int(v.endLine, line), line)
    const from = Math.max(int(v.from, 0), 0)
    let to = int(v.to, -1)
    if (to < -1) to = -1
    if (to >= 0 && endLine === line && to < from) to = from
    const d: EditorDecoration = {
      kind,
      line,
      endLine,
      from,
      to,
      severity: typeof v.severity === 'string' && SEVERITIES.includes(v.severity) ? (v.severity as Severity) : 'info',
    }
    const id = text(v.id, 256)
    if (id) d.id = id
    const message = text(v.message, MAX_MESSAGE) || text(v.hover, MAX_MESSAGE)
    if (message) d.message = message
    if (kind === 'diagnostic' && !message) continue
    if (kind === 'inline') {
      d.text = text(v.text, MAX_INLINE_TEXT).replace(/\s+/g, ' ')
      if (!d.text.trim()) continue
      const command = coerceCommand(v.command)
      if (command) d.command = command
    }
    if (kind === 'gutter' && typeof v.icon === 'string' && Object.hasOwn(GUTTER_ICONS, v.icon)) d.icon = v.icon
    const actions = coerceActions(v.actions)
    if (actions.length) d.actions = actions
    out.push(d)
  }
  return out
}

/**
 * Whether a publish computed for document version `published` (0: the
 * publisher didn't say) may be drawn in a document now at `current`: only
 * when it was computed for exactly this text.
 */
export function publishIsCurrent(published: number, current: number): boolean {
  return published === 0 || published === current
}

/**
 * Where a decoration lands in doc, as offsets: lines and columns beyond the
 * document are clamped to it, so a late or sloppy publish never throws. A
 * line-level decoration (no columns) spans its lines from the first one's
 * start to the last one's end.
 */
export function decorationRange(doc: DocLike, d: Pick<EditorDecoration, 'line' | 'endLine' | 'from' | 'to'>): { from: number; to: number } {
  const first = doc.line(clamp(d.line, 1, doc.lines))
  const last = doc.line(clamp(d.endLine, clamp(d.line, 1, doc.lines), doc.lines))
  const from = Math.min(first.from + Math.max(d.from, 0), first.to)
  const to = d.to < 0 ? last.to : Math.min(last.from + d.to, last.to)
  return { from, to: Math.max(to, from) }
}

/** A decoration without columns: it is about whole lines. */
export function isLineLevel(d: Pick<EditorDecoration, 'from' | 'to'>): boolean {
  return d.from === 0 && d.to < 0
}

/** The line and columns of the offsets from..to in doc, as a quick fix request names them. */
export function positionOf(doc: DocLike & { lineAt(pos: number): { number: number; from: number } }, from: number, to: number) {
  const first = doc.lineAt(from)
  const last = doc.lineAt(to)
  return { line: first.number, from: from - first.from, endLine: last.number, to: to - last.from }
}

export interface Change {
  from: number
  to: number
  insert: string
}

/**
 * A quick fix's edits as changes to doc, sorted, or the reason they can't be
 * applied. Unlike decorations, edits are never clamped: a position outside
 * the document means the edits were made for another text, and overlapping
 * edits have no single meaning, so nothing is applied then.
 */
export function editChanges(doc: DocLike, edits: readonly TextEdit[]): Change[] | string {
  if (edits.length > MAX_EDITS) return `more than ${MAX_EDITS} edits`
  const out: Change[] = []
  for (const e of edits) {
    const endLine = e.endLine || e.line
    if (!Number.isInteger(e.line) || !Number.isInteger(endLine) || !Number.isInteger(e.from) || !Number.isInteger(e.to)) return 'an edit has no position'
    if (e.line < 1 || endLine < e.line || endLine > doc.lines || e.from < 0 || e.to < 0) return 'an edit is outside the document'
    const first = doc.line(e.line)
    const last = doc.line(endLine)
    const from = first.from + e.from
    const to = last.from + e.to
    if (from > first.to || to > last.to || to < from) return 'an edit is outside the document'
    if (typeof e.text !== 'string') return 'an edit has no text'
    out.push({ from, to, insert: e.text.replace(/\r\n?/g, '\n') })
  }
  out.sort((a, b) => a.from - b.from || a.to - b.to)
  for (let i = 1; i < out.length; i++) {
    if (out[i].from < out[i - 1].to) return 'two edits overlap'
  }
  return out
}

function clamp(n: number, min: number, max: number): number {
  return Math.min(Math.max(n, min), max)
}
