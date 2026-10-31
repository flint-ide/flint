// Pure helpers of the pull request review UI: CI and review badges, and where review
// threads go in the side-by-side diff. No React or CodeMirror here, so node:test runs it.

/** CI check counts by normalized state (Go's github.CheckCounts). */
export interface CheckCounts {
  success: number
  failure: number
  pending: number
  skipped: number
  neutral: number
}

/** The state of all checks together, like GitHub's commit status. */
export type Rollup = 'success' | 'failure' | 'pending' | 'none'

export function countChecks(checks: readonly { state: string }[]): CheckCounts {
  const n: CheckCounts = { success: 0, failure: 0, pending: 0, skipped: 0, neutral: 0 }
  for (const c of checks) {
    const key = c.state in n ? (c.state as keyof CheckCounts) : 'neutral'
    n[key] += 1
  }
  return n
}

/** Any failure fails; otherwise anything running is pending; skipped and neutral checks pass. */
export function rollup(n: CheckCounts): Rollup {
  if (n.failure > 0) return 'failure'
  if (n.pending > 0) return 'pending'
  return n.success + n.skipped + n.neutral > 0 ? 'success' : 'none'
}

export const ROLLUP_GLYPH: Record<Rollup, string> = { success: '✓', failure: '✗', pending: '●', none: '' }

/** "2 failing, 1 pending, 5 successful" (skipped and neutral last). */
export function checksSummary(n: CheckCounts): string {
  const parts = [
    n.failure && `${n.failure} failing`,
    n.pending && `${n.pending} pending`,
    n.success && `${n.success} successful`,
    n.skipped && `${n.skipped} skipped`,
    n.neutral && `${n.neutral} neutral`,
  ].filter(Boolean)
  return parts.length ? parts.join(', ') : 'No checks'
}

export type BadgeTone = 'ok' | 'bad' | 'pending' | 'muted' | 'merged'

export interface Badge {
  label: string
  tone: BadgeTone
}

/** The state badge of a pull request: merged / closed / draft first, then the review decision. */
export function stateBadge(pr: { state: string; draft: boolean; reviewDecision: string }): Badge | null {
  if (pr.state === 'MERGED') return { label: 'Merged', tone: 'merged' }
  if (pr.state === 'CLOSED') return { label: 'Closed', tone: 'bad' }
  if (pr.draft) return { label: 'Draft', tone: 'muted' }
  return reviewBadge(pr.reviewDecision)
}

export function reviewBadge(decision: string): Badge | null {
  switch (decision) {
    case 'APPROVED':
      return { label: 'Approved', tone: 'ok' }
    case 'CHANGES_REQUESTED':
      return { label: 'Changes requested', tone: 'bad' }
    case 'REVIEW_REQUIRED':
      return { label: 'Review required', tone: 'pending' }
    default:
      return null
  }
}

/** A submitted review's state, as GitHub words it. */
export function reviewStateLabel(state: string): string {
  switch (state) {
    case 'APPROVED':
      return 'approved'
    case 'CHANGES_REQUESTED':
      return 'requested changes'
    case 'DISMISSED':
      return 'review dismissed'
    default:
      return 'commented'
  }
}

/** "just now", "5m ago", "3h ago", "2d ago", else the date. */
export function timeAgo(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  if (s < 30 * 86400) return `${Math.floor(s / 86400)}d ago`
  return new Date(t).toISOString().slice(0, 10)
}

// ---------- threads in the diff ----------

export type Side = 'a' | 'b'

/** A diff hunk on 0-based, half-open line ranges (Go's textdiff.Hunk). */
export interface LineHunk {
  fromA: number
  toA: number
  fromB: number
  toB: number
}

/** What placement needs of a review thread (Go's github.Thread). */
export interface ThreadPos {
  id: string
  line: number // 1-based on side; 0 = outdated / file level
  side: string // LEFT (base, side a) | RIGHT (head, side b)
  fileLevel: boolean
}

/** A line with threads (and/or the new-comment box) under it. */
export interface Anchor {
  key: string
  side: Side
  /** 0-based line on its side the widget goes under. */
  line: number
  /**
   * 0-based line on the other side under which an equally tall spacer goes, keeping
   * both sides aligned; -1 means above the first line.
   */
  mirror: number
  threadIds: string[]
  /** The new-comment box is open on this line. */
  draft: boolean
}

export const anchorKey = (side: Side, line: number) => `${side}:${line}`

/**
 * The line of the other side that sits level with `line` of `side`. Unchanged lines
 * map one to one (shifted by what hunks above added or removed); inside a hunk, the
 * same row of the other side, or its last line when that side is shorter.
 */
export function lineOnOtherSide(hunks: readonly LineHunk[], side: Side, line: number): number {
  let delta = 0 // other - own, for unchanged lines
  for (const h of hunks) {
    const [from, to, oFrom, oTo] = side === 'a' ? [h.fromA, h.toA, h.fromB, h.toB] : [h.fromB, h.toB, h.fromA, h.toA]
    if (line < from) break
    if (line < to) {
      const row = line - from
      return row < oTo - oFrom ? oFrom + row : oTo - 1
    }
    delta = oTo - to
  }
  return line + delta
}

/**
 * Groups threads by the line they are on. Threads without a line on the current diff
 * (outdated, file-level, past the end) come back as `detached`, shown above the diff.
 * `draft` adds the new-comment box to its line.
 */
export function placeThreads(
  threads: readonly ThreadPos[],
  hunks: readonly LineHunk[],
  lines: { a: number; b: number },
  draft: { side: Side; line: number } | null = null,
): { anchors: Anchor[]; detached: string[] } {
  const byKey = new Map<string, Anchor>()
  const detached: string[] = []
  const at = (side: Side, line: number): Anchor => {
    const key = anchorKey(side, line)
    const existing = byKey.get(key)
    if (existing) return existing
    const other = side === 'a' ? lines.b : lines.a
    const mirror = Math.min(lineOnOtherSide(hunks, side, line), other - 1)
    const anchor: Anchor = { key, side, line, mirror, threadIds: [], draft: false }
    byKey.set(key, anchor)
    return anchor
  }
  for (const t of threads) {
    const side: Side = t.side === 'LEFT' ? 'a' : 'b'
    const line = t.line - 1
    if (t.fileLevel || t.line < 1 || line >= lines[side]) {
      detached.push(t.id)
      continue
    }
    const anchor = at(side, line)
    byKey.set(anchor.key, { ...anchor, threadIds: [...anchor.threadIds, t.id] })
  }
  if (draft && draft.line >= 0 && draft.line < lines[draft.side]) {
    const anchor = at(draft.side, draft.line)
    byKey.set(anchor.key, { ...anchor, draft: true })
  }
  const anchors = [...byKey.values()].sort((x, y) => (x.side === y.side ? x.line - y.line : x.side < y.side ? -1 : 1))
  return { anchors, detached }
}

/** A folded unchanged stretch (lib/diffDecorations' Fold). */
export interface FoldLike {
  a: number
  b: number
  lines: number
}

/** Drops folds that would hide a thread (or its spacer on the other side). */
export function foldsClearOf<F extends FoldLike>(folds: readonly F[], anchors: readonly Anchor[]): F[] {
  const hides = (f: F, side: Side, line: number) => {
    const first = side === 'a' ? f.a : f.b
    return line >= first && line < first + f.lines
  }
  return folds.filter(
    (f) => !anchors.some((x) => hides(f, x.side, x.line) || hides(f, x.side === 'a' ? 'b' : 'a', x.mirror)),
  )
}

/** Whether a 1-based line lies in one of the ranges GitHub accepts comments on. */
export function isCommentable(ranges: readonly { from: number; to: number }[] | null | undefined, line: number): boolean {
  return !!ranges?.some((r) => line >= r.from && line <= r.to)
}

/** Review threads per file path, for the counts in the file list. */
export function threadCounts(threads: readonly { path: string; resolved: boolean }[]): Record<string, { open: number; resolved: number }> {
  const out: Record<string, { open: number; resolved: number }> = {}
  for (const t of threads) {
    const prev = out[t.path] ?? { open: 0, resolved: 0 }
    out[t.path] = t.resolved ? { ...prev, resolved: prev.resolved + 1 } : { ...prev, open: prev.open + 1 }
  }
  return out
}
