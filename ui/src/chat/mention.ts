/**
 * The pure core of the @-mention picker: finding the mention token under the
 * caret, replacing it, and ranking and grouping candidates. No DOM, so it is
 * unit tested (mention.test.ts).
 */
import type { ReactNode } from 'react'

/** A half-open range of UTF-16 offsets into the text. */
export type MentionRange = { readonly start: number; readonly end: number }

/**
 * The mention being typed: `start` is the trigger's offset, `end` the end of the
 * whole token (it may run past the caret), `query` the text between the trigger
 * and the caret.
 */
export type MentionToken = MentionRange & { readonly query: string }

/** A candidate for the picker. */
export type MentionItem = {
  readonly id: string
  /** What the row shows and, by default, what is inserted after the trigger. */
  readonly label: string
  /** The group header the row sits under ("Files", "Symbols", "Open tabs"). */
  readonly group: string
  /** Muted text on the right, e.g. the directory. Searched too. */
  readonly detail?: string
  /** Extra words that match the item without being shown. */
  readonly keywords?: readonly string[]
  /** A leading icon. */
  readonly icon?: ReactNode
  /** The exact text to insert instead of trigger + label. */
  readonly insert?: string
}

/** One ranked candidate: rank 0 is the best, `hit` the part of the label to emphasise. */
export type MentionMatch<T extends MentionItem = MentionItem> = {
  readonly item: T
  readonly rank: number
  readonly hit: MentionRange | null
}

/** A group of matches; `offset` is the flat index of its first match. */
export type MentionGroup<T extends MentionItem = MentionItem> = {
  readonly name: string
  readonly offset: number
  readonly matches: readonly MentionMatch<T>[]
}

const WHITESPACE = /\s/
const OPENERS = new Set(['(', '[', '{', '<', '"', "'", '`'])
// Letters (any script), combining marks, digits and path punctuation.
const QUERY_CHAR = /^[\p{L}\p{M}\p{N}/._:-]$/u
const QUERY = /^[\p{L}\p{M}\p{N}/._:-]*$/u

function isBoundary(ch: string | undefined): boolean {
  return ch === undefined || WHITESPACE.test(ch) || OPENERS.has(ch)
}

function collapsedCaret(text: string, caret: number | MentionRange): number | null {
  const sel = typeof caret === 'number' ? { start: caret, end: caret } : caret
  if (sel.start !== sel.end || !Number.isInteger(sel.start)) return null
  return sel.start >= 0 && sel.start <= text.length ? sel.start : null
}

function tokenEnd(text: string, from: number): number {
  let end = from
  while (end < text.length) {
    const ch = String.fromCodePoint(text.codePointAt(end) ?? 0)
    if (!QUERY_CHAR.test(ch)) break
    end += ch.length
  }
  return end
}

/**
 * The mention token under a collapsed caret, or null. A trigger counts only at
 * the start of the text or after whitespace or an opening bracket/quote (so an
 * e-mail address never opens the picker); the query runs from it to the caret
 * and may hold letters, digits and `/ . - _ :` but no whitespace. `trigger` must
 * be one non-space character. Pass `{ start, end }` from a textarea's
 * selectionStart/selectionEnd; a non-collapsed selection is never a mention.
 */
export function mentionQueryAt(text: string, caret: number | MentionRange, trigger = '@'): MentionToken | null {
  if (trigger.length !== 1 || WHITESPACE.test(trigger)) return null
  const pos = collapsedCaret(text, caret)
  if (pos === null) return null
  let i = pos - 1
  while (i >= 0 && text[i] !== trigger && !WHITESPACE.test(text[i])) i -= 1
  if (i < 0 || text[i] !== trigger || !isBoundary(text[i - 1])) return null
  const query = text.slice(i + 1, pos)
  if (!QUERY.test(query)) return null
  return { start: i, end: tokenEnd(text, pos), query }
}

function clampRange(text: string, range: MentionRange): MentionRange {
  const clamp = (n: number) => Math.min(Math.max(Number.isFinite(n) ? Math.trunc(n) : 0, 0), text.length)
  const a = clamp(range.start)
  const b = clamp(range.end)
  return a <= b ? { start: a, end: b } : { start: b, end: a }
}

/**
 * Replaces `range` with `insert` plus one space and returns the new text and the
 * caret after that space. A space that already follows is reused, not doubled.
 * An empty `insert` removes the token (and one of two spaces it would leave).
 */
export function applyMention(text: string, range: MentionRange, insert: string): { text: string; caret: number } {
  const { start, end } = clampRange(text, range)
  const before = text.slice(0, start)
  const after = text.slice(end)
  if (insert === '') {
    const dropSpace = after.startsWith(' ') && (before === '' || WHITESPACE.test(before[before.length - 1]))
    return { text: before + (dropSpace ? after.slice(1) : after), caret: start }
  }
  if (after.startsWith(' ')) return { text: before + insert + after, caret: start + insert.length + 1 }
  return { text: `${before}${insert} ${after}`, caret: start + insert.length + 1 }
}

// Letters that carry no decomposable mark but that people type without one.
const BASE_LETTER: Readonly<Record<string, string>> = { ł: 'l', Ł: 'l', đ: 'd', Đ: 'd', ø: 'o', Ø: 'o' }

function foldChar(ch: string): string {
  return BASE_LETTER[ch] ?? ch.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()
}

/** Case- and diacritic-folded text, with `map[i]` the original offset of folded char i. */
function fold(text: string): { readonly text: string; readonly map: readonly number[] } {
  let folded = ''
  const map: number[] = []
  let offset = 0
  for (const ch of text) {
    const f = foldChar(ch)
    folded += f
    for (let k = 0; k < f.length; k += 1) map.push(offset)
    offset += ch.length
  }
  return { text: folded, map }
}

function toOriginal(label: string, map: readonly number[], start: number, length: number): MentionRange {
  const end = start + length
  return { start: map[start], end: end < map.length ? map[end] : label.length }
}

function segmentStart(folded: string): number {
  return Math.max(folded.lastIndexOf('/'), folded.lastIndexOf('\\')) + 1
}

/** Where the query falls in the label (folded), or -1. Prefers the last segment, then the start. */
function labelHit(folded: string, q: string): { readonly at: number; readonly rank: number } {
  const seg = segmentStart(folded)
  if (folded.startsWith(q, seg)) return { at: seg, rank: 0 }
  if (folded.startsWith(q)) return { at: 0, rank: 1 }
  const at = folded.indexOf(q)
  return { at, rank: at < 0 ? -1 : 2 }
}

/** The part of `label` to emphasise for `query`, or null. */
export function highlightRange(label: string, query: string): MentionRange | null {
  const q = fold(query).text
  if (q === '') return null
  const f = fold(label)
  const { at } = labelHit(f.text, q)
  return at < 0 ? null : toOriginal(label, f.map, at, q.length)
}

function matchItem<T extends MentionItem>(item: T, q: string, rank: boolean): MentionMatch<T> | null {
  if (q === '') return { item, rank: 0, hit: null }
  const f = fold(item.label)
  const { at, rank: r } = labelHit(f.text, q)
  const hit = at < 0 ? null : toOriginal(item.label, f.map, at, q.length)
  if (!rank) return { item, rank: 0, hit }
  if (r >= 0) return { item, rank: r, hit }
  const extra = [item.detail ?? '', ...(item.keywords ?? [])]
  return extra.some((s) => fold(s).text.includes(q)) ? { item, rank: 3, hit: null } : null
}

function normalLimit(limit: number): number {
  if (Number.isNaN(limit)) return 0
  return limit === Infinity ? Infinity : Math.max(0, Math.floor(limit))
}

/**
 * Filters, ranks and groups items for `query` (case- and diacritic-insensitive
 * on label, detail and keywords). Within a group: prefix of the label's last
 * path segment, then prefix of the label, then a substring of it, then a match in
 * detail/keywords only; ties keep the input order. Groups follow the order in
 * which they first appear in `items`; groups with no matches are left out. With
 * `rank: false` (results a search already filtered) every item is kept in its
 * order, and only the highlight is computed.
 */
export function groupMentions<T extends MentionItem>(
  items: readonly T[],
  query: string,
  limitPerGroup = Infinity,
  options: { readonly rank?: boolean } = {},
): readonly MentionGroup<T>[] {
  const limit = normalLimit(limitPerGroup)
  const q = fold(query).text
  const byGroup = new Map<string, MentionMatch<T>[]>()
  // Local arrays that never leave this function: push, so a long list stays linear.
  // Every group is registered on first sight, matching or not, so groups keep their places as the query changes.
  items.forEach((item) => {
    const found = byGroup.get(item.group) ?? []
    if (found.length === 0) byGroup.set(item.group, found)
    const m = matchItem(item, q, options.rank ?? true)
    if (m) found.push(m)
  })
  const groups: MentionGroup<T>[] = []
  let offset = 0
  byGroup.forEach((found, name) => {
    const matches = [...found].sort((a, b) => a.rank - b.rank).slice(0, limit)
    if (matches.length === 0) return
    groups.push({ name, offset, matches })
    offset += matches.length
  })
  return groups
}

/** Every item of the groups in display order (flat index = position). */
export function flatMentions<T extends MentionItem>(groups: readonly MentionGroup<T>[]): readonly T[] {
  return groups.flatMap((g) => g.matches.map((m) => m.item))
}

/** The item at a flat index across groups, or undefined. */
export function mentionAt<T extends MentionItem>(groups: readonly MentionGroup<T>[], index: number): T | undefined {
  const group = groups.find((g) => index >= g.offset && index < g.offset + g.matches.length)
  return group?.matches[index - group.offset].item
}

/** The parts of a keydown the picker looks at (a React or DOM KeyboardEvent fits). */
export type MentionKey = {
  readonly key: string
  readonly shiftKey?: boolean
  readonly ctrlKey?: boolean
  readonly altKey?: boolean
  readonly metaKey?: boolean
  readonly isComposing?: boolean
  readonly keyCode?: number
}

/** `wait`: the key would move or pick, but the rows are not for the current query yet; swallow it. */
export type MentionKeyAction = 'next' | 'prev' | 'pick' | 'close' | 'wait'

/**
 * True for a key that belongs to an IME composition, including the commit key
 * WebKit sends after compositionend (isComposing false, keyCode 229). A textarea's
 * own Enter-to-send must check this too, not only isComposing.
 */
export function isImeKey(e: MentionKey): boolean {
  return e.isComposing === true || e.keyCode === 229
}

/**
 * What a key does while the picker is open, or null to let the textarea have it.
 * Moves and picks need `hasItems`; Escape always closes. While `pending` (a search
 * runs, or the rows belong to an older query) Enter, Tab and the moves are held
 * ('wait') so a fast Enter neither sends the text nor picks a stale row. IME keys
 * are never used.
 */
export function mentionKeyAction(e: MentionKey, hasItems: boolean, pending = false): MentionKeyAction | null {
  if (isImeKey(e)) return null
  if (e.key === 'Escape') return 'close'
  if (e.altKey || e.metaKey || e.shiftKey) return null
  const action = listKey(e)
  if (action === null) return null
  if (pending) return 'wait'
  return hasItems ? action : null
}

/** The list key an unmodified (or Ctrl+N/P) press means, ignoring the list's state. */
function listKey(e: MentionKey): 'next' | 'prev' | 'pick' | null {
  if (e.ctrlKey) return e.key === 'n' ? 'next' : e.key === 'p' ? 'prev' : null
  if (e.key === 'ArrowDown') return 'next'
  if (e.key === 'ArrowUp') return 'prev'
  return e.key === 'Enter' || e.key === 'Tab' ? 'pick' : null
}

/** Moves an index by delta within [0, length), wrapping at both ends; an out-of-range index starts over at 0. */
export function wrapIndex(index: number, delta: number, length: number): number {
  if (length <= 0 || index < 0 || index >= length) return 0
  return (((index + delta) % length) + length) % length
}
