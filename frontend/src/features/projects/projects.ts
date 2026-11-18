// Projects (Go: internal/features/projects): pure helpers for the panel and the
// board, testable with node:test. The shapes mirror the Go service's views
// (the generated binding classes fit them structurally).
import { isWithin } from '../../lib/fileTree'
import { jointLead, stateInfo } from '../../lib/agentTasks'

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
  /** The joint group (one agent across repositories) the task belongs to; '' for a task with a chat of its own. */
  joint: string
  /** The lead of its joint group: the chat runs in this task's worktree. */
  lead: boolean
}

/** A waiting start: the user asked to start the item when it is ready (Go: WaitingView). */
export interface WaitingInfo {
  since: string
  /** Why the last automatic start failed ('' when none was tried). */
  reason: string
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
  /** one agent works in all of repos (a joint group) instead of one per repository. */
  together: boolean
  /** The items that must be done first (ids), those of them that are not done, and whether the item may start. */
  after: string[]
  waitingFor: string[]
  ready: boolean
  /** What is wrong with after (an unknown id, the item itself, a cycle); '' when nothing. */
  dependencyProblem: string
  waiting: WaitingInfo | null
}

export interface ProblemInfo {
  file: string
  reason: string
}

export interface ProjectInfo {
  path: string
  name: string
  workspace: boolean
  /** The project's folder is trusted; otherwise get_project leaves it out and Go asks before Delegate. */
  trusted: boolean
  error: string
}

/** The project's context file, PROJECT.md (Go: projects.ContextView). */
export interface ContextInfo {
  /** The file, to open in the editor. */
  path: string
  /** The file is there (Open); otherwise the UI offers Add Project Context…. */
  exists: boolean
  /** Why agents don't get it ('' when they do or it is absent); also one of the problems. */
  problem: string
}

export interface ProjectData {
  path: string
  name: string
  workspace: boolean
  trusted: boolean
  repositories: RepoView[]
  context: ContextInfo
  items: ItemInfo[]
  problems: ProblemInfo[]
  defaultAgent: string
  /** The clone running for this project (null when none). */
  clone: CloneInfo | null
}

export type Status = 'doing' | 'review' | 'todo' | 'done'

/** The panel's order (Go: projects.Statuses). */
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

/** "api, web" for the repositories an item names; "active folder" when it names none. */
export const reposText = (item: Pick<ItemInfo, 'repos'>): string => (item.repos.length ? item.repos.join(', ') : 'active folder')

// ---- Delegate

export type DelegateFix = 'locate' | 'attach' | 'trust' | 'folder' | 'none'

/** Why Delegate would be refused now and what the UI can offer; Go checks again. */
export interface DelegateIssue {
  fix: DelegateFix
  repo: string
  /** The repository's URL (locate). */
  url: string
  /** The checkout the fix is about (attach, trust). */
  folder: string
  message: string
}

/**
 * What stands in the way of Delegate, most urgent first (an empty list: nothing
 * known). The agent comes first, as in Go: it has no fix here. root is the
 * window's active folder, used by an item without repositories.
 */
export function delegateIssues(item: ItemInfo, repos: readonly RepoView[], root: string): DelegateIssue[] {
  if (item.agentProblem) return [{ fix: 'none', repo: '', url: '', folder: '', message: item.agentProblem }]
  if (item.repos.length === 0) {
    return root ? [] : [{ fix: 'folder', repo: '', url: '', folder: '', message: 'The item names no repository, so it runs in the active folder: open the folder it is for first.' }]
  }
  const out: DelegateIssue[] = []
  for (const name of item.repos) {
    const r = repos.find((x) => x.name === name)
    if (!r) out.push({ fix: 'none', repo: name, url: '', folder: '', message: `The item names the repository "${name}", which flint-project.json does not list.` })
    else {
      const issue = repoIssue(r)
      if (issue) out.push(issue)
    }
  }
  return out
}

/** What keeps a repository of the project from being used by Delegate, with its fix; null when it can be used. */
export function repoIssue(r: RepoView): DelegateIssue | null {
  const base = { repo: r.name, url: r.url, folder: r.folder }
  if (r.problem) return { ...base, fix: 'locate', message: `${r.name}: ${r.problem}` }
  if (!r.folder) return { ...base, fix: 'locate', message: `No checkout of ${r.name} was found: locate it.` }
  if (!r.inWorkspace) return { ...base, fix: 'attach', message: `${r.name} (${r.folder}) is not in the workspace yet.` }
  if (!r.trusted) return { ...base, fix: 'trust', message: `${r.name} (${r.folder}) is in Restricted Mode: trust the folder first.` }
  return null
}

/** The next step of Delegate's walk-through: delegate, fix an issue, or stop with the reason. */
export type DelegateStep = { kind: 'delegate' } | { kind: 'fix'; issue: DelegateIssue; sig: string } | { kind: 'stuck'; message: string }

/**
 * Decides the walk-through's next step from the issues left (delegateIssues) and
 * the signature of the issue the previous step fixed ('' before the first): an
 * issue with no fix here, or the same issue again (the fix made no progress),
 * stops it with the issue's message.
 */
export function delegateStep(issues: readonly DelegateIssue[], last: string): DelegateStep {
  const [issue] = issues
  if (!issue) return { kind: 'delegate' }
  const sig = `${issue.fix}:${issue.repo}:${issue.folder}`
  if (issue.fix === 'none' || issue.fix === 'folder' || sig === last) return { kind: 'stuck', message: `Delegate can't go on: ${issue.message}` }
  return { kind: 'fix', issue, sig }
}

/** The question before the folder picker of Locate…: which repository the user is asked to find. */
export function locateQuestion(repo: string, url: string, note = ''): string {
  return [
    `Locate your checkout of "${repo}"`,
    '',
    `Flint needs a folder on this computer that is a git checkout of ${url} (a remote with that URL).`,
    'It is remembered on this computer only, not in the project.',
    ...(note ? ['', note] : []),
  ].join('\n')
}

/** Delegate's question for a repository with no checkout: locate one, or clone it. */
export function noCheckoutQuestion(repo: string, url: string, note = ''): string {
  return [
    `There is no checkout of "${repo}" on this computer`,
    '',
    `${url}`,
    '',
    'Locate… takes a folder that already is a checkout of it. Clone… asks for a folder to clone it into, then shows the URL and the destination once more before it runs git.',
    ...(note ? ['', note] : []),
  ].join('\n')
}

/** The sentence about an item that runs as one agent; '' for any other. */
export function togetherNote(item: Pick<ItemInfo, 'together' | 'repos'>): string {
  return item.together && item.repos.length > 1 ? `It runs as one agent across the repositories ${item.repos.join(', ')}: every one of them must be usable before it starts.` : ''
}

/** The question before trusting a repository's folder: trust is for the whole folder, from now on. */
export function trustQuestion(repo: string, folder: string, note = ''): string {
  return [
    `Trust the folder ${folder}?`,
    '',
    `${repo} is checked out there, and it is in Restricted Mode, so no agent can start in it.`,
    'Trusting it turns Restricted Mode off for the whole folder from now on, not just for this Delegate: its hooks, settings and language servers run, and agents use the agent settings it holds. Trust it only if you trust its authors. Revoke Folder Trust turns it back on.',
    ...(note ? ['', note] : []),
  ].join('\n')
}

/** The question before trusting a project's own folder. */
export function trustProjectQuestion(name: string, folder: string): string {
  return [
    `Trust the folder ${folder}?`,
    '',
    `It holds the project "${name}". Trusted, agents can read its items with get_project, and Delegate stops asking before it sends an item.`,
    'Trust is for the whole folder from now on: its hooks, settings and language servers run too. Trust it only if you trust its authors.',
  ].join('\n')
}

/** A marker of a repository row: what keeps it from being used, or where its checkout is. */
export interface RepoMarker {
  label: string
  cls: 'error' | 'warn' | 'muted'
  hint: string
}

/** The markers of a repository in the Repositories group. */
export function repoMarkers(r: RepoView): RepoMarker[] {
  if (r.problem) return [{ label: 'problem', cls: 'error', hint: r.problem }]
  if (!r.folder) return [{ label: 'no checkout', cls: 'error', hint: 'No folder open in the window and no located folder has a remote with this URL: Locate… it' }]
  const out: RepoMarker[] = []
  if (r.located) out.push({ label: 'located', cls: 'muted', hint: 'Chosen with Locate… (remembered on this computer)' })
  if (!r.inWorkspace) out.push({ label: 'not in workspace', cls: 'warn', hint: 'Delegate adds the folder to the workspace first' })
  if (!r.trusted) out.push({ label: 'Restricted Mode', cls: 'warn', hint: 'No agent can start here until you trust the folder' })
  return out
}

/** Whether every repository of the project can be used by Delegate now (the Repositories group starts collapsed then). */
export const allReposResolve = (repos: readonly RepoView[]): boolean => repos.every((r) => repoIssue(r) === null)

// ---- Agent tasks of an item

/** The state pill of an item's agent task: the Agent Tasks panel's states, plus merged and stale. */
export function taskPill(t: TaskInfo): { cls: string; label: string; hint: string } {
  const s = stateInfo(t.state)
  if (t.merged) return { cls: 'finished', label: 'merged', hint: 'Merged into its base branch' }
  if (t.stale && !t.gone) return { cls: 'stale', label: 'stale', hint: 'The worktree folder is gone' }
  return { cls: s.state, label: s.label, hint: t.error || s.hint }
}

/**
 * Open finds the task's folder and chat; a removed task, a missing folder and a queued task have
 * none. A member of a joint group has no chat of its own: Open goes to its lead (in all, the
 * item's tasks), so it is as good as the lead is.
 */
export function canOpenTask(t: TaskInfo, all: readonly TaskInfo[] = [t]): boolean {
  const target = jointLead(t, all)
  if (t.joint && !target.lead) return false
  return !target.gone && !target.stale && target.state !== 'queued'
}

/** A line of an item's task list: a task on its own, or the tasks of one joint group (lead first). */
export interface TaskRow {
  /** The joint group's id; '' for a task on its own. */
  joint: string
  tasks: TaskInfo[]
}

/**
 * The item's tasks as rows: the tasks of one joint group together, where the first of them stands, the
 * lead first; the others in the order Go listed them. A group with one task listed is a plain task.
 */
export function groupTasks(tasks: readonly TaskInfo[]): TaskRow[] {
  const members = new Map<string, TaskInfo[]>()
  for (const t of tasks) if (t.joint) members.set(t.joint, [...(members.get(t.joint) ?? []), t])
  const rows: TaskRow[] = []
  const seen = new Set<string>()
  for (const t of tasks) {
    const all = t.joint ? members.get(t.joint)! : []
    if (all.length < 2) {
      rows.push({ joint: '', tasks: [t] })
      continue
    }
    if (seen.has(t.joint)) continue
    seen.add(t.joint)
    rows.push({ joint: t.joint, tasks: [...all.filter((m) => m.lead), ...all.filter((m) => !m.lead)] })
  }
  return rows
}

/** The repositories of a joint group, lead first: "api, web". */
export const jointRepos = (tasks: readonly TaskInfo[]): string => tasks.map(taskRepo).join(', ')

/** What a joint group's tasks have in common, said once above them. */
export const jointHint = (tasks: readonly TaskInfo[]): string =>
  `One agent works in the worktrees of all ${tasks.length} tasks (${jointRepos(tasks)}). Its one chat is in the lead's worktree; Open on any of them shows it.`

/** "api" for a task in a repository of the project, "active folder" otherwise. */
export const taskRepo = (t: Pick<TaskInfo, 'repo'>): string => t.repo || 'active folder'

// ---- Which project is selected

/** The project to show: the wanted one when it is still known, else the first. */
export function pickSelected(infos: readonly ProjectInfo[], wanted: string): string {
  return infos.find((i) => i.path === wanted)?.path ?? infos[0]?.path ?? ''
}

/** The Close Project action applies to remembered projects only (a workspace folder's goes with the folder). */
export const canClose = (info: ProjectInfo | undefined): boolean => !!info && !info.workspace

// ---- Files that change a project

/** Whether a changed file is one the project reads: flint-project.json or a file in items/. */
export function touchesProject(folder: string, paths: readonly string[]): boolean {
  if (!folder) return false
  const config = `${folder.replace(/\/+$/, '')}/flint-project.json`
  const items = `${folder.replace(/\/+$/, '')}/items`
  return paths.some((p) => p === config || isWithin(items, p))
}

// ---- Board

export const BOARD_VIEW = 'projects.board'

/** The data of a board tab: its project folder. */
export interface BoardData {
  folder: string
}

/** Tab data from a saved session (any JSON): the folder, or null when it is not a board's data. */
export function coerceBoardData(data: unknown): BoardData | null {
  if (typeof data !== 'object' || data === null) return null
  const folder = (data as { folder?: unknown }).folder
  return typeof folder === 'string' && folder.startsWith('/') && folder.length < 4096 ? { folder } : null
}

/** What a board shows instead of columns: null when the project is known and readable (infos null: not listed yet). */
export function boardProblem(folder: string, infos: readonly ProjectInfo[] | null): string | null {
  if (infos === null) return null
  const info = infos.find((i) => i.path === folder)
  if (!info) return `The project ${folder} is not known any more: it was closed, or its folder left the workspace. Open it again with Open Project….`
  return info.error ? `The project cannot be read: ${info.error}` : null
}

export const boardTitle = (name: string): string => `Project: ${name}`

/** What a dragged card carries (a card of this project only). */
export const DRAG_TYPE = 'application/x-flint-project-item'

export function dragPayload(folder: string, item: Pick<ItemInfo, 'id' | 'status'>): string {
  return JSON.stringify({ folder, id: item.id, status: item.status })
}

export interface Drop {
  id: string
  status: string
}

/** A dropped payload (untrusted text) when it is a card of this project, else null. */
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

/** The message that asks the agent to fix a problem file of the project. */
export function problemPrompt(projectName: string, projectPath: string, p: ProblemInfo): string {
  return [
    `Flint cannot use a file of the project "${projectName}" (${projectPath}):`,
    '',
    `${p.file}: ${p.reason}`,
    '',
    p.file === CONTEXT_FILE
      ? `Fix the file so that Flint can give it to agents again: plain Markdown in UTF-8, at most ${MAX_CONTEXT_CHARS.toLocaleString('en-US')} characters, without control characters (other than tab and newline) or invisible ones, and not a link to a file outside the project's folder. Keep what it says.`
      : 'Fix the file so that it follows the format of a project item (front matter with title, status, priority, agent and repos, then the text), and keep what it says.',
  ].join('\n')
}

// ---- Project context

/** The project's context file (Go: projects.ContextFile). */
export const CONTEXT_FILE = 'PROJECT.md'
/** The characters of it agents get at most (Go: projects.MaxContextChars). */
export const MAX_CONTEXT_CHARS = 16000

/** What the Context line of the panel and the board says, and the action it offers. */
export interface ContextLine {
  text: string
  hint: string
  /** 'open': Open the file; 'add': Add Project Context…; null: nothing (an untrusted project without one). */
  action: 'open' | 'add' | null
  problem: boolean
}

export function contextLine(c: ContextInfo | undefined, trusted: boolean): ContextLine {
  if (c?.exists && c.problem) {
    return { text: `${CONTEXT_FILE} (not sent)`, hint: `Agents don't get ${CONTEXT_FILE} until it is fixed: ${c.problem}`, action: 'open', problem: true }
  }
  if (c?.exists) {
    return { text: CONTEXT_FILE, hint: `Every agent Delegate starts gets ${CONTEXT_FILE} at the start of its first message, with where its item stands in the project`, action: 'open', problem: false }
  }
  return {
    text: 'none',
    hint: trusted
      ? `Agents get their item and where it stands in the project. Add ${CONTEXT_FILE} to tell every one of them what the project is for, how its repositories fit together and when work is done`
      : `This project has no ${CONTEXT_FILE}. Trust the project's folder to add one`,
    action: trusted ? 'add' : null,
    problem: false,
  }
}

// ---- Dependencies

/** The title of the item with this id; the id itself when the project has no such item (yet). */
export function itemTitleOf(id: string, items: readonly Pick<ItemInfo, 'id' | 'title'>[]): string {
  return items.find((i) => i.id === id)?.title ?? id
}

/** The titles of the items with these ids (ids for the unknown ones), in the order given. */
export const titlesOf = (ids: readonly string[], items: readonly Pick<ItemInfo, 'id' | 'title'>[]): string[] => ids.map((id) => itemTitleOf(id, items))

/** "a", "a and b", "a, b and c". */
export function joinList(names: readonly string[]): string {
  if (names.length <= 1) return names.join('')
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
}

/** "A is done" / "A and B are done". */
export const areDone = (names: readonly string[]): string => `${joinList(names)} ${names.length === 1 ? 'is' : 'are'} done`

/** A marker next to an item's title or repositories (same look as the repository markers). */
export type ItemMarker = RepoMarker

/**
 * The marker of an item that cannot start: a problem with its after line, a start that waits, or just
 * not ready. None for a ready item and for a done one (what it waited for no longer matters).
 */
export function dependencyMarker(item: ItemInfo, items: readonly ItemInfo[]): ItemMarker | null {
  if (item.dependencyProblem) return { label: 'dependency problem', cls: 'error', hint: item.dependencyProblem }
  if (item.status === 'done') return null
  const on = joinList(titlesOf(item.waitingFor, items))
  if (item.waiting) return { label: 'waiting to start', cls: 'warn', hint: `Starts by itself when ${areDone(titlesOf(item.waitingFor, items))}` }
  if (!item.ready) return { label: 'not ready', cls: 'warn', hint: `Waits for ${on}: Delegate offers to start it when it is done` }
  return null
}

/** "one agent": the marker of an item that runs as one agent across its repositories. */
export function togetherMarker(item: Pick<ItemInfo, 'together' | 'repos'>): ItemMarker | null {
  return item.together
    ? { label: 'one agent', cls: 'muted', hint: `Delegate starts one agent that works in all of ${item.repos.join(', ')}, in one chat, instead of one agent per repository` }
    : null
}

/** A line under an item: what it waits for, its dependency problem, its waiting start. */
export interface DependencyLine {
  kind: 'after' | 'problem' | 'waiting' | 'reason'
  text: string
  cls: 'muted' | 'warn' | 'error'
}

/** The dependency lines of an item, in the order they are shown. */
export function dependencyLines(item: ItemInfo, items: readonly ItemInfo[]): DependencyLine[] {
  const out: DependencyLine[] = []
  if (item.dependencyProblem) out.push({ kind: 'problem', text: item.dependencyProblem, cls: 'error' })
  if (item.after.length > 0 && item.status !== 'done' && !item.dependencyProblem) {
    out.push(
      item.waitingFor.length > 0
        ? { kind: 'after', text: `waits for ${joinList(titlesOf(item.waitingFor, items))}`, cls: 'warn' }
        : { kind: 'after', text: `after ${joinList(titlesOf(item.after, items))} (done)`, cls: 'muted' },
    )
  }
  if (item.waiting) {
    out.push({
      kind: 'waiting',
      text: item.waitingFor.length > 0 ? `starts when ${areDone(titlesOf(item.waitingFor, items))}` : 'ready: starts as soon as its checks pass',
      cls: 'muted',
    })
    if (item.waiting.reason) out.push({ kind: 'reason', text: `Last start failed: ${item.waiting.reason}`, cls: 'error' })
  }
  return out
}

/**
 * Delegate's question for an item that is not ready (Go answered notReady and what it waits for): start it
 * when those items are done. waitingFor is Go's answer (ids); an item that runs as one agent says so.
 */
export function notReadyQuestion(item: Pick<ItemInfo, 'title' | 'together' | 'repos'>, waitingFor: readonly string[], items: readonly ItemInfo[]): string {
  const together = item.together && item.repos.length > 1 ? `It runs as one agent across the repositories ${item.repos.join(', ')}.` : ''
  return [
    `Start "${item.title}" when ${areDone(titlesOf(waitingFor, items))}?`,
    ...(together ? ['', together] : []),
    '',
    'Nothing starts now. Flint then delegates it by itself, with every check Delegate makes at that time (the agent, the checkouts, trust). Cancel start takes it back.',
  ].join('\n')
}

/** The toast after the user said yes to the question above. */
export const waitingToast = (title: string, waitingFor: readonly string[], items: readonly ItemInfo[]): string =>
  `"${title}" starts when ${areDone(titlesOf(waitingFor, items))}`

/** "Started 2 agent tasks", "Started one agent across 2 repositories" for a joint group. */
export function startedText(item: Pick<ItemInfo, 'together'>, tasks: number): string {
  if (item.together && tasks > 1) return `Started one agent across ${tasks} repositories`
  return `Started ${tasks} agent ${tasks === 1 ? 'task' : 'tasks'}`
}

// ---- Events of Go

/** The data of "projects:auto-start" (Go: AutoStart). */
export interface AutoStart {
  project: string
  id: string
  title: string
  tasks: number
  error: string
  warning: string
}

/** An event's data (untrusted) as an AutoStart, or null. */
export function coerceAutoStart(raw: unknown): AutoStart | null {
  if (typeof raw !== 'object' || raw === null) return null
  const o = raw as Record<string, unknown>
  if (typeof o.project !== 'string' || typeof o.id !== 'string') return null
  return {
    project: o.project,
    id: o.id,
    title: typeof o.title === 'string' && o.title ? o.title : o.id,
    tasks: typeof o.tasks === 'number' && Number.isFinite(o.tasks) ? o.tasks : 0,
    error: typeof o.error === 'string' ? o.error : '',
    warning: typeof o.warning === 'string' ? o.warning : '',
  }
}

/** The toast of an automatic start: started (with how many tasks), or why not (the item keeps waiting). */
export function autoStartToast(a: AutoStart): { message: string; kind: 'info' | 'error' } {
  if (a.error) return { message: `Couldn't start "${a.title}": ${a.error} It keeps waiting.`, kind: 'error' }
  const base = `Started ${a.title} (${a.tasks} agent ${a.tasks === 1 ? 'task' : 'tasks'})`
  return a.warning ? { message: `${base}, but ${a.warning}`, kind: 'error' } : { message: base, kind: 'info' }
}

// ---- Cloning

/** The clone running for a project (Go: CloneView). */
export interface CloneInfo {
  repo: string
  url: string
  destination: string
  progress: string
}

/** What the panel shows of a running clone. */
export interface CloneState {
  repo: string
  destination: string
  progress: string
}

/** The data of "projects:clone-progress" and "projects:clone-done" (Go: CloneEvent). */
export interface CloneEvent {
  project: string
  repo: string
  destination: string
  progress: string
  done: boolean
  error: string
  cancelled: boolean
}

/** The state of a clone from the project's own view of it (Project.clone); null when none runs. */
export const cloneFromInfo = (c: Pick<CloneInfo, 'repo' | 'destination' | 'progress'> | null | undefined): CloneState | null =>
  c ? { repo: c.repo, destination: c.destination, progress: c.progress } : null

/** An event's data (untrusted) as a CloneEvent, or null. */
export function coerceCloneEvent(raw: unknown): CloneEvent | null {
  if (typeof raw !== 'object' || raw === null) return null
  const o = raw as Record<string, unknown>
  if (typeof o.project !== 'string' || typeof o.repo !== 'string') return null
  const str = (v: unknown) => (typeof v === 'string' ? v : '')
  return {
    project: o.project,
    repo: o.repo,
    destination: str(o.destination),
    progress: str(o.progress),
    done: o.done === true,
    error: str(o.error),
    cancelled: o.cancelled === true,
  }
}

/** The state after an event: a progress line updates the clone, the end clears it. */
export function applyCloneEvent(cur: CloneState | null, ev: CloneEvent): CloneState | null {
  if (ev.done) return null
  return { repo: ev.repo, destination: ev.destination || cur?.destination || '', progress: ev.progress || (cur?.repo === ev.repo ? cur.progress : '') }
}

/** The line of the panel while a clone runs: "Cloning api into /w/api". */
export const cloneTitle = (c: CloneState): string => (c.destination ? `Cloning ${c.repo} into ${c.destination}` : `Cloning ${c.repo}…`)

/** The toast after a clone: where it went, and whether it is trusted (inside a trusted folder) or in Restricted Mode. */
export const clonedText = (repo: string, r: Pick<RepoView, 'folder' | 'trusted'>): string =>
  r.trusted
    ? `Cloned ${repo} into ${r.folder}: it is trusted, because it is inside a trusted folder`
    : `Cloned ${repo} into ${r.folder}: it is in Restricted Mode until you trust it`

/** A rejected Clone call: cancelling is not an error to show in red. */
export function cloneFailure(repo: string, message: string): { message: string; kind: 'info' | 'error' } {
  return /clone was cancelled/i.test(message) ? { message: `The clone of ${repo} was cancelled`, kind: 'info' } : { message, kind: 'error' }
}

/** Whether Clone… is offered for a repository: it has no checkout at all (Locate… stays for the rest). */
export const canClone = (r: Pick<RepoView, 'folder'>): boolean => !r.folder

// ---- Planning with an agent

/** Go's MaxGoal. */
export const MAX_GOAL = 2000

/** The goal's length in characters, as Go counts them. */
export const goalLength = (goal: string): number => Array.from(goal).length

/** Why a goal cannot be sent; null when it can. */
export function goalProblem(goal: string): string | null {
  if (goal.trim() === '') return 'Say what the goal is.'
  if (goalLength(goal) > MAX_GOAL) return `At most ${MAX_GOAL} characters.`
  return null
}

/** Go refuses the plan of a project whose folder is in Restricted Mode: the dialog then offers Trust Folder…. */
export const isRestrictedError = (message: string): boolean => /Restricted Mode/.test(message)

/**
 * What to tell the user when the planning tool is off (PlanPrompt.toolOn false): the agent gets the
 * prompt but can only add items once the switch is on. The names are the Settings labels.
 */
export const PLAN_TOOL_OFF =
  'The prompt is in the chat draft. The agent can only add items after you switch on Add project items in Settings ▸ Agent Context ▸ Actions (and Share Flint\'s context with agents).'

/** The agent the Plan with Agent… dialog starts with: the default agent, else the preferred one, else the first. */
export function pickPlanAgent(agents: readonly string[], defaultAgent: string, preferred: string): string {
  return [defaultAgent, preferred].find((a) => a && agents.includes(a)) ?? agents[0] ?? ''
}
