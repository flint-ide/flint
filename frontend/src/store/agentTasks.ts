// Agent tasks (TaskAPI in Go): the Tasks panel's list, the New Agent Task dialog
// and the row actions. State changes come as "tasks:changed" events (derived in
// Go from the chats' agent events); changed-file counts follow git and file events.
// Go also keeps the queue: it says which new tasks start now and announces each
// queued one with "tasks:start" when a slot is free; this store starts the chats.
import { createStore } from '../lib/store'
import * as TaskAPI from '../../bindings/flint/taskapi'
import { EventsOn } from '../wails/runtime'
import { git, main } from '../wails/models'
import { errorMessage } from '../api/backend'
import {
  failurePrompt,
  groupMates,
  mergeTaskLists,
  pruneChanges,
  removeOthersQuestion,
  removeQuestions,
  tasksTouchedBy,
  type TaskView,
} from '../lib/agentTasks'
import { isWithin } from '../lib/fileTree'
import { askAgent } from '../lib/askAgent'
import { confirmAction, showToast } from './dialog'
import { requestGitHubSignIn } from './accounts'
import { isSignInError } from '../lib/githubSettings'
import { useWorkspace } from './workspace'
import { useEditor } from './editor'
import { chatTabId, useAgentChat } from './agentChat'
import { resolveConflictsWithAgent } from './agent'
import { activeRoot } from './activeRoot'

export type TaskChanges = git.DiffStat
export type TaskComparison = main.TaskComparison
export type FileComparison = main.FileComparison
export type TaskFileDiff = main.TaskFileDiff

/** The comparison of a group of tasks that is open (components/tasks/CompareView.tsx). */
export interface CompareState {
  group: string
  data: TaskComparison | null
  /** The selected file and its diff in every task. */
  path: string | null
  file: FileComparison | null
  /** The two tasks of the direct diff (ids), and that diff of the selected file. */
  pair: [string, string] | null
  between: TaskFileDiff | null
  error: string | null
}

interface TasksState {
  tasks: TaskView[]
  /** What each task changed since its base, once loaded. */
  changes: Record<string, TaskChanges>
  /** Tasks whose changed files are listed in the panel. */
  expanded: Record<string, boolean>
  /** Tasks with an action running (merge, pull request, removal). */
  busy: Record<string, boolean>
  error: string | null
  newTaskOpen: boolean
  compare: CompareState | null
}

export const useTasks = createStore<TasksState>(() => ({
  tasks: [],
  changes: {},
  expanded: {},
  busy: {},
  error: null,
  newTaskOpen: false,
  compare: null,
}))

/** Lists the tasks of every repository open in the window (Go reads each repository once). */
export async function loadTasks(): Promise<void> {
  const roots = useWorkspace.getState().roots
  try {
    const tasks = mergeTaskLists([((await TaskAPI.ListAll(roots)) ?? []) as TaskView[]])
    useTasks.setState((s) => ({ tasks, changes: pruneChanges(s.changes, tasks), error: null }))
  } catch (err) {
    useTasks.setState({ error: errorMessage(err) })
  }
}

/** Re-reads what the given tasks (default: all live ones) changed. */
export async function loadChanges(ids?: string[]): Promise<void> {
  const live = useTasks.getState().tasks.filter((t) => !t.stale && (!ids || ids.includes(t.id)))
  await Promise.all(
    live.map(async (t) => {
      try {
        const stat = await TaskAPI.Changes(t.id)
        useTasks.setState((s) => ({ changes: { ...s.changes, [t.id]: stat } }))
      } catch {
        // A worktree removed meanwhile: the next list shows it as stale.
      }
    }),
  )
}

/** Edits and commits come in bursts (an agent at work): counts wait until they settle. */
const SETTLE_MS = 800

/**
 * Follows task, git and file events while the panel is shown; returns the
 * unsubscribe. No timer polls: every refresh answers an event.
 */
export function followTasks(): () => void {
  let listTimer: ReturnType<typeof setTimeout> | undefined
  let changeTimer: ReturnType<typeof setTimeout> | undefined
  const pending = new Set<string>()
  const relist = () => {
    clearTimeout(listTimer)
    listTimer = setTimeout(() => void loadTasks(), 100)
  }
  const recount = (ids: string[]) => {
    ids.forEach((id) => pending.add(id))
    clearTimeout(changeTimer)
    changeTimer = setTimeout(() => {
      const batch = [...pending]
      pending.clear()
      void loadChanges(batch)
    }, SETTLE_MS)
  }
  const all = () => useTasks.getState().tasks.map((t) => t.id)
  const offs = [
    // Usage of a turn arrives after its end: the tokens of a task follow it.
    EventsOn('usage:changed', relist),
    EventsOn('tasks:changed', relist),
    // A commit, merge or checkout anywhere changes ahead/merged and the counts.
    EventsOn('git:changed', () => {
      relist()
      recount(all())
    }),
    EventsOn('fs:changed', (paths: string[]) => {
      const ids = tasksTouchedBy(paths, useTasks.getState().tasks)
      if (ids.length) recount(ids)
    }),
  ]
  void loadTasks().then(() => loadChanges())
  const offRoots = useWorkspace.subscribe((s, prev) => {
    if (s.roots !== prev.roots) relist()
  })
  return () => {
    clearTimeout(listTimer)
    clearTimeout(changeTimer)
    offs.forEach((off) => off())
    offRoots()
  }
}

/** Opens the New Agent Task dialog (in the Tasks panel). */
export function openNewTask(): void {
  useWorkspace.getState().setSidebar('tasks')
  useTasks.setState({ newTaskOpen: true })
}

export function closeNewTask(): void {
  useTasks.setState({ newTaskOpen: false })
}

/** Shows a task's folder in the window: attached as a project unless a project already holds it. */
export async function showFolder(path: string): Promise<boolean> {
  const { roots, addRoot } = useWorkspace.getState()
  if (roots.some((r) => isWithin(r, path))) return true
  return (await addRoot(path)) !== null
}

/** The open chat working in a task's folder, most recently opened first. */
function chatOf(t: TaskView): string | null {
  const { sessions, order } = useAgentChat.getState()
  for (let i = order.length - 1; i >= 0; i--) if (sessions[order[i]]?.cwd === t.worktree) return order[i]
  return null
}

export interface NewTaskInput {
  description: string
  /** One task per entry; an agent named twice gets two attempts (lib planAgents). */
  agents: string[]
  base: string
}

/**
 * Creates the tasks (a branch and a worktree each) and starts the ones Go gave a
 * slot: opens the folder, starts a chat with the agent there and sends the
 * description, which the user wrote and started. The others are queued; Go
 * announces them later. Returns an error message, or null when the tasks exist.
 */
export async function startTask(input: NewTaskInput): Promise<string | null> {
  let tasks: TaskView[]
  try {
    const req = main.NewTaskGroup.createFrom({ dir: activeRoot(), description: input.description, base: input.base, agents: input.agents })
    tasks = ((await TaskAPI.CreateGroup(req)) ?? []) as TaskView[]
  } catch (err) {
    return errorMessage(err)
  }
  closeNewTask()
  void loadTasks()
  const queued = tasks.filter((t) => t.state === 'queued').length
  if (queued > 0) showToast(`${queued} ${queued === 1 ? 'task is' : 'tasks are'} queued: ${queued === 1 ? 'it starts' : 'they start'} when a working task settles`)
  for (const t of tasks) if (t.state !== 'queued') await beginTask(t, input.description.trim())
  return null
}

/**
 * Starts a task's chat and sends its description, then tells Go the start is
 * over (whether the turn began or not), so the slot held for it is counted right.
 */
async function beginTask(t: TaskView, text: string): Promise<void> {
  try {
    if (!(await showFolder(t.worktree))) return
    const chat = useAgentChat.getState()
    const id = await chat.start(t.agent, t.worktree)
    if (!id) return // start() told the user why; the task stays, Open retries
    chat.setDraft(id, text)
    await useAgentChat.getState().send(id)
  } finally {
    await TaskAPI.StartDone(t.id).catch(() => undefined)
  }
}

/** A task Go took off the queue: started with the description as it was saved (secrets redacted). */
async function beginQueuedTask(id: string): Promise<void> {
  let task: TaskView | undefined
  try {
    const tasks = ((await TaskAPI.ListAll(useWorkspace.getState().roots)) ?? []) as TaskView[]
    task = tasks.find((t) => t.id === id)
  } catch {
    // Listed below as not found.
  }
  if (!task || task.stale) {
    await TaskAPI.StartDone(id).catch(() => undefined)
    return
  }
  await beginTask(task, task.prompt)
}

/** Starts a queued task now, although the limit of working tasks is reached. */
export async function startQueuedNow(t: TaskView): Promise<void> {
  const started = await withBusy(t, () => TaskAPI.StartNow(t.id))
  if (!started) return
  void loadTasks()
  await beginTask(started as TaskView, t.prompt)
}

let subscribed = false
/**
 * Follows the queue for the whole session (not only while the panel is shown):
 * starts the chats of tasks Go takes off the queue, and lets Go look at the queue
 * when the window's folders are known or change (after a restart queued tasks
 * wait for their repository to be open).
 */
export function subscribeTaskQueue(): void {
  if (subscribed) return
  subscribed = true
  // One at a time: starting a chat opens a folder and a tab.
  let chain: Promise<void> = Promise.resolve()
  EventsOn('tasks:start', (id: string) => {
    chain = chain.then(() => beginQueuedTask(id)).catch(() => undefined)
  })
  const pump = () => void TaskAPI.StartQueued().catch(() => undefined)
  useWorkspace.subscribe((s, prev) => {
    if (s.roots !== prev.roots && s.roots.length > 0) pump()
  })
  if (useWorkspace.getState().roots.length > 0) pump()
}

/** Shows the task's folder and its chat (a new one when none is open). */
export async function openTask(t: TaskView): Promise<void> {
  if (!(await showFolder(t.worktree))) return
  const id = chatOf(t)
  if (id) useEditor.getState().setActive(chatTabId(id))
  else await useAgentChat.getState().start(t.agent, t.worktree)
}

export function toggleChanges(t: TaskView): void {
  const open = !useTasks.getState().expanded[t.id]
  useTasks.setState((s) => ({ expanded: { ...s.expanded, [t.id]: open } }))
  if (open) void loadChanges([t.id])
}

/** Opens one changed file in the diff view, against where the task left its base. */
export function openTaskFileDiff(t: TaskView, f: git.FileStat): void {
  const changes = useTasks.getState().changes[t.id]
  if (!changes) return
  useEditor.getState().openRevisionDiff({
    repoRoot: t.worktree,
    path: `${t.worktree}/${f.path}`,
    basePath: `${t.worktree}/${f.origPath || f.path}`,
    baseRev: changes.mergeBase,
    baseLabel: t.base,
  })
}

async function withBusy<T>(t: TaskView, fn: () => Promise<T>): Promise<T | undefined> {
  if (useTasks.getState().busy[t.id]) return undefined
  useTasks.setState((s) => ({ busy: { ...s.busy, [t.id]: true } }))
  try {
    return await fn()
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return undefined
  } finally {
    useTasks.setState((s) => ({ busy: { ...s.busy, [t.id]: false } }))
  }
}

/** Merges the task into its base checkout; on conflicts offers the agent hand-off. True when it was merged. */
export async function mergeTask(t: TaskView): Promise<boolean> {
  if (!(await confirmAction(`Merge ${t.branch} (${t.ahead} commit(s)) into ${t.base}?`, 'Merge'))) return false
  const res = await withBusy(t, () => TaskAPI.Merge(t.id))
  if (!res) return false
  if (res.merged) {
    showToast(`Merged ${t.branch} into ${t.base}`)
    return true
  }
  const list = res.conflicts.slice(0, 5).join(', ') + (res.conflicts.length > 5 ? ', …' : '')
  const ok = await confirmAction(
    `Merging into ${t.base} would conflict in ${list}. Nothing was changed. Merge ${t.base} into the task's branch instead and let an agent resolve the conflicts there? Then merge again.`,
    'Resolve with agent',
  )
  if (ok) await resolveInTask(t)
  return false
}

async function resolveInTask(t: TaskView): Promise<void> {
  const files = await withBusy(t, () => TaskAPI.UpdateFromBase(t.id))
  if (!files) return
  if (files.length === 0) {
    showToast(`${t.base} merged into the task cleanly: merge the task again`)
    return
  }
  await showFolder(t.worktree)
  await resolveConflictsWithAgent(t.worktree, 'merging', files)
}

/** Pushes the branch and opens a pull request (Go asks for confirmation first). True when it was created. */
export async function createTaskPullRequest(t: TaskView): Promise<boolean> {
  try {
    useTasks.setState((s) => ({ busy: { ...s.busy, [t.id]: true } }))
    const url = await TaskAPI.CreatePullRequest(t.id)
    showToast(`Pull request created: ${url}`)
    return true
  } catch (err) {
    const msg = errorMessage(err)
    if (isSignInError(msg)) requestGitHubSignIn()
    else if (!msg.includes('cancelled')) showToast(msg, 'error')
    return false
  } finally {
    useTasks.setState((s) => ({ busy: { ...s.busy, [t.id]: false } }))
  }
}

/** Hands a failed task to its agent: a draft in the task's chat, for the user to send. */
export async function askAgentAboutTask(t: TaskView): Promise<void> {
  if (!(await showFolder(t.worktree))) return
  await askAgent({ prompt: failurePrompt(t), root: t.worktree })
}

/**
 * Keeps one task of a group: merges it (or opens its pull request), exactly as
 * Merge and Create PR do, then offers to remove the others. Each removal goes
 * through removeTask's own questions about uncommitted work and unmerged branches.
 */
export async function keepTask(t: TaskView, how: 'merge' | 'pr'): Promise<void> {
  const kept = how === 'merge' ? await mergeTask(t) : await createTaskPullRequest(t)
  if (!kept) return
  await loadTasks()
  const others = groupMates(t, useTasks.getState().tasks)
  if (others.length === 0) return
  if (!(await confirmAction(removeOthersQuestion(t, others), others.length === 1 ? 'Remove it' : 'Remove them'))) return
  for (const o of others) await removeTask(o, true)
  if (useTasks.getState().compare?.group === t.group) closeCompare()
}

/** Opens the comparison of a group of tasks. */
export async function openCompare(group: string): Promise<void> {
  useTasks.setState({ compare: { group, data: null, path: null, file: null, pair: null, between: null, error: null } })
  await refreshCompare()
}

export function closeCompare(): void {
  useTasks.setState({ compare: null })
}

const patchCompare = (group: string, patch: Partial<CompareState>) =>
  useTasks.setState((s) => (s.compare?.group === group ? { compare: { ...s.compare, ...patch } } : {}))

/** Re-reads the open comparison (the file list, and the selected file's diffs). */
export async function refreshCompare(): Promise<void> {
  const open = useTasks.getState().compare
  if (!open) return
  try {
    const data = await TaskAPI.Compare(open.group)
    const live = data.tasks.filter((c) => !c.task.stale).map((c) => c.task.id)
    const now = useTasks.getState().compare
    const pair = now?.pair && now.pair.every((id) => live.includes(id)) ? now.pair : live.length >= 2 ? ([live[0], live[1]] as [string, string]) : null
    patchCompare(open.group, { data, pair, error: null })
    const path = now?.path && data.files.some((f) => f.path === now.path) ? now.path : (data.files[0]?.path ?? null)
    await selectCompareFile(path)
  } catch (err) {
    patchCompare(open.group, { error: errorMessage(err) })
  }
}

/** Shows one file of the open comparison: its diff in every task, and between the chosen pair. */
export async function selectCompareFile(path: string | null): Promise<void> {
  const open = useTasks.getState().compare
  if (!open) return
  if (path === null) {
    patchCompare(open.group, { path: null, file: null, between: null })
    return
  }
  if (path !== open.path) patchCompare(open.group, { path, file: null, between: null })
  try {
    const [file, between] = await Promise.all([
      TaskAPI.CompareFile(open.group, path),
      open.pair ? TaskAPI.DiffBetween(open.pair[0], open.pair[1], path) : Promise.resolve(null),
    ])
    if (useTasks.getState().compare?.path === path) patchCompare(open.group, { file, between, error: null })
  } catch (err) {
    patchCompare(open.group, { error: errorMessage(err) })
  }
}

/** Chooses the two tasks of the direct diff. */
export async function setComparePair(pair: [string, string]): Promise<void> {
  const open = useTasks.getState().compare
  if (!open || pair[0] === pair[1]) return
  patchCompare(open.group, { pair, between: null })
  await selectCompareFile(open.path)
}

/** Opens a compared file in the diff view of one task, against where it left its base. */
export function openComparedFileDiff(c: main.ComparedTask, path: string, origPath: string): void {
  useEditor.getState().openRevisionDiff({
    repoRoot: c.task.worktree,
    path: `${c.task.worktree}/${path}`,
    basePath: `${c.task.worktree}/${origPath || path}`,
    baseRev: c.mergeBase,
    baseLabel: c.task.base,
  })
}

/**
 * Removes a task after confirming; closes its chats and its folder first.
 * asked: the user already said yes to removing it (the questions about work that
 * would be lost are still asked).
 */
export async function removeTask(t: TaskView, asked = false): Promise<void> {
  const { roots } = useWorkspace.getState()
  if (roots[0] === t.worktree) {
    showToast("The task's folder is this window's main project: open another folder first", 'error')
    return
  }
  let info: main.RemoveInfo
  try {
    info = await TaskAPI.RemoveCheck(t.id)
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return
  }
  const what = t.stale ? 'The task is forgotten (its folder is already gone).' : `Its chats are stopped and the folder ${t.worktree} is deleted.`
  if (!asked && !(await confirmAction(`Remove the task "${t.title}"? ${what}`, 'Remove'))) return
  const q = removeQuestions(info)
  const which = asked ? `${t.branch}: ` : ''
  if (q.discard && !(await confirmAction(which + q.discard, 'Delete them'))) return
  const deleteBranch = q.branch ? await confirmAction(which + q.branch, 'Delete branch') : false
  const chat = useAgentChat.getState()
  for (const s of Object.values(chat.sessions)) if (isWithin(t.worktree, s.cwd)) await chat.stop(s.id)
  if (roots.includes(t.worktree)) await useWorkspace.getState().removeRoot(t.worktree)
  else useEditor.getState().closePaths([t.worktree])
  const res = await withBusy(t, () =>
    TaskAPI.Remove(t.id, main.RemoveOptions.createFrom({ discardChanges: !!q.discard, deleteBranch })),
  )
  if (!res) return
  showToast(res.branchKept ? `Task removed; the branch ${t.branch} was kept` : 'Task removed')
  void loadTasks()
}
