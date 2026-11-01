// Agent tasks (TaskAPI in Go): the Tasks panel's list, the New Agent Task dialog
// and the row actions. State changes come as "tasks:changed" events (derived in
// Go from the chats' agent events); changed-file counts follow git and file events.
import { createStore } from '../lib/store'
import * as TaskAPI from '../../bindings/flint/taskapi'
import { EventsOn } from '../wails/runtime'
import { git, main } from '../wails/models'
import { errorMessage } from '../api/backend'
import { failurePrompt, mergeTaskLists, pruneChanges, removeQuestions, tasksTouchedBy, type TaskView } from '../lib/agentTasks'
import { isWithin } from '../lib/fileTree'
import { askAgent } from '../lib/askAgent'
import { confirmAction, showToast } from './dialog'
import { useWorkspace } from './workspace'
import { useEditor } from './editor'
import { chatTabId, useAgentChat } from './agentChat'
import { resolveConflictsWithAgent } from './agent'
import { activeRoot } from './activeRoot'

export type TaskChanges = git.DiffStat

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
}

export const useTasks = createStore<TasksState>(() => ({
  tasks: [],
  changes: {},
  expanded: {},
  busy: {},
  error: null,
  newTaskOpen: false,
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
  agent: string
  base: string
}

/**
 * Creates the task (branch + worktree), opens its folder, starts a chat with the
 * chosen agent there and sends the description, which the user wrote and started.
 * Returns an error message, or null when the task started.
 */
export async function startTask(input: NewTaskInput): Promise<string | null> {
  let task: TaskView
  try {
    task = (await TaskAPI.Create(main.NewTask.createFrom({ dir: activeRoot(), ...input }))) as TaskView
  } catch (err) {
    return errorMessage(err)
  }
  closeNewTask()
  void loadTasks()
  await showFolder(task.worktree)
  const chat = useAgentChat.getState()
  const id = await chat.start(input.agent, task.worktree)
  if (!id) return null // start() told the user why; the task stays, Open retries
  chat.setDraft(id, input.description.trim())
  await useAgentChat.getState().send(id)
  return null
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

/** Merges the task into its base checkout; on conflicts offers the agent hand-off. */
export async function mergeTask(t: TaskView): Promise<void> {
  if (!(await confirmAction(`Merge ${t.branch} (${t.ahead} commit(s)) into ${t.base}?`, 'Merge'))) return
  const res = await withBusy(t, () => TaskAPI.Merge(t.id))
  if (!res) return
  if (res.merged) {
    showToast(`Merged ${t.branch} into ${t.base}`)
    return
  }
  const list = res.conflicts.slice(0, 5).join(', ') + (res.conflicts.length > 5 ? ', …' : '')
  const ok = await confirmAction(
    `Merging into ${t.base} would conflict in ${list}. Nothing was changed. Merge ${t.base} into the task's branch instead and let an agent resolve the conflicts there? Then merge again.`,
    'Resolve with agent',
  )
  if (ok) await resolveInTask(t)
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

/** Pushes the branch and opens a pull request (Go asks for confirmation first). */
export async function createTaskPullRequest(t: TaskView): Promise<void> {
  try {
    useTasks.setState((s) => ({ busy: { ...s.busy, [t.id]: true } }))
    const url = await TaskAPI.CreatePullRequest(t.id)
    showToast(`Pull request created: ${url}`)
  } catch (err) {
    const msg = errorMessage(err)
    if (!msg.includes('cancelled')) showToast(msg, 'error')
  } finally {
    useTasks.setState((s) => ({ busy: { ...s.busy, [t.id]: false } }))
  }
}

/** Hands a failed task to its agent: a draft in the task's chat, for the user to send. */
export async function askAgentAboutTask(t: TaskView): Promise<void> {
  if (!(await showFolder(t.worktree))) return
  await askAgent({ prompt: failurePrompt(t), root: t.worktree })
}

/** Removes a task after confirming; closes its chats and its folder first. */
export async function removeTask(t: TaskView): Promise<void> {
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
  if (!(await confirmAction(`Remove the task "${t.title}"? ${what}`, 'Remove'))) return
  const q = removeQuestions(info)
  if (q.discard && !(await confirmAction(q.discard, 'Delete them'))) return
  const deleteBranch = q.branch ? await confirmAction(q.branch, 'Delete branch') : false
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
