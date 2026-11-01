// Inline suggestions (ghost text): the pure parts — when to ask the model,
// what code around the cursor it sees, and how a shown suggestion follows
// typing or is accepted. lib/inlineCompletion.ts wires them into CodeMirror;
// Go (internal/completions) sends the request.
import type { Text } from '@codemirror/state'

/** Characters sent at most on each side of the cursor (Go refuses > 64 KB). */
export const MAX_CONTEXT_CHARS = 20_000

/** A suggestion shown at pos. */
export interface Ghost {
  pos: number
  text: string
}

export interface TriggerInput {
  /** The editor's selection ranges. */
  ranges: readonly { from: number; to: number }[]
  /** userEvent of the transaction that changed the document ('input.type', 'input.paste', 'delete.backward'…). */
  userEvent: string | undefined
  /** The cursor line's text and the cursor's column in it. */
  lineText: string
  column: number
}

// What may follow the cursor on its line: nothing, or closing characters a
// suggestion can sit before (")", "]", "}", quotes, ";", ",", ":").
const AFTER_CURSOR_OK = /^[\s)\]}"'`;,:]*$/

/**
 * Whether typing that just happened should ask for a suggestion: one cursor,
 * no selection, the change was typing (not a paste, undo or deletion), and
 * the cursor is not in the middle of a word.
 */
export function shouldSuggest(i: TriggerInput): boolean {
  if (i.ranges.length !== 1 || i.ranges[0].from !== i.ranges[0].to) return false
  const ev = i.userEvent ?? ''
  if (ev !== 'input' && ev !== 'input.type' && !ev.startsWith('input.type.')) return false
  return AFTER_CURSOR_OK.test(i.lineText.slice(i.column))
}

/**
 * The code the model sees: up to `lines` lines before the cursor (with the
 * cursor line's start) and after it, each cut to MAX_CONTEXT_CHARS keeping
 * what is nearest the cursor.
 */
export function completionContext(doc: Text, pos: number, lines: number, maxChars = MAX_CONTEXT_CHARS): { prefix: string; suffix: string } {
  const n = doc.lineAt(pos).number
  const start = doc.line(Math.max(1, n - lines)).from
  const end = doc.line(Math.min(doc.lines, n + lines)).to
  let prefix = doc.sliceString(start, pos)
  let suffix = doc.sliceString(pos, end)
  if (prefix.length > maxChars) prefix = prefix.slice(prefix.length - maxChars)
  if (suffix.length > maxChars) suffix = suffix.slice(0, maxChars)
  return { prefix, suffix }
}

/**
 * The suggestion to show, or null: blank answers are dropped, and a
 * single-line suggestion that ends with what already follows the cursor
 * (the model repeating ")" or "}") loses that ending.
 */
export function cleanSuggestion(text: string, restOfLine: string): string | null {
  if (!text.trim()) return null
  const rest = restOfLine.trim()
  if (rest && !text.includes('\n') && text.trimEnd().endsWith(rest)) {
    const cut = text.trimEnd().slice(0, -rest.length)
    return cut.trim() ? cut : null
  }
  return text
}

/**
 * The suggestion after the user typed `inserted` at `at`: typing its next
 * characters keeps the rest shown; anything else dismisses it (null).
 */
export function advanceGhost(g: Ghost, at: number, inserted: string): Ghost | null {
  if (at !== g.pos || !inserted || !g.text.startsWith(inserted)) return null
  const text = g.text.slice(inserted.length)
  return text ? { pos: g.pos + inserted.length, text } : null
}

/** The change that accepts the suggestion and where the cursor goes. */
export function acceptGhost(g: Ghost, lineBreak = '\n'): { from: number; insert: string; cursor: number } {
  const insert = g.text.split('\n').join(lineBreak)
  // A line break is one position in the document, whatever its separator.
  return { from: g.pos, insert, cursor: g.pos + g.text.length }
}
