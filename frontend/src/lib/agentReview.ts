// Pure parts of Review Changes with Agent: where a finding sits after the file
// changed, counts, and the prompts that hand findings to the agent chat. No
// React or CodeMirror here, so node:test runs it.

import type { Attachment } from './agentReducer'
import { fenced, problemAttachments, type Lines } from './agentPrompt'

export type Severity = 'error' | 'warning' | 'info'

export const SEVERITIES: readonly Severity[] = ['error', 'warning', 'info']

/** A finding as Go's review.Finding sends it. */
export interface Finding {
  id: string
  path: string
  /** 1-based line of the new version when the review ran; 0 = the whole file. */
  line: number
  endLine: number
  severity: string
  title: string
  explanation: string
  suggestion: string
  /** The line and its neighbours when the review ran. */
  lineText: string
  before: string
  after: string
}

/** A finding where it is now. */
export interface PlacedFinding extends Finding {
  /** 1-based line in the current file; 0 = file level. */
  at: number
  /** The code it was about changed, so it can't be placed any more. */
  stale: boolean
}

export const severityOf = (s: string): Severity => (s === 'error' || s === 'info' ? s : 'warning')

export const placed = (f: Finding): PlacedFinding => ({ ...f, at: f.line, stale: false })

/**
 * Finds a finding's line again in the file's current lines (null: the file is
 * gone). The line keeps its place while its text is there; otherwise the copy of
 * its text whose neighbours match best, nearest the old place, wins. A blank line
 * needs a matching neighbour. Nothing found: stale, at its old place.
 */
export function reanchor(f: Finding, lines: readonly string[] | null): { at: number; stale: boolean } {
  if (f.line < 1) return { at: 0, stale: false }
  if (!lines) return { at: f.line, stale: true }
  const known = f.lineText !== '' || f.before !== '' || f.after !== ''
  if (!known) return { at: f.line, stale: f.line > lines.length }
  let best = -1
  let bestScore = -1
  const old = f.line - 1
  for (let i = 0; i < lines.length; i++) {
    if (lines[i] !== f.lineText) continue
    const score = Number((i > 0 ? lines[i - 1] : '') === f.before) + Number((i + 1 < lines.length ? lines[i + 1] : '') === f.after)
    if (score > bestScore || (score === bestScore && Math.abs(i - old) < Math.abs(best - old))) {
      best = i
      bestScore = score
    }
  }
  const blank = f.lineText.trim() === ''
  if (best < 0 || (blank && bestScore < 1 && best !== old)) return { at: Math.min(f.line, Math.max(lines.length, 1)), stale: true }
  return { at: best + 1, stale: false }
}

/** Places every finding of path anew against the file's current lines. */
export function reanchorPath<F extends PlacedFinding>(findings: readonly F[], path: string, lines: readonly string[] | null): F[] {
  return findings.map((f) => {
    if (f.path !== path) return f
    const { at, stale } = reanchor(f, lines)
    return at === f.at && stale === f.stale ? f : { ...f, at, stale }
  })
}

/** The lines of a file's text, as Go's review.Lines splits them. */
export function splitLines(content: string): string[] {
  if (content === '') return []
  return content.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n')
}

export function severityCounts(findings: readonly { severity: string }[]): Record<Severity, number> {
  const n: Record<Severity, number> = { error: 0, warning: 0, info: 0 }
  for (const f of findings) n[severityOf(f.severity)] += 1
  return n
}

/** "2 errors, 1 warning" (none: "no findings"). */
export function countSummary(findings: readonly { severity: string }[]): string {
  const n = severityCounts(findings)
  const parts = SEVERITIES.filter((s) => n[s] > 0).map((s) => `${n[s]} ${s === 'info' ? 'info' : s + (n[s] === 1 ? '' : 's')}`)
  return parts.length ? parts.join(', ') : 'no findings'
}

/** By file, then line; file-level findings first in their file. */
export function sortFindings<F extends PlacedFinding>(findings: readonly F[]): F[] {
  return [...findings].sort((a, b) => (a.path === b.path ? a.at - b.at : a.path < b.path ? -1 : 1))
}

/** The placement input of lib/prReview's placeThreads: findings sit on the new side. */
export function findingThreads(findings: readonly PlacedFinding[]): { id: string; line: number; side: string; fileLevel: boolean }[] {
  return findings.map((f) => ({ id: f.id, line: f.at, side: 'RIGHT', fileLevel: f.at < 1 || f.stale }))
}

/** "main.go:12" (file level: "main.go"). */
export const location = (f: PlacedFinding) => (f.at > 0 ? `${f.path}:${f.at}` : f.path)

function findingBlock(f: PlacedFinding): string {
  const lines = [`${location(f)} (${severityOf(f.severity)}): ${f.title}`]
  if (f.stale) lines.push(`(The code there changed after the review: check whether this still applies; it was line ${f.line}.)`)
  if (f.explanation && f.explanation !== f.title) lines.push(f.explanation)
  if (f.suggestion) {
    const range = f.endLine > f.line ? `lines ${f.at}–${f.at + f.endLine - f.line}` : `line ${f.at}`
    lines.push(`Suggested replacement for ${range}:\n${fenced(f.suggestion)}`)
  }
  return lines.join('\n\n')
}

/** The chat draft that asks the agent to fix one finding. */
export function findingPrompt(f: PlacedFinding): string {
  return `Fix this finding from a review of my uncommitted changes:\n\n${findingBlock(f)}`
}

/** The chat draft that asks the agent to fix several findings, worst first. */
export function findingsPrompt(findings: readonly PlacedFinding[]): string {
  if (findings.length === 1) return findingPrompt(findings[0])
  const rank = (f: PlacedFinding) => SEVERITIES.indexOf(severityOf(f.severity))
  const sorted = sortFindings(findings).sort((a, b) => rank(a) - rank(b))
  const items = sorted.map((f, i) => `${i + 1}. ${findingBlock(f).replace(/\n/g, '\n   ')}`)
  return `Fix these ${findings.length} findings (${countSummary(findings)}) from a review of my uncommitted changes:\n\n${items.join('\n\n')}`
}

/** Lines of a string array as lib/agentPrompt's Lines. */
export const asLines = (lines: readonly string[]): Lines => ({ lines: lines.length, line: (n) => ({ text: lines[n - 1] ?? '' }) })

/**
 * The code around each placed finding of one file (absPath), for the chat
 * draft; the agent reads the rest itself.
 */
export function findingAttachments(absPath: string, lines: readonly string[], findings: readonly PlacedFinding[]): Attachment[] {
  const onLines = findings.filter((f) => f.at > 0 && !f.stale && f.at <= lines.length)
  if (onLines.length === 0) return [{ path: absPath }]
  return problemAttachments(
    absPath,
    asLines(lines),
    onLines.map((f) => ({ line: f.at, col: 1, endLine: f.at + Math.max(0, f.endLine - f.line), severity: severityOf(f.severity), message: f.title })),
  )
}
