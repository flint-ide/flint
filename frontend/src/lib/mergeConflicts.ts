// Conflict blocks in a file as git writes them, and how the merge editor resolves
// them. Pure: the document is anything shaped like CodeMirror's Text. The marker
// rules match internal/git/markers.go.

import { fenced } from './agentPrompt'

/** The part of a CodeMirror Text this module reads (Text satisfies it). */
export interface DocLines {
  readonly lines: number
  readonly length: number
  line(n: number): { text: string; from: number; to: number }
}

/** One conflict: 1-based line numbers of its markers (base is null without one). */
export interface ConflictRange {
  open: number
  base: number | null
  sep: number
  close: number
}

/** The lines of each side of a conflict. */
export interface ConflictText {
  current: string[]
  base: string[] | null
  incoming: string[]
}

/** How a conflict is resolved by an action. */
export type Resolution = 'current' | 'incoming' | 'both' | 'both-incoming-first'

/** A stretch of a diff3-style merge: lines common to all sides, or a conflict with its base. */
export type Diff3Segment = { common: string[] } | { current: string[]; base: string[]; incoming: string[] }

const MARKER = 7

/**
 * '<' opens a conflict, '|' starts its base, '=' separates the sides, '>' closes it.
 * A marker is exactly seven of its character, alone or followed by a space and a
 * label (never for '='), so a Markdown heading underline is not one.
 */
export function markerKind(text: string): '<' | '|' | '=' | '>' | null {
  const line = text.replace(/[\r\n]+$/, '')
  const c = line[0]
  if (c !== '<' && c !== '|' && c !== '=' && c !== '>') return null
  if (line.length < MARKER || line.slice(0, MARKER) !== c.repeat(MARKER)) return null
  const rest = line.slice(MARKER)
  return rest === '' || (c !== '=' && rest[0] === ' ') ? c : null
}

/** Complete conflict blocks in the document, in order. */
export function findConflicts(doc: DocLines): ConflictRange[] {
  const found: ConflictRange[] = []
  let open = 0
  let base: number | null = null
  let sep = 0
  for (let n = 1; n <= doc.lines; n++) {
    const kind = markerKind(doc.line(n).text)
    if (kind === '<') {
      open = n
      base = null
      sep = 0
    } else if (kind === '|' && open && !sep && base === null) {
      base = n
    } else if (kind === '=' && open && !sep) {
      sep = n
    } else if (kind === '>' && sep) {
      found.push({ open, base, sep, close: n })
      open = 0
      base = null
      sep = 0
    }
  }
  return found
}

/** Whether text still holds a complete conflict block. */
export function hasConflictMarkers(text: string): boolean {
  return findConflicts(linesOf(text)).length > 0
}

/** A DocLines over a plain string (tests and quick checks). */
export function linesOf(text: string): DocLines {
  const lines = text.split('\n')
  const starts: number[] = []
  let pos = 0
  for (const l of lines) {
    starts.push(pos)
    pos += l.length + 1
  }
  return {
    lines: lines.length,
    length: text.length,
    line: (n) => ({ text: lines[n - 1], from: starts[n - 1], to: starts[n - 1] + lines[n - 1].length }),
  }
}

function slice(doc: DocLines, from: number, to: number): string[] {
  const out: string[] = []
  for (let n = from; n < to; n++) out.push(doc.line(n).text)
  return out
}

export function conflictText(doc: DocLines, c: ConflictRange): ConflictText {
  return {
    current: slice(doc, c.open + 1, c.base ?? c.sep),
    base: c.base === null ? null : slice(doc, c.base + 1, c.sep),
    incoming: slice(doc, c.sep + 1, c.close),
  }
}

export function resolvedLines(t: ConflictText, how: Resolution): string[] {
  switch (how) {
    case 'current':
      return t.current
    case 'incoming':
      return t.incoming
    case 'both':
      return [...t.current, ...t.incoming]
    case 'both-incoming-first':
      return [...t.incoming, ...t.current]
  }
}

/**
 * The edit replacing a conflict (its marker lines included) by lines. An empty
 * resolution removes the lines entirely, with one of their line breaks.
 */
export function replaceConflict(doc: DocLines, c: ConflictRange, lines: string[], lineBreak = '\n'): { from: number; to: number; insert: string } {
  const from = doc.line(c.open).from
  if (c.close < doc.lines) {
    return { from, to: doc.line(c.close + 1).from, insert: lines.map((l) => l + lineBreak).join('') }
  }
  const to = doc.line(c.close).to
  if (lines.length === 0 && c.open > 1) return { from: doc.line(c.open - 1).to, to, insert: '' }
  return { from, to, insert: lines.join(lineBreak) }
}

/** Index of the conflict containing line, or -1. */
export function conflictAt(conflicts: ConflictRange[], line: number): number {
  return conflicts.findIndex((c) => line >= c.open && line <= c.close)
}

/** The next (dir 1) or previous (-1) conflict from line, wrapping around; -1 when none. */
export function nextConflict(conflicts: ConflictRange[], line: number, dir: 1 | -1): number {
  if (conflicts.length === 0) return -1
  if (dir === 1) {
    const i = conflicts.findIndex((c) => c.open > line)
    return i < 0 ? 0 : i
  }
  for (let i = conflicts.length - 1; i >= 0; i--) if (conflicts[i].close < line) return i
  return conflicts.length - 1
}

/** Splits a diff3-style merge (Go's ConflictFile.diff3) into common stretches and conflicts. */
export function diff3Segments(text: string): Diff3Segment[] {
  const lines = text.replace(/\r\n/g, '\n').split('\n')
  if (lines[lines.length - 1] === '') lines.pop()
  const doc = linesOf(lines.join('\n'))
  const segments: Diff3Segment[] = []
  let next = 1
  for (const c of findConflicts(doc)) {
    if (c.open > next) segments.push({ common: slice(doc, next, c.open) })
    const t = conflictText(doc, c)
    segments.push({ current: t.current, base: t.base ?? [], incoming: t.incoming })
    next = c.close + 1
  }
  if (next <= doc.lines && lines.length > 0) segments.push({ common: slice(doc, next, doc.lines + 1) })
  return segments
}

/**
 * The base of a conflict whose markers lack it. Git's default style joins conflicts
 * a few lines apart into one, so the conflict is matched against runs of the diff3
 * merge (conflict, common lines, conflict…) whose sides add up to the same lines.
 */
export function baseFor(t: ConflictText, segments: readonly Diff3Segment[]): string[] | null {
  if (t.base) return t.base
  for (let i = 0; i < segments.length; i++) {
    if ('common' in segments[i]) continue
    const run = { current: [] as string[], base: [] as string[], incoming: [] as string[] }
    for (const s of segments.slice(i)) {
      if ('common' in s) {
        run.current.push(...s.common)
        run.base.push(...s.common)
        run.incoming.push(...s.common)
      } else {
        run.current.push(...s.current)
        run.base.push(...s.base)
        run.incoming.push(...s.incoming)
      }
      if (!startsWith(t.current, run.current) || !startsWith(t.incoming, run.incoming)) break
      if (!('common' in s) && run.current.length === t.current.length && run.incoming.length === t.incoming.length) return run.base
    }
  }
  return null
}

function startsWith(lines: string[], prefix: string[]): boolean {
  return prefix.length <= lines.length && prefix.every((l, i) => l === lines[i])
}

/** One side as the merge editor and the agent prompt name it. */
export interface SideInfo {
  label: string
  detail: string
}

const OPERATION_NAMES: Record<string, string> = {
  merging: 'a merge',
  rebasing: 'a rebase',
  'cherry-picking': 'a cherry-pick',
  reverting: 'a revert',
}

export interface ConflictPrompt {
  /** Repository-relative path. */
  rel: string
  index: number
  total: number
  range: ConflictRange
  text: ConflictText
  base: string[] | null
  operation: string
  current: SideInfo
  incoming: SideInfo
  /** The file holds credentials as a whole (Go's redact.SecretFile): no contents go along. */
  secretFile?: boolean
}

/** What GitAPI.RedactForAgent returns. */
export interface Redacted {
  secretFile: boolean
  texts: string[]
}

/** A conflict's sides and base as GitAPI.RedactForAgent takes them: one string each. */
export function redactionInput(t: ConflictText, base: string[] | null): string[] {
  return [t.current, t.incoming, ...(base ? [base] : [])].map((lines) => lines.join('\n'))
}

/** The sides and base again, from RedactForAgent's answer to redactionInput. */
export function applyRedaction(t: ConflictText, base: string[] | null, r: Redacted): { text: ConflictText; base: string[] | null; secretFile: boolean } {
  if (r.secretFile || r.texts.length !== redactionInput(t, base).length) {
    return { text: { current: [], base: null, incoming: [] }, base: null, secretFile: true }
  }
  const lines = (i: number, original: string[]) => (original.length === 0 ? [] : r.texts[i].split('\n'))
  return {
    text: { current: lines(0, t.current), base: null, incoming: lines(1, t.incoming) },
    base: base ? lines(2, base) : null,
    secretFile: false,
  }
}

/**
 * Prompt handing one conflict, with both sides and the base, to the agent. The texts
 * must already be redacted (GitAPI.RedactForAgent).
 */
export function conflictPrompt(p: ConflictPrompt): string {
  const op = OPERATION_NAMES[p.operation] ?? 'applying changes (for example git stash pop)'
  const block = (lines: string[]) => fenced(lines.join('\n'))
  const base = p.base
    ? `Base (common ancestor):\n\n${block(p.base)}`
    : `Base: not shown here; \`git show :1:${p.rel}\` prints the base version of the file.`
  const sides = p.secretFile
    ? [
        `Current (${p.current.label}): ${p.current.detail}`,
        `Incoming (${p.incoming.label}): ${p.incoming.detail}`,
        `${p.rel} holds credentials, so Flint left the conflict's contents out of this message.`,
      ]
    : [
        `Current (${p.current.label}): ${p.current.detail}\n\n${block(p.text.current)}`,
        `Incoming (${p.incoming.label}): ${p.incoming.detail}\n\n${block(p.text.incoming)}`,
        base,
      ]
  return [
    `Resolve merge conflict ${p.index + 1} of ${p.total} in ${p.rel} (lines ${p.range.open}–${p.range.close}), left by ${op}.`,
    ...sides,
    'Replace only this conflict block in the file (from its <<<<<<< line to its >>>>>>> line) with code that keeps what both sides intended. Leave the other conflicts and the index alone: I review the result in the merge editor and mark the file resolved myself.',
  ].join('\n\n')
}
