// Agent tasks: an agent on its own branch in its own git worktree (TaskAPI in
// Go). Pure helpers for the Tasks panel and the settings, testable with node:test.
import { isWithin } from './fileTree'
import { formatCost, formatTokens } from './usage'

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
  /** Links the tasks started together from one description ('' for a task on its own). */
  group: string
  /** What the task's chats used so far. */
  tokens: number
  costUsd: number
}

/** The counts of Go's git.DiffStat the panel shows. */
export interface ChangeCounts {
  count: number
  added: number
  deleted: number
}

export type TaskState = 'queued' | 'idle' | 'working' | 'waiting' | 'finished' | 'failed'

const STATES: Record<TaskState, { label: string; hint: string }> = {
  queued: { label: 'queued', hint: 'Waits for a working task to settle: its chat has not started and nothing was sent' },
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
  /** Start a queued task although no slot is free. */
  startNow: boolean
}

/** Which row actions make sense now (Go checks again when one runs). */
export function taskActions(t: TaskView): TaskActions {
  const live = !t.stale
  const busy = t.state === 'working' || t.state === 'waiting'
  const queued = t.state === 'queued'
  const hasWork = live && t.ahead > 0 && !busy
  return {
    // Opening a queued task would start a chat by hand; Start now does it properly.
    open: live && !queued,
    diff: live && !queued,
    merge: hasWork,
    pr: hasWork && !t.pullRequest,
    remove: true,
    askAgent: live && t.state === 'failed',
    startNow: live && queued,
  }
}

/** "12k tokens · $0.42" of a task's chats; '' when it used nothing yet. */
export function formatTaskUsage(t: Pick<TaskView, 'tokens' | 'costUsd'>): string {
  if (t.tokens <= 0 && t.costUsd <= 0) return ''
  const parts = [`${formatTokens(t.tokens)} tokens`]
  if (t.costUsd > 0) parts.push(formatCost(t.costUsd))
  return parts.join(' · ')
}

/** The most tasks one description starts (Go's agenttask.MaxGroup). */
export const MAX_GROUP = 6

/**
 * The agents a new task goes to: every chosen agent, `attempts` times each
 * (agent by agent, so the first attempts of all come first in the queue).
 * Go creates one task per entry.
 */
export function planAgents(chosen: string[], attempts: number): string[] {
  const agents = [...new Set(chosen.filter(Boolean))]
  const n = Math.max(1, Math.floor(attempts) || 1)
  const out: string[] = []
  for (let i = 0; i < n; i++) out.push(...agents)
  return out
}

/** Why a plan cannot start; null when it can. */
export function planProblem(plan: string[]): string | null {
  if (plan.length === 0) return 'Choose an agent.'
  if (plan.length > MAX_GROUP) return `One task starts at most ${MAX_GROUP} agents: choose fewer agents or attempts.`
  return null
}

/** What Start will do, under the form: '' for one task that starts at once. */
export function planNote(plan: string[], maxParallel: number): string {
  if (plan.length <= 1) return ''
  const queued = maxParallel > 0 && plan.length > maxParallel ? ` At most ${maxParallel} work at the same time; the rest wait in the queue.` : ''
  return `Starts ${plan.length} tasks from the same commit, each on its own branch and worktree, to compare.${queued}`
}

/** A row of the panel: a task on its own, or the tasks of one group together. */
export interface TaskGroupRow {
  /** The group id; '' for a task on its own. */
  group: string
  tasks: TaskView[]
}

/**
 * The panel's rows from the task list: tasks of a group are shown together, where
 * the first of them stands in the list, oldest first (the order they were
 * created in, so agents keep their place). A group with one task left is a
 * plain task.
 */
export function groupRows(tasks: TaskView[]): TaskGroupRow[] {
  const members = new Map<string, TaskView[]>()
  for (const t of tasks) if (t.group) members.set(t.group, [...(members.get(t.group) ?? []), t])
  const rows: TaskGroupRow[] = []
  const done = new Set<string>()
  for (const t of tasks) {
    const group = t.group && (members.get(t.group)?.length ?? 0) > 1 ? t.group : ''
    if (!group) {
      rows.push({ group: '', tasks: [t] })
      continue
    }
    if (done.has(group)) continue
    done.add(group)
    // Stable: tasks created in the same second keep the order Go listed them in.
    rows.push({ group, tasks: [...members.get(group)!].sort((a, b) => a.createdAt - b.createdAt) })
  }
  return rows
}

/** The other tasks of t's group. */
export function groupMates(t: TaskView, tasks: TaskView[]): TaskView[] {
  return t.group ? tasks.filter((o) => o.group === t.group && o.id !== t.id) : []
}

/** A short name for a task within its group: the agent, numbered when it has several attempts. */
export function attemptLabels(tasks: TaskView[]): Record<string, string> {
  const total = new Map<string, number>()
  for (const t of tasks) total.set(t.agent, (total.get(t.agent) ?? 0) + 1)
  const seen = new Map<string, number>()
  const out: Record<string, string> = {}
  for (const t of tasks) {
    const n = (seen.get(t.agent) ?? 0) + 1
    seen.set(t.agent, n)
    out[t.id] = (total.get(t.agent) ?? 0) > 1 ? `${t.agent} #${n}` : t.agent
  }
  return out
}

/** "2 working · 1 queued · 1 finished" for a group's header. */
export function groupSummary(tasks: TaskView[]): string {
  const order: string[] = ['working', 'waiting', 'queued', 'finished', 'failed', 'idle', 'stale']
  const counts = new Map<string, number>()
  for (const t of tasks) {
    const key = t.stale ? 'stale' : stateInfo(t.state).state
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  return order
    .filter((k) => counts.has(k))
    .map((k) => `${counts.get(k)} ${k === 'waiting' ? 'waiting for you' : k}`)
    .join(' · ')
}

/** Mirrors Go's agenttask.FileChange. */
export interface FileChange {
  origPath: string
  added: number
  deleted: number
  binary: boolean
  untracked: boolean
}

/** One cell of the comparison table: what a task did to a file. */
export function changeCell(c: FileChange | null | undefined): string {
  if (!c) return '—'
  if (c.binary) return 'bin'
  return `+${c.added} −${c.deleted}`
}

/** The message offering to remove the tasks that were not kept. */
export function removeOthersQuestion(kept: TaskView, others: TaskView[]): string {
  const n = others.length
  return `Kept "${kept.branch}". Remove the other ${n === 1 ? 'task' : `${n} tasks`} of this comparison (${others
    .map((o) => o.branch)
    .join(', ')})? Their chats are stopped and their worktrees deleted; Flint asks again before deleting uncommitted work or an unmerged branch.`
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
  /** Ignored files and folders (.env, build output): deleted with the worktree too. */
  ignored: number
  unmerged: number
}

/** The extra questions removing a task needs: null when nothing would be lost. */
export function removeQuestions(info: RemoveInfo): { discard: string | null; branch: string | null } {
  const lost = [
    info.changes > 0 ? `${info.changes} uncommitted change(s) or new file(s)` : '',
    info.ignored > 0 ? `${info.ignored} ignored file(s) or folder(s), such as .env or build output` : '',
  ].filter(Boolean)
  const discard = lost.length ? `Its worktree has ${lost.join(' and ')}. Delete them with it?` : null
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
  /** How many tasks may work at the same time; more wait in the queue. 0 = no limit. */
  maxParallel: number
}

export const DEFAULT_AGENT_TASKS: AgentTaskSettings = Object.freeze({ worktreeDir: '', branchPrefix: 'agent/', maxParallel: 3 })

/** Go's maxMaxParallel. */
export const MAX_PARALLEL_LIMIT = 32

/** Same rule as Go's checkMaxParallel. */
export function validMaxParallel(n: unknown): n is number {
  return typeof n === 'number' && Number.isInteger(n) && n >= 0 && n <= MAX_PARALLEL_LIMIT
}

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
    maxParallel: validMaxParallel(o.maxParallel) ? o.maxParallel : d.maxParallel,
  }
}
