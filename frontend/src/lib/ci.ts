// Pure parts of following CI on your pull requests (store/ci.ts): the state of
// a branch's checks, when to look again, what changed, and the agent draft for
// a failed run. Go reads gh and builds the redacted, bounded report.

/** The part of settings.json under "ci" (Go: internal/settings/ci.go). */
export interface CISettings {
  /** Seconds between reads of a pull request's checks while some are running. */
  pollSeconds: number
  /** Lines from the end of a failed step that go into the agent's draft. */
  logTailLines: number
  /** Error lines picked from the rest of the failed step. */
  logErrorLines: number
  /** Notify when CI fails or passes while Flint is in the background. */
  notify: boolean
}

/** Mirrors Go's defaultCI. */
export const DEFAULT_CI: CISettings = Object.freeze({ pollSeconds: 30, logTailLines: 60, logErrorLines: 40, notify: true })

/** Same bounds as Go (Min/MaxCI*). */
export const CI_LIMITS = {
  pollSeconds: { min: 10, max: 3600 },
  logTailLines: { min: 5, max: 1000 },
  logErrorLines: { min: 0, max: 500 },
} as const

type NumberKey = keyof typeof CI_LIMITS

function clampCI(key: NumberKey, v: unknown): number {
  if (typeof v !== 'number' || !Number.isFinite(v) || (v === 0 && key !== 'logErrorLines')) return DEFAULT_CI[key]
  const { min, max } = CI_LIMITS[key]
  return Math.min(max, Math.max(min, Math.round(v)))
}

export function coerceCI(v: unknown): CISettings {
  const o = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {}
  return {
    pollSeconds: clampCI('pollSeconds', o.pollSeconds),
    logTailLines: clampCI('logTailLines', o.logTailLines),
    logErrorLines: clampCI('logErrorLines', o.logErrorLines),
    notify: typeof o.notify === 'boolean' ? o.notify : DEFAULT_CI.notify,
  }
}

export type CIState = 'none' | 'pending' | 'failure' | 'success'

interface Counts {
  success: number
  failure: number
  pending: number
  skipped: number
  neutral: number
}

/** The state of a branch's pull request checks, like GitHub's commit status. */
export function ciState(b: { pr: unknown; counts: Counts } | null | undefined): CIState {
  if (!b?.pr) return 'none'
  const n = b.counts
  if (n.failure > 0) return 'failure'
  if (n.pending > 0) return 'pending'
  return n.success + n.skipped + n.neutral > 0 ? 'success' : 'none'
}

/** What a change of state is worth telling about (null: nothing). */
export function ciTransition(prev: CIState | undefined, next: CIState): 'failed' | 'passed' | null {
  if (prev === undefined || prev === next) return null
  if (next === 'failure') return 'failed'
  if (next === 'success' && (prev === 'pending' || prev === 'failure')) return 'passed'
  return null
}

const MAX_BACKOFF = 8
const MAX_POLL_MS = 30 * 60_000

/**
 * The wait before reading running checks again: the interval, growing by half
 * each time nothing changed (up to 8×, at most 30 min), doubled in the background.
 */
export function nextPollMs(pollSeconds: number, unchanged: number, focused: boolean): number {
  const factor = Math.min(MAX_BACKOFF, 1.5 ** Math.max(0, unchanged))
  const ms = pollSeconds * 1000 * factor * (focused ? 1 : 2)
  return Math.round(Math.min(MAX_POLL_MS, ms))
}

export interface CheckLike {
  name: string
  workflow: string
  state: string
  runId: number
  jobId: number
}

export interface FailedRun {
  runId: number
  /** Failed jobs of the run (empty: the check named no job). */
  jobIds: number[]
  /** "Workflow / check" names, for labels. */
  names: string[]
}

/** The GitHub Actions runs with failed checks, in check order. */
export function failedRuns(checks: readonly CheckLike[]): FailedRun[] {
  const runs = new Map<number, FailedRun>()
  for (const c of checks) {
    if (c.state !== 'failure' || !c.runId) continue
    const run = runs.get(c.runId) ?? { runId: c.runId, jobIds: [], names: [] }
    if (c.jobId && !run.jobIds.includes(c.jobId)) run.jobIds.push(c.jobId)
    run.names.push(checkName(c))
    runs.set(c.runId, run)
  }
  return [...runs.values()]
}

export const checkName = (c: { name: string; workflow: string }) => (c.workflow ? `${c.workflow} / ${c.name}` : c.name)

/** Go's main.CIReport, as far as the prompt reads it. */
export interface CIReportLike {
  runId: number
  workflow: string
  title: string
  url: string
  branch: string
  sha: string
  attempt: number
  jobs: {
    name: string
    url: string
    step: string
    stepNumber: number
    lines: string[]
    omitted: number
    noLog: boolean
    files: { text: string; path: string; line: number; column: number }[]
  }[]
}

export interface PromptContext {
  pr?: { number: number; title: string; url: string }
  /** The branch the workspace folder has checked out, to warn when it differs. */
  workspaceBranch?: string
}

/** A code fence longer than any run of backticks in lines. */
export function fence(lines: readonly string[]): string {
  const longest = Math.max(0, ...lines.map((l) => Math.max(0, ...(l.match(/`+/g) ?? []).map((m) => m.length))))
  return '`'.repeat(Math.max(3, longest + 1))
}

const location = (f: { path: string; line: number; column: number }) => `${f.path}:${f.line}${f.column ? `:${f.column}` : ''}`

/** The agent chat draft for failed runs: what failed, the excerpt, the files, the links. */
export function ciFixPrompt(reports: readonly CIReportLike[], ctx: PromptContext): string {
  const first = reports[0]
  const branch = first?.branch ?? ''
  const sha = first?.sha ? ` at commit ${first.sha.slice(0, 12)}` : ''
  const out: string[] = []
  out.push(
    ctx.pr
      ? `CI failed on pull request #${ctx.pr.number} "${ctx.pr.title}" (${ctx.pr.url}), branch ${branch}${sha}.`
      : `CI failed on branch ${branch}${sha}.`,
  )
  if (ctx.workspaceBranch && branch && ctx.workspaceBranch !== branch) {
    out.push(`The pull request's branch is ${branch}, but this workspace is on ${ctx.workspaceBranch}: check out ${branch} (or ask me) before changing files.`)
  }
  for (const r of reports) {
    out.push('', `Workflow "${r.workflow}"${r.attempt > 1 ? ` (attempt ${r.attempt})` : ''}: ${r.url}`)
    for (const j of r.jobs) {
      const step = j.step ? ` at step ${j.stepNumber ? `${j.stepNumber} ` : ''}"${j.step}"` : ''
      const link = j.url ? ` (${j.url})` : ''
      if (j.noLog) {
        out.push('', `Job "${j.name}" failed${step}${link}. GitHub returned no log for it (it may have expired).`)
        continue
      }
      out.push('', `Job "${j.name}" failed${step}${link}. The relevant lines of its log:`)
      const f = fence(j.lines)
      out.push(`${f}text`, ...j.lines, f)
      if (j.omitted > 0) out.push(`(${j.omitted} lines of the step are left out; the job link has the full log.)`)
      if (j.files.length) {
        out.push('', 'Files in this workspace the log points at:')
        for (const file of j.files) {
          const at = location(file)
          out.push(`- ${at}${file.text !== at && !at.endsWith(file.text) ? ` (the log says ${file.text})` : ''}`)
        }
      }
    }
  }
  out.push('', 'Find the cause and fix it. Run the failing step\'s command locally to confirm before you call it fixed.')
  return out.join('\n')
}

/** The background notification for a change of a pull request's CI. */
export function ciNotice(kind: 'failed' | 'passed', pr: { number: number; title: string }, failed: readonly string[]): { title: string; body: string } {
  const title = `CI ${kind}: #${pr.number} ${pr.title}`
  if (kind === 'passed') return { title, body: 'All checks passed' }
  const shown = failed.slice(0, 3).join(', ')
  return { title, body: failed.length > 3 ? `${shown} and ${failed.length - 3} more` : shown || 'A check failed' }
}
