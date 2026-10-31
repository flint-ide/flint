// Pure helpers for git blame annotations and history lists (no CodeMirror, no React).
import type { git } from '../wails/models'

export type BlameCommit = git.BlameCommit
export type BlameRange = git.BlameRange

/** Stand-in for lines git has no commit for (untracked file, new lines). */
export const NOT_COMMITTED: BlameCommit = {
  hash: '0000000000000000000000000000000000000000',
  author: 'Not Committed Yet',
  email: '',
  time: 0,
  summary: 'Not committed',
  path: '',
  previous: '',
  prevPath: '',
  uncommitted: true,
}

/** YYYY-MM-DD in local time, from unix seconds. */
export function formatDate(time: number): string {
  if (!time) return ''
  const d = new Date(time * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** "YYYY-MM-DD HH:MM" in local time, for tooltips and history rows. */
export function formatDateTime(time: number): string {
  if (!time) return ''
  const d = new Date(time * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${formatDate(time)} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** Short author for the narrow gutter column: the first name, capped. */
export function shortAuthor(name: string, max = 12): string {
  const first = name.trim().split(/\s+/)[0] ?? ''
  return first.length > max ? `${first.slice(0, max - 1)}…` : first
}

/** Gutter text, IntelliJ style: date and short author. */
export function blameLabel(c: BlameCommit): string {
  return c.uncommitted ? 'Not committed' : `${formatDate(c.time)} ${shortAuthor(c.author)}`
}

/** Hover text: full hash, author, date and summary. */
export function blameTooltip(c: BlameCommit): string {
  if (c.uncommitted) return 'Not committed yet'
  const who = c.email ? `${c.author} <${c.email}>` : c.author
  return `${c.hash}\n${who}\n${formatDateTime(c.time)}\n\n${c.summary}`
}

/**
 * The commit of each line 1…lines (index 0 unused). Lines without a range — beyond
 * what was blamed, or every line of an untracked file — get NOT_COMMITTED.
 */
export function lineCommits(commits: BlameCommit[], ranges: BlameRange[], lines: number, untracked = false): BlameCommit[] {
  const out = new Array<BlameCommit>(lines + 1).fill(NOT_COMMITTED)
  if (untracked) return out
  for (const r of ranges) {
    const c = commits[r.commit]
    if (!c) continue
    const end = Math.min(r.line + r.count, lines + 1)
    for (let n = Math.max(r.line, 1); n < end; n++) out[n] = c
  }
  return out
}

/** Relative path of an absolute one under root ("" when outside). */
export function relativeTo(root: string, path: string): string {
  const prefix = root.endsWith('/') ? root : `${root}/`
  return path.startsWith(prefix) ? path.slice(prefix.length) : ''
}

/** Absolute path of a repo-relative one. */
export function joinRoot(root: string, rel: string): string {
  return `${root.replace(/\/$/, '')}/${rel}`
}
