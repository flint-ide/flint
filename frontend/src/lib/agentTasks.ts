// Agent tasks: an agent on its own branch in its own git worktree (TaskAPI in
// Go). Pure helpers for the Tasks panel and the settings, testable with node:test.
import { isWithin } from './fileTree'

/** Mirrors Go's TaskView. */
export interface TaskView {
  id: string
  title: string
  prompt: string
  agent: string
  branch: string
  base: string
  worktree: string
  checkout: string
  createdAt: number
  pullRequest: string
  state: string
  error: string
  stale: boolean
  ahead: number
  merged: boolean
}

/** The counts of Go's git.DiffStat the panel shows. */
export interface ChangeCounts {
  count: number
  added: number
  deleted: number
}

export type TaskState = 'idle' | 'working' | 'waiting' | 'finished' | 'failed'

const STATES: Record<TaskState, { label: string; hint: string }> = {
  idle: { label: 'idle', hint: 'No turn is running' },
  working: { label: 'working', hint: 'The agent is working' },
  waiting: { label: 'waiting for you', hint: 'The agent asks for your approval in its chat' },
  finished: { label: 'finished', hint: 'The agent finished its turn: review the changes' },
  failed: { label: 'failed', hint: 'The agent stopped with an error' },
}

export function stateInfo(state: string): { state: TaskState; label: string; hint: string } {
  const s = (state in STATES ? state : 'idle') as TaskState
  return { state: s, ...STATES[s] }
}

/** "3 files +12 −4"; '' while unknown. */
export function formatChanges(c: ChangeCounts | undefined): string {
  if (!c) return ''
  if (c.count === 0) return 'no changes'
  return `${c.count} ${c.count === 1 ? 'file' : 'files'} +${c.added} −${c.deleted}`
}

export interface TaskActions {
  open: boolean
  diff: boolean
  merge: boolean
  pr: boolean
  remove: boolean
  /** Hand the failure to the agent. */
  askAgent: boolean
}

/** Which row actions make sense now (Go checks again when one runs). */
export function taskActions(t: TaskView): TaskActions {
  const live = !t.stale
  const busy = t.state === 'working' || t.state === 'waiting'
  const hasWork = live && t.ahead > 0 && !busy
  return {
    open: live,
    diff: live,
    merge: hasWork,
    pr: hasWork && !t.pullRequest,
    remove: true,
    askAgent: live && t.state === 'failed',
  }
}

/** The message a failed task's chat gets (in the draft, for the user to review). */
export function failurePrompt(t: TaskView): string {
  const why = t.error ? `It stopped with this error:\n\n${t.error}\n\n` : 'It stopped with an error. '
  return `You were working on this task: "${t.title}". ${why}Find out what went wrong, fix it if you can, and continue the task.`
}

/** Ids of the tasks whose worktree contains one of the changed paths. */
export function tasksTouchedBy(paths: string[], tasks: TaskView[]): string[] {
  return tasks.filter((t) => paths.some((p) => isWithin(t.worktree, p))).map((t) => t.id)
}

/** One list from the lists of several workspace folders (one repository lists the same tasks), newest first. */
export function mergeTaskLists(lists: TaskView[][]): TaskView[] {
  const byId = new Map<string, TaskView>()
  for (const list of lists) for (const t of list) if (!byId.has(t.id)) byId.set(t.id, t)
  return [...byId.values()].sort((a, b) => b.createdAt - a.createdAt || a.title.localeCompare(b.title))
}

/** Drops the changes of removed tasks; the same object when nothing went. */
export function pruneChanges<T>(changes: Record<string, T>, tasks: TaskView[]): Record<string, T> {
  const ids = new Set(tasks.map((t) => t.id))
  const keys = Object.keys(changes)
  if (keys.every((k) => ids.has(k))) return changes
  return Object.fromEntries(keys.filter((k) => ids.has(k)).map((k) => [k, changes[k]]))
}

/** Mirrors Go's RemoveInfo. */
export interface RemoveInfo {
  stale: boolean
  changes: number
  unmerged: number
}

/** The extra questions removing a task needs: null when nothing would be lost. */
export function removeQuestions(info: RemoveInfo): { discard: string | null; branch: string | null } {
  const discard = info.changes > 0 ? `Its worktree has ${info.changes} uncommitted change(s) or new file(s). Delete them with it?` : null
  let branch: string | null = null
  if (info.unmerged > 0) branch = `The branch has ${info.unmerged} commit(s) that are not in its base. Delete the branch too?`
  if (info.unmerged < 0) branch = 'Flint could not tell whether the branch was merged (its base is gone). Delete the branch too?'
  return { discard, branch }
}

/** Mirrors Go's settings.AgentTasks. */
export interface AgentTaskSettings {
  /** Where task worktrees go ('' = "<repo>.worktrees" next to the main checkout). */
  worktreeDir: string
  /** Starts every task branch, e.g. 'agent/'. */
  branchPrefix: string
}

export const DEFAULT_AGENT_TASKS: AgentTaskSettings = Object.freeze({ worktreeDir: '', branchPrefix: 'agent/' })

/** Same rules as Go's checkBranchPrefix. */
export function validBranchPrefix(p: string): boolean {
  return (
    p.length <= 40 &&
    /^[A-Za-z0-9._/-]*$/.test(p) &&
    !/^[-/.]/.test(p) &&
    !/\.\.|\/\/|\/\./.test(p) &&
    !p.includes('.lock/') &&
    !p.endsWith('.')
  )
}

/** Same rules as Go's checkWorktreeDir. */
export function validWorktreeDir(d: string): boolean {
  if (d === '') return true
  return d.length <= 1024 && !/[\x00-\x1f\x7f]/.test(d) && (d.startsWith('/') || d === '~' || d.startsWith('~/'))
}

export function coerceAgentTasks(v: unknown): AgentTaskSettings {
  if (typeof v !== 'object' || v === null) return DEFAULT_AGENT_TASKS
  const o = v as Record<string, unknown>
  const d = DEFAULT_AGENT_TASKS
  return {
    worktreeDir: typeof o.worktreeDir === 'string' && validWorktreeDir(o.worktreeDir) ? o.worktreeDir : d.worktreeDir,
    branchPrefix: typeof o.branchPrefix === 'string' && validBranchPrefix(o.branchPrefix) ? o.branchPrefix : d.branchPrefix,
  }
}
