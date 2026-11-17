// Backlog (Go: internal/features/backlog): pure helpers for the panel and the
// board, testable with node:test. The shapes mirror the Go service's views
// (the generated binding classes fit them structurally).
import { isWithin } from '../../lib/fileTree'
import { stateInfo } from '../../lib/agentTasks'

export interface RepoView {
  name: string
  url: string
  folder: string
  located: boolean
  inWorkspace: boolean
  trusted: boolean
  problem: string
}

export interface TaskInfo {
  id: string
  repo: string
  title: string
  agent: string
  branch: string
  worktree: string
  state: string
  error: string
  pullRequest: string
  merged: boolean
  stale: boolean
  gone: boolean
}

export interface ItemInfo {
  id: string
  title: string
  status: string
  priority: string
  agent: string
  repos: string[]
  body: string
  path: string
  agentProblem: string
  failed: boolean
  tasks: TaskInfo[]
}

export interface ProblemInfo {
  file: string
  reason: string
}

export interface BacklogInfo {
  path: string
  name: string
  workspace: boolean
  error: string
}

export interface BacklogData {
  path: string
  name: string
  workspace: boolean
  repositories: RepoView[]
  items: ItemInfo[]
  problems: ProblemInfo[]
  defaultAgent: string
}

export type Status = 'doing' | 'review' | 'todo' | 'done'

/** The panel's order (Go: backlog.Statuses). */
export const PANEL_STATUSES: readonly Status[] = ['doing', 'review', 'todo', 'done']
/** The board's columns, left to right: the order work flows in. */
export const BOARD_STATUSES: readonly Status[] = ['todo', 'doing', 'review', 'done']

const STATUS_LABELS: Record<Status, string> = { todo: 'To do', doing: 'Doing', review: 'Review', done: 'Done' }

export const isStatus = (s: string): s is Status => (PANEL_STATUSES as readonly string[]).includes(s)
export const statusLabel = (s: string): string => (isStatus(s) ? STATUS_LABELS[s] : s)

/** The items of each status, in the order they arrived (Go sorts by priority, then title). */
export function groupByStatus(items: readonly ItemInfo[]): Record<Status, ItemInfo[]> {
  const out: Record<Status, ItemInfo[]> = { doing: [], review: [], todo: [], done: [] }
  for (const it of items) out[isStatus(it.status) ? it.status : 'todo'].push(it)
  return out
}

/** The statuses an item can be moved to from its menu (every other one). */
export const otherStatuses = (current: string, order: readonly Status[] = BOARD_STATUSES): Status[] => order.filter((s) => s !== current)

export const PRIORITY_LABELS: Record<string, string> = { high: 'high', normal: 'normal', low: 'low' }

/** The agent an item runs with: its own, else the default one ("default: claude"; none when no agent can start). */
export function agentText(item: Pick<ItemInfo, 'agent'>, defaultAgent: string, label: (name: string) => string = (n) => n): string {
  if (item.agent) return label(item.agent)
  return defaultAgent ? `default: ${label(defaultAgent)}` : 'default: no agent available'
}

/** "api, web" for the repositories an item names; "active project" when it names none. */
export const reposText = (item: Pick<ItemInfo, 'repos'>): string => (item.repos.length ? item.repos.join(', ') : 'active project')

// ---- Delegate

export type DelegateFix = 'locate' | 'attach' | 'trust' | 'project' | 'none'

/** Why Delegate would be refused now and what the UI can offer; Go checks again. */
export interface DelegateIssue {
  fix: DelegateFix
  repo: string
  /** The checkout the fix is about (attach, trust). */
  folder: string
  message: string
}

/**
 * What stands in the way of Delegate, most urgent first (an empty list: nothing
 * known). The agent comes first, as in Go: it has no fix here. project is the
 * window's active project, used by an item without repositories.
 */
export function delegateIssues(item: ItemInfo, repos: readonly RepoView[], project: string): DelegateIssue[] {
  if (item.agentProblem) return [{ fix: 'none', repo: '', folder: '', message: item.agentProblem }]
  if (item.repos.length === 0) {
    return project ? [] : [{ fix: 'project', repo: '', folder: '', message: 'The item names no repository, so it runs in the active project: open that project first.' }]
  }
  const out: DelegateIssue[] = []
  for (const name of item.repos) {
    const r = repos.find((x) => x.name === name)
    if (!r) out.push({ fix: 'none', repo: name, folder: '', message: `The item names the repository "${name}", which flint-backlog.json does not list.` })
    else if (r.problem) out.push({ fix: 'locate', repo: name, folder: r.folder, message: `${name}: ${r.problem}` })
    else if (!r.folder) out.push({ fix: 'locate', repo: name, folder: '', message: `No checkout of ${name} was found: locate it.` })
    else if (!r.inWorkspace) out.push({ fix: 'attach', repo: name, folder: r.folder, message: `${name} (${r.folder}) is not in the workspace yet.` })
    else if (!r.trusted) out.push({ fix: 'trust', repo: name, folder: r.folder, message: `${name} (${r.folder}) is in Restricted Mode: trust the folder first.` })
  }
  return out
}

// ---- Agent tasks of an item

/** The state pill of an item's agent task: the Agent Tasks panel's states, plus merged and stale. */
export function taskPill(t: TaskInfo): { cls: string; label: string; hint: string } {
  const s = stateInfo(t.state)
  if (t.merged) return { cls: 'finished', label: 'merged', hint: 'Merged into its base branch' }
  if (t.stale && !t.gone) return { cls: 'stale', label: 'stale', hint: 'The worktree folder is gone' }
  return { cls: s.state, label: s.label, hint: t.error || s.hint }
}

/** Open finds the task's folder and chat; a removed task, a missing folder and a queued task have none. */
export const canOpenTask = (t: TaskInfo): boolean => !t.gone && !t.stale && t.state !== 'queued'

/** "api" for a task in a repository of the backlog, "active project" otherwise. */
export const taskRepo = (t: Pick<TaskInfo, 'repo'>): string => t.repo || 'active project'

// ---- Which backlog is selected

/** The backlog to show: the wanted one when it is still known, else the first. */
export function pickSelected(infos: readonly BacklogInfo[], wanted: string): string {
  return infos.find((i) => i.path === wanted)?.path ?? infos[0]?.path ?? ''
}

/** The Close Backlog action applies to remembered backlogs only (a workspace folder's goes with the folder). */
export const canClose = (info: BacklogInfo | undefined): boolean => !!info && !info.workspace

// ---- Files that change a backlog

/** Whether a changed file is one the backlog reads: flint-backlog.json or a file in items/. */
export function touchesBacklog(folder: string, paths: readonly string[]): boolean {
  if (!folder) return false
  const config = `${folder.replace(/\/+$/, '')}/flint-backlog.json`
  const items = `${folder.replace(/\/+$/, '')}/items`
  return paths.some((p) => p === config || isWithin(items, p))
}

// ---- Board

export const BOARD_VIEW = 'backlog.board'

/** The data of a board tab: its backlog folder. */
export interface BoardData {
  folder: string
}

/** Tab data from a saved session (any JSON): the folder, or null when it is not a board's data. */
export function coerceBoardData(data: unknown): BoardData | null {
  if (typeof data !== 'object' || data === null) return null
  const folder = (data as { folder?: unknown }).folder
  return typeof folder === 'string' && folder.startsWith('/') && folder.length < 4096 ? { folder } : null
}

/** What a board shows instead of columns: null when the backlog is known and readable (infos null: not listed yet). */
export function boardProblem(folder: string, infos: readonly BacklogInfo[] | null): string | null {
  if (infos === null) return null
  const info = infos.find((i) => i.path === folder)
  if (!info) return `The backlog ${folder} is not known any more: it was closed, or its folder left the workspace. Open it again with Open Backlog….`
  return info.error ? `The backlog cannot be read: ${info.error}` : null
}

export const boardTitle = (name: string): string => `Backlog: ${name}`

/** What a dragged card carries (a card of this backlog only). */
export const DRAG_TYPE = 'application/x-flint-backlog-item'

export function dragPayload(folder: string, item: Pick<ItemInfo, 'id' | 'status'>): string {
  return JSON.stringify({ folder, id: item.id, status: item.status })
}

export interface Drop {
  id: string
  status: string
}

/** A dropped payload (untrusted text) when it is a card of this backlog, else null. */
export function parseDrop(raw: string, folder: string): Drop | null {
  try {
    const v: unknown = JSON.parse(raw)
    if (typeof v !== 'object' || v === null) return null
    const { folder: f, id, status } = v as Record<string, unknown>
    return f === folder && typeof id === 'string' && id !== '' && typeof status === 'string' ? { id, status } : null
  } catch {
    return null
  }
}

/** The status a drop on a column sets; null when nothing changes (the same column, an unknown one). */
export function dropStatus(from: string, to: string): Status | null {
  return isStatus(to) && to !== from ? to : null
}

/** The message that asks the agent to fix a problem file of the backlog. */
export function problemPrompt(backlogName: string, backlogPath: string, p: ProblemInfo): string {
  return [
    `Flint cannot use a file of the backlog "${backlogName}" (${backlogPath}):`,
    '',
    `${p.file}: ${p.reason}`,
    '',
    'Fix the file so that it follows the backlog format (front matter with title, status, priority, agent and repos, then the text), and keep what it says.',
  ].join('\n')
}
