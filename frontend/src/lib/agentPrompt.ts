// Pure prompt building for "ask the agent" actions (diagnostics, terminal output,
// failed tests…). Everything here is plain data in, plain text out.

import type { Attachment } from './agentReducer'
import { inside } from './sessionSnapshot'

/** One editor problem, with 1-based line/column numbers. */
export interface Problem {
  line: number
  col: number
  /** Last line the problem covers (≥ line). */
  endLine: number
  severity: 'error' | 'warning' | 'info' | 'hint'
  /** Who reported it, e.g. gopls. */
  source?: string
  message: string
}

/** The part of a CodeMirror Text the snippets need (Text satisfies it). */
export interface Lines {
  readonly lines: number
  line(n: number): { text: string }
}

/** Lines of context shown around a problem. */
export const CONTEXT_LINES = 2
/** Above this many separate snippets the agent gets the file path instead. */
export const MAX_SNIPPETS = 8

/** Path as the agent (running in the project root) sees it. */
export function relativePath(path: string, root: string): string {
  return root && path !== root && inside(path, root) ? path.slice(root.replace(/\/$/, '').length + 1) : path
}

/** A backtick fence longer than any backtick run inside text (same rule as the Go side). */
export function codeFence(text: string): string {
  const longest = Math.max(0, ...(text.match(/`+/g) ?? []).map((run) => run.length))
  return '`'.repeat(Math.max(3, longest + 1))
}

/** text as a fenced Markdown block. */
export function fenced(text: string, lang = ''): string {
  const fence = codeFence(text)
  return `${fence}${lang}\n${text}\n${fence}`
}

/** "12:5 error (gopls): message", continuation lines of the message indented. */
export function problemLine(p: Problem): string {
  const source = p.source ? ` (${p.source})` : ''
  const message = p.message.trim().replace(/\n/g, '\n  ')
  return `${p.line}:${p.col} ${p.severity}${source}: ${message}`
}

/** Prompt asking to fix one problem in a file. */
export function problemPrompt(path: string, root: string, p: Problem): string {
  const rel = relativePath(path, root)
  return `Fix this ${p.severity}${p.source ? ` reported by ${p.source}` : ''} in ${rel}:\n\n- ${rel}:${problemLine(p)}`
}

/** Prompt asking to fix every problem of a file, worst first. */
export function fileProblemsPrompt(path: string, root: string, problems: Problem[]): string {
  if (problems.length === 1) return problemPrompt(path, root, problems[0])
  const rel = relativePath(path, root)
  const sorted = sortProblems(problems)
  return `Fix these ${problems.length} problems (${countSummary(problems)}) in ${rel}:\n\n${sorted.map((p) => `- ${problemLine(p)}`).join('\n')}`
}

/**
 * Attachments with the lines around each problem (overlapping ranges merged).
 * Too many separate places: one attachment naming the file, the agent reads it.
 */
export function problemAttachments(path: string, doc: Lines, problems: Problem[]): Attachment[] {
  const ranges = mergeRanges(
    problems.map((p) => [
      Math.max(1, Math.min(p.line, doc.lines) - CONTEXT_LINES),
      Math.min(doc.lines, Math.max(p.endLine, p.line) + CONTEXT_LINES),
    ]),
  )
  if (ranges.length === 0 || ranges.length > MAX_SNIPPETS) return [{ path }]
  return ranges.map(([startLine, endLine]) => ({ path, startLine, endLine, text: sliceLines(doc, startLine, endLine) }))
}

/** Prompt carrying selected terminal output as a fenced block. */
export function terminalOutputPrompt(output: string, title?: string): string {
  const text = output
    .split('\n')
    .map((l) => l.trimEnd())
    .join('\n')
    .replace(/^\n+|\n+$/g, '')
  return `Output from the terminal${title ? ` "${title}"` : ''}:\n\n${fenced(text)}`
}

/** A failed test as the prompt needs it (store/run's FailedTest fits). */
export interface FailedTestInfo {
  name: string
  output: string
  location?: { path: string; line: number }
}

/** Test output kept per failure; the agent can rerun the test for the rest. */
export const MAX_TEST_OUTPUT_LINES = 40
/** Failures listed in full; the rest are only counted. */
export const MAX_FAILED_TESTS = 10

/** Prompt asking to make failed tests pass, each with its location and the end of its output. */
export function failedTestsPrompt(tests: FailedTestInfo[], root: string): string {
  const shown = tests.slice(0, MAX_FAILED_TESTS)
  const parts = shown.map((t) => {
    const where = t.location ? ` (${relativePath(t.location.path, root)}:${t.location.line})` : ''
    const lines = t.output.replace(/\s+$/, '').split('\n')
    const tail = lines.slice(-MAX_TEST_OUTPUT_LINES).join('\n')
    const cut = lines.length > MAX_TEST_OUTPUT_LINES ? `(last ${MAX_TEST_OUTPUT_LINES} lines)\n` : ''
    return `- ${t.name}${where}${tail ? `\n\n${cut}${fenced(tail)}` : ''}`
  })
  const more = tests.length > shown.length ? `\n\n…and ${tests.length - shown.length} more failed.` : ''
  const what = tests.length === 1 ? 'this failing test' : `these ${tests.length} failing tests`
  return `Make ${what} pass (fix the code, or the test if it is wrong):\n\n${parts.join('\n\n')}${more}`
}

/** Adds text after what the user already typed, separated by a blank line. */
export function appendToDraft(draft: string, text: string): string {
  if (!text.trim()) return draft
  if (!draft.trim()) return text
  return `${draft.trimEnd()}\n\n${text}`
}

/** Chat candidates, most relevant first: the active tab, recently used tabs, newest chat. */
export interface ChatPick {
  activeId: string | null
  /** Editor history, oldest first. */
  backStack: string[]
  /** Chat session ids, oldest first. */
  order: string[]
  /** Working directory of each open chat. */
  cwds: Record<string, string>
  /** Current project; empty when no folder is open (any chat qualifies). */
  root: string
}

/** The chat to send to: active or most recently used one of the current project. */
export function pickChat({ activeId, backStack, order, cwds, root }: ChatPick): string | null {
  const fromTabs = [activeId, ...[...backStack].reverse()]
    .filter((id): id is string => !!id && id.startsWith('chat:'))
    .map((id) => id.slice(5))
  const candidates = [...fromTabs, ...[...order].reverse()]
  return candidates.find((id) => id in cwds && (!root || inside(cwds[id], root))) ?? null
}

const SEVERITY_RANK: Record<Problem['severity'], number> = { error: 0, warning: 1, info: 2, hint: 3 }

function sortProblems(problems: Problem[]): Problem[] {
  return [...problems].sort((a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] || a.line - b.line || a.col - b.col)
}

function countSummary(problems: Problem[]): string {
  const counts = new Map<string, number>()
  for (const p of sortProblems(problems)) counts.set(p.severity, (counts.get(p.severity) ?? 0) + 1)
  return [...counts].map(([sev, n]) => `${n} ${n === 1 ? sev : `${sev}s`}`).join(', ')
}

function mergeRanges(ranges: number[][]): [number, number][] {
  const sorted = [...ranges].sort((a, b) => a[0] - b[0])
  return sorted.reduce<[number, number][]>((out, [from, to]) => {
    const last = out[out.length - 1]
    if (last && from <= last[1] + 1) return [...out.slice(0, -1), [last[0], Math.max(last[1], to)]]
    return [...out, [from, to]]
  }, [])
}

function sliceLines(doc: Lines, from: number, to: number): string {
  return Array.from({ length: to - from + 1 }, (_, i) => doc.line(from + i).text).join('\n')
}
