// CI of your pull requests: the current branch's (status bar, Pull Requests
// view) and agent tasks' (Agent Tasks panel). GitHub has no push channel to a
// desktop app, so checks are read when a branch starts being followed, when the
// window regains focus, after git changes the branch or its refs, and — only
// while some check is still running — again after settings.ci.pollSeconds,
// backing off while nothing changes. Everything is read through gh (Go).
import { createStore } from '../lib/store'
import { GitHub, errorMessage } from '../api/backend'
import type { github } from '../wails/models'
import { ciFixPrompt, ciNotice, ciState, ciTransition, checkName, failedRuns, nextPollMs, type CIState, type FailedRun } from '../lib/ci'
import { askAgent } from '../lib/askAgent'
import { alertDone } from './alerts'
import { confirmAction, showToast } from './dialog'
import { pullRequestTabId, useEditor } from './editor'
import { useGit } from './git'
import { useSettings } from './settings'
import { showFolder, useTasks } from './agentTasks'
import type { TaskView } from '../lib/agentTasks'

export interface CIEntry {
  /** Folder gh runs in (a work-tree root). */
  root: string
  branch: string
  data: github.BranchChecks | null
  state: CIState
  error: string | null
  loading: boolean
}

interface CIStore {
  entries: Record<string, CIEntry>
  /** What an entry is busy with ("fix", "rerun"), by key. */
  busy: Record<string, string>
}

export const ciKey = (root: string, branch: string) => `${root}\n${branch}`

export const useCI = createStore<CIStore>(() => ({ entries: {}, busy: {} }))

const set = useCI.setState
const get = useCI.getState

/** Who follows each key: "current" (the active repository) or "task:<id>". */
const owners = new Map<string, Set<string>>()
const timers = new Map<string, ReturnType<typeof setTimeout>>()
/** Polls in a row that found the same checks, for the back-off. */
const unchanged = new Map<string, number>()
const signatures = new Map<string, string>()
const lastRead = new Map<string, number>()
const inflight = new Map<string, Promise<void>>()

/** Focus refreshes skip entries read this recently. */
const FOCUS_GAP_MS = 5_000

function follow(owner: string, root: string | null, branch: string | null): void {
  const key = root && branch ? ciKey(root, branch) : null
  for (const [k, who] of owners) {
    if (k !== key && who.delete(owner) && who.size === 0) forget(k)
  }
  if (!key || !root || !branch) return
  const who = owners.get(key) ?? new Set<string>()
  const isNew = who.size === 0
  who.add(owner)
  owners.set(key, who)
  if (isNew) {
    set((s) => ({ entries: { ...s.entries, [key]: { root, branch, data: null, state: 'none', error: null, loading: true } } }))
    void refresh(key)
  }
}

function forget(key: string): void {
  owners.delete(key)
  clearTimeout(timers.get(key))
  for (const m of [timers, unchanged, signatures, lastRead]) m.delete(key)
  set((s) => {
    const { [key]: _gone, ...entries } = s.entries
    return { entries }
  })
}

/** Reads key's checks now (concurrent calls share one read). */
export function refresh(key: string): Promise<void> {
  const running = inflight.get(key)
  if (running) return running
  const job = read(key).finally(() => inflight.delete(key))
  inflight.set(key, job)
  return job
}

async function read(key: string): Promise<void> {
  const entry = get().entries[key]
  if (!entry) return
  clearTimeout(timers.get(key))
  patch(key, { loading: true })
  lastRead.set(key, Date.now())
  try {
    const data = await GitHub.BranchChecks(entry.root, entry.branch)
    if (!get().entries[key]) return // stopped following meanwhile
    const state = ciState(data)
    const before = get().entries[key]
    if (before.data) notify(ciTransition(before.state, state), before.root, data)
    const sig = JSON.stringify(data.counts) + (data.pr?.headSha ?? '')
    unchanged.set(key, signatures.get(key) === sig ? (unchanged.get(key) ?? 0) + 1 : 0)
    signatures.set(key, sig)
    patch(key, { data, state, error: null, loading: false })
  } catch (err) {
    patch(key, { error: errorMessage(err), loading: false })
  }
  schedule(key)
}

function patch(key: string, change: Partial<CIEntry>): void {
  set((s) => (s.entries[key] ? { entries: { ...s.entries, [key]: { ...s.entries[key], ...change } } } : s))
}

/** Polls again only while checks are running (in the background only when CI notifications are on). */
function schedule(key: string): void {
  clearTimeout(timers.get(key))
  const entry = get().entries[key]
  if (!entry?.data?.pr || entry.data.counts.pending === 0) return // nothing running: nothing to wait for
  const { ci, notifyInBackground } = useSettings.getState().settings
  const focused = document.hasFocus()
  if (!focused && !(ci.notify && notifyInBackground)) return // the focus handler reads it again
  timers.set(key, setTimeout(() => void refresh(key), nextPollMs(ci.pollSeconds, unchanged.get(key) ?? 0, focused)))
}

function notify(kind: 'failed' | 'passed' | null, root: string, data: github.BranchChecks): void {
  if (!kind || !data.pr || !useSettings.getState().settings.ci.notify) return
  const failed = data.checks.filter((c) => c.state === 'failure').map(checkName)
  const { title, body } = ciNotice(kind, data.pr, failed)
  alertDone(title, body, pullRequestTabId(root, data.pr.number))
}

let following = false

/**
 * Follows the active repository's branch and agent tasks' pull requests for
 * the life of the window. Returns a function that stops it.
 */
export function followCI(): () => void {
  if (following) return () => {}
  following = true
  const followCurrent = () => {
    const { root, status } = useGit.getState()
    follow('current', root, status?.branch || null)
  }
  const followTasks = (tasks: TaskView[]) => {
    const wanted = new Set<string>()
    for (const t of tasks) {
      if (!t.pullRequest || t.merged || t.stale) continue
      wanted.add(t.id)
      follow(`task:${t.id}`, t.worktree, t.branch)
    }
    for (const who of owners.values()) {
      for (const o of who) if (o.startsWith('task:') && !wanted.has(o.slice(5))) follow(o, null, null)
    }
  }
  followCurrent()
  followTasks(useTasks.getState().tasks)
  const offGit = useGit.subscribe((s, prev) => {
    if (s.root !== prev.root || s.status?.branch !== prev.status?.branch) followCurrent()
    else if (s.refsVersion !== prev.refsVersion && s.root && s.status?.branch) void refresh(ciKey(s.root, s.status.branch))
  })
  const offTasks = useTasks.subscribe((s, prev) => {
    if (s.tasks !== prev.tasks) followTasks(s.tasks)
  })
  const onFocus = () => {
    for (const key of owners.keys()) {
      if (Date.now() - (lastRead.get(key) ?? 0) > FOCUS_GAP_MS) void refresh(key)
    }
  }
  window.addEventListener('focus', onFocus)
  return () => {
    following = false
    offGit()
    offTasks()
    window.removeEventListener('focus', onFocus)
    for (const key of [...owners.keys()]) forget(key)
  }
}

/** The current branch's entry. */
export function useCurrentCI(): CIEntry | null {
  const root = useGit((s) => s.root)
  const branch = useGit((s) => s.status?.branch ?? '')
  return useCI((s) => (root && branch ? (s.entries[ciKey(root, branch)] ?? null) : null))
}

/** What Fix with agent works on. */
export interface FixTarget {
  /** Folder gh runs in and the agent works in. */
  root: string
  pr: { number: number; title: string; url: string } | null
  checks: readonly github.Check[]
  /** An agent task: its chat and worktree get the draft. */
  task?: TaskView
}

/** Most runs one Fix with agent reads. */
const MAX_RUNS = 3

/**
 * Hands failed CI to the agent: Go reads the failed jobs' logs (redacted,
 * bounded), and the draft lands in the chat of the right project for the user
 * to review and send. only narrows it to one run (and job).
 */
export async function fixCIWithAgent(target: FixTarget, only?: { runId: number; jobId: number }): Promise<void> {
  const runs: FailedRun[] = only
    ? [{ runId: only.runId, jobIds: only.jobId ? [only.jobId] : [], names: [] }]
    : failedRuns(target.checks).slice(0, MAX_RUNS)
  if (runs.length === 0) {
    showToast('No failed GitHub Actions checks to read: open the check on GitHub', 'error')
    return
  }
  const busyKey = `${target.root}#fix`
  set((s) => ({ busy: { ...s.busy, [busyKey]: 'fix' } }))
  try {
    const reports = await Promise.all(runs.map((r) => GitHub.CIFailure(target.root, r.runId, r.jobIds)))
    const git = useGit.getState()
    const workspaceBranch = target.task ? target.task.branch : git.root === target.root ? git.status?.branch : undefined
    const prompt = ciFixPrompt(reports, { pr: target.pr ?? undefined, workspaceBranch })
    if (target.task && !(await showFolder(target.task.worktree))) return
    await askAgent({ prompt, root: target.root })
  } catch (err) {
    showToast(`Couldn't read the CI log: ${errorMessage(err)}`, 'error')
  } finally {
    set((s) => {
      const { [busyKey]: _done, ...busy } = s.busy
      return { busy }
    })
  }
}

/** Opens a run's (or job's) failed-steps log in a tab. */
export function openCILog(root: string, runId: number, jobId: number, label: string): void {
  useEditor.getState().openCILog(root, runId, jobId, `Log: ${label}`)
}

/** Re-runs a run's failed jobs on GitHub after asking. */
export async function rerunFailedJobs(root: string, runId: number, label: string): Promise<void> {
  if (!(await confirmAction(`Re-run the failed jobs of ${label} on GitHub?`, 'Re-run'))) return
  try {
    await GitHub.RerunFailed(root, runId)
    showToast(`Re-running the failed jobs of ${label}`)
    for (const [key, entry] of Object.entries(get().entries)) if (entry.root === root) void refresh(key)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}
