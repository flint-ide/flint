// Edit with Agent (⌘I): the pure parts — what is edited, the bounded context
// the agent sees, and whether its answer may still be applied. The widget and
// the run live in lib/inlineEditWidget.ts; Go builds the prompt
// (internal/agent/inlineedit.go) and enforces its own limits.
import type { ChangeDesc, Text } from '@codemirror/state'

/** Characters of context sent at most on each side of the selection (Go refuses > 32 KB). */
export const MAX_CONTEXT_CHARS = 10_000
/** Largest selection Flint sends (Go refuses > 64 KB). */
export const MAX_SELECTION_CHARS = 20_000

export interface Range {
  from: number
  to: number
}

/** What the request is about: the doc offsets and the text there when it was asked. */
export interface Pending extends Range {
  original: string
}

export interface EditRequest {
  path: string
  language: string
  instruction: string
  before: string
  selection: string
  after: string
}

/** The range to edit: the selection, or the cursor's whole line when nothing is selected. */
export function editRange(doc: Text, sel: Range): Range {
  if (sel.from !== sel.to) return { from: Math.min(sel.from, sel.to), to: Math.max(sel.from, sel.to) }
  const line = doc.lineAt(sel.from)
  return { from: line.from, to: line.to }
}

/**
 * The lines around range: up to `lines` lines on each side, cut to
 * MAX_CONTEXT_CHARS at a line boundary (the nearest lines are kept).
 */
export function editContext(doc: Text, range: Range, lines: number, maxChars = MAX_CONTEXT_CHARS): { before: string; after: string } {
  if (lines <= 0) return { before: '', after: '' }
  const first = doc.lineAt(range.from).number
  const last = doc.lineAt(range.to).number
  const start = doc.line(Math.max(1, first - lines)).from
  const end = doc.line(Math.min(doc.lines, last + lines)).to
  let before = doc.sliceString(start, range.from)
  let after = doc.sliceString(range.to, end)
  if (before.length > maxChars) {
    const cut = before.slice(before.length - maxChars)
    const nl = cut.indexOf('\n')
    before = nl >= 0 ? cut.slice(nl + 1) : ''
  }
  if (after.length > maxChars) {
    const cut = after.slice(0, maxChars)
    const nl = cut.lastIndexOf('\n')
    after = nl >= 0 ? cut.slice(0, nl + 1) : ''
  }
  return { before, after }
}

/** The language name the agent is told: the file's extension, or its name (Dockerfile, Makefile). */
export function languageOf(path: string): string {
  const name = path.split('/').pop() ?? ''
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : name
}

/** The request for pending in doc, or the reason it can't be sent. */
export function buildEditRequest(
  path: string,
  doc: Text,
  pending: Pending,
  instruction: string,
  contextLines: number,
): EditRequest | { error: string } {
  if (!instruction.trim()) return { error: 'Type what to change.' }
  if (pending.original.length > MAX_SELECTION_CHARS) {
    return { error: `The selection is too large for an inline edit (over ${MAX_SELECTION_CHARS.toLocaleString()} characters).` }
  }
  const { before, after } = editContext(doc, pending, contextLines)
  return { path, language: languageOf(path), instruction: instruction.trim(), before, selection: pending.original, after }
}

/**
 * Follows the range through edits made while the agent works: text typed right
 * before or after it stays outside.
 */
export function mapPending(p: Pending, changes: ChangeDesc): Pending {
  const from = changes.mapPos(p.from, 1)
  const to = Math.max(from, changes.mapPos(p.to, -1))
  return from === p.from && to === p.to ? p : { ...p, from, to }
}

/** Whether the answer may replace the range: it still holds the text the agent was given. */
export function canApply(doc: Text, p: Pending): boolean {
  return p.to <= doc.length && doc.sliceString(p.from, p.to) === p.original
}

/**
 * The agent's answer shaped like the text it replaces: \n line breaks, the
 * original's indentation when the answer came back without any, and the
 * original's trailing line break kept (agents drop both).
 */
export function fitReplacement(original: string, reply: string): string {
  let text = reply.replace(/\r\n?/g, '\n')
  const indent = /^[ \t]*/.exec(original)?.[0] ?? ''
  if (indent && text.trim() && !/^[ \t]/.test(text)) {
    text = text.split('\n').map((l) => (l.trim() ? indent + l : l)).join('\n')
  }
  if (original.endsWith('\n') && text !== '' && !text.endsWith('\n')) text += '\n'
  if (!original.endsWith('\n') && text.endsWith('\n') && !original.includes('\n')) text = text.replace(/\n+$/, '')
  return text
}
