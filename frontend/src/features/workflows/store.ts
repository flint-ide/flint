// Workflows (Go: internal/features/workflows): the panel's and the run tab's state and actions.
// Go emits "workflows:changed" (a run id, or "" for the list) after every change of a run; the
// views reload from it. Nothing polls.
import { createStore } from '../../lib/store'
import * as API from '../../../bindings/flint/internal/features/workflows/api'
import { EventsOn } from '../../wails/runtime'
import { askAgent } from '../../lib/askAgent'
import { useSettings } from '../../store/settings'
import { errorMessage } from '../../api/backend'
import { confirmAction, showToast } from '../../store/dialog'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { useAgentChat, chatTabId } from '../../store/agentChat'
import { runTabId } from '../../store/run'
import { activeContextPath, activeRoot, rootFor } from '../../store/activeRoot'
import { trustFolder } from '../../store/trust'
import { loadTasks, openTask as openAgentTask, showFolder, toggleChanges, useTasks } from '../../store/agentTasks'
import { useAgentDefs, loadAgentDefs } from '../../store/agentDefs'
import type { ScheduleView } from '../../../bindings/flint/internal/features/workflows/models'
import { chatButton, orderedActions, runTabTitle, RUN_VIEW, WORKFLOW_VIEW, isWorkflowFile, workflowForPath, workflowViewKey, workflowViewTitle, type RunInfo, type StepInfo, type WorkflowInfo } from './workflows'

export interface Listing {
  workflows: WorkflowInfo[]
  problems: string[]
  restricted: boolean
  hidden: number
  /** How script steps run: 'on', 'off' or 'unavailable'. */
  sandbox: string
  /** With the sandbox off: the user already allowed script steps without it in this folder. */
  unsandboxedOK: boolean
}

export interface Listed {
  listing: Listing | null
  /** Why the last read failed ('' when it worked); the previous listing stays. */
  error: string
}

/** Where a dialog is drawn: the panel hosts Run Workflow… and New Workflow…, the run tab hosts Send back…. */
export type WorkflowDialog =
  | { kind: 'run'; dir: string; workflow: string }
  | { kind: 'new'; dir: string }
  | { kind: 'coordinate'; dir: string }
  | { kind: 'sendBack'; run: string }

interface WorkflowsState {
  /** The runs, newest first; null before the first answer. */
  runs: RunInfo[] | null
  /** Why the runs could not be read ('' when they could). */
  error: string
  /** The workflows of a folder, by folder. */
  lists: Record<string, Listed>
  /** Runs with an action under way, by id. */
  busy: Record<string, boolean>
  dialog: WorkflowDialog | null
  /** The schedules with their next start; null before the first answer. */
  schedules: ScheduleView[] | null
}

export const useWorkflows = createStore<WorkflowsState>(() => ({ runs: null, error: '', lists: {}, busy: {}, dialog: null, schedules: null }))

// ---- reading

let runsSeq = 0

/** Reads the runs; the previous ones stay when the read fails. */
export async function loadRuns(): Promise<void> {
  const seq = ++runsSeq
  try {
    const runs = (await API.Runs()) ?? []
    if (seq === runsSeq) useWorkflows.setState({ runs, error: '' })
  } catch (err) {
    if (seq === runsSeq) useWorkflows.setState({ error: errorMessage(err) })
  }
}

const listSeq: Record<string, number> = {}

/** Reads the workflows offered for a folder. */
export async function loadList(dir: string): Promise<void> {
  if (!dir) return
  const seq = (listSeq[dir] = (listSeq[dir] ?? 0) + 1)
  let next: Listed
  try {
    next = { listing: await API.List(dir), error: '' }
  } catch (err) {
    next = { listing: useWorkflows.getState().lists[dir]?.listing ?? null, error: errorMessage(err) }
  }
  if (seq !== listSeq[dir]) return
  useWorkflows.setState((s) => ({ lists: { ...s.lists, [dir]: next } }))
}

/** Reads the schedules with their next start; the previous list stays when the read fails. */
export async function loadSchedules(): Promise<void> {
  try {
    useWorkflows.setState({ schedules: (await API.Schedules()) ?? [] })
  } catch {
    // Keep the previous list: the panel only shows it, and the next change reads it again.
  }
}

let timer: ReturnType<typeof setTimeout> | undefined

function scheduleRuns(): void {
  clearTimeout(timer)
  timer = setTimeout(() => {
    void loadRuns()
    void loadSchedules()
  }, 100)
}

const WORKFLOW_FILE = /\/workflows\/[^/]+\.md$/

/**
 * Follows the runs for the whole session (the badge and the status bar item need them while no view is
 * open): reads them now and again on "workflows:changed". The folders that were listed are read again
 * when a workflow file changes. Returns the unsubscribe.
 */
export function followWorkflows(): () => void {
  const offs = [
    EventsOn('workflows:changed', scheduleRuns),
    EventsOn('fs:changed', (paths: unknown) => {
      if (!Array.isArray(paths) || !paths.some((p) => typeof p === 'string' && WORKFLOW_FILE.test(p))) return
      for (const dir of Object.keys(useWorkflows.getState().lists)) void loadList(dir)
    }),
  ]
  const offRoots = useWorkspace.subscribe((s, prev) => {
    if (s.roots !== prev.roots) for (const dir of Object.keys(useWorkflows.getState().lists)) void loadList(dir)
  })
  // A saved schedule changes its next start.
  const offSettings = useSettings.subscribe((s, prev) => {
    if (s.settings.workflows?.schedules !== prev.settings.workflows?.schedules) void loadSchedules()
  })
  void loadRuns()
  void loadSchedules()
  return () => {
    clearTimeout(timer)
    offs.forEach((off) => off())
    offRoots()
    offSettings()
  }
}

// ---- dialogs

export function closeDialog(): void {
  useWorkflows.setState({ dialog: null })
}

/** Run Workflow…: asks for the workflow, the task and the agent, in the panel (the commands switch to it). */
export function openRunDialog(workflow = '', folder = ''): void {
  const dir = folder || activeRoot()
  if (!dir) {
    showToast('Open a folder first: a workflow runs in a branch and worktree of it', 'error')
    return
  }
  useWorkspace.getState().setSidebar('workflows')
  if (useAgentDefs.getState().defs === null) void loadAgentDefs()
  void loadList(dir)
  useWorkflows.setState({ dialog: { kind: 'run', dir, workflow } })
}

/** New Workflow…: asks for a name; the file is made in the active folder. */
export function openNewDialog(): void {
  const dir = activeRoot()
  if (!dir) {
    showToast('Open a folder first: a workflow file is written into its .flint/workflows', 'error')
    return
  }
  useWorkspace.getState().setSidebar('workflows')
  useWorkflows.setState({ dialog: { kind: 'new', dir } })
}

/** Coordinate with Agent…: asks for a goal and the agent, in the panel (the commands switch to it). */
export function openCoordinateDialog(): void {
  const dir = activeRoot()
  if (!dir) {
    showToast('Open a folder first: the agent coordinates the workflows of the active folder', 'error')
    return
  }
  useWorkspace.getState().setSidebar('workflows')
  useWorkflows.setState({ dialog: { kind: 'coordinate', dir } })
}

/**
 * Asks Go for the coordinator prompt and puts it into a chat draft with the chosen agent: the user reads it and
 * sends it; nothing is sent for them. Returns the error to show in the dialog ('' on success) and whether the
 * workflow tools are off (then the dialog stays open to say what to switch on).
 */
export async function coordinateWithAgent(dir: string, goal: string, agent: string): Promise<{ error: string; toolOff: boolean }> {
  let prompt
  try {
    prompt = await API.CoordinatePrompt(dir, goal.trim())
  } catch (err) {
    return { error: errorMessage(err), toolOff: false }
  }
  const id = await askAgent({ prompt: prompt.prompt, root: rootFor(dir) || dir, agent })
  if (!id) return { error: `No chat with ${agent} could be opened.`, toolOff: false }
  if (prompt.toolOn) closeDialog()
  return { error: '', toolOff: !prompt.toolOn }
}

/** Opens Settings ▸ Agent Context, where the workflow tools are switched on. */
export function openAgentContextSettings(): void {
  closeDialog()
  useEditor.getState().openSettings('agentContext')
}

/** Accepts the current workflow of a schedule (Go starts running it again), then reads the schedules. Returns the error to show, '' on success. */
export async function acceptSchedule(name: string): Promise<string> {
  try {
    await API.AcceptSchedule(name)
    await loadSchedules()
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/** Takes back the user's OK to run script steps without a sandbox in dir; '' or why it failed. */
export async function forgetUnsandboxed(dir: string): Promise<string> {
  try {
    await API.ForgetUnsandboxed(dir)
    await loadList(dir)
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/** Opens Settings ▸ Workflows (schedules, sandbox). */
export const openWorkflowSettings = (): void => useEditor.getState().openSettings('workflows')

/** Starts a run. Returns the error to show in the dialog, '' on success (the run tab opens). */
export async function startRun(req: { dir: string; workflow: string; description: string; agent: string }): Promise<string> {
  try {
    const run = await API.Start({ ...req, description: req.description.trim() } as Parameters<typeof API.Start>[0])
    await loadRuns()
    closeDialog()
    void loadTasks()
    openRun(run.id, runTabTitle(run))
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/** Writes a starter workflow into the folder's .flint/workflows and opens it. Returns the error to show, '' on success. */
export async function createWorkflow(dir: string, name: string): Promise<string> {
  try {
    const path = await API.NewWorkflow(dir, name)
    useEditor.getState().openFile(path)
    void loadList(dir)
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/** Trusts the folder (asked first), so its repository's workflows are offered. */
export async function trustWorkflowFolder(dir: string): Promise<void> {
  const ok = await confirmAction(
    `Trust the folder ${dir}?\n\nIts workflows can then be run, and the steps run in a worktree of it. Trust is for the whole folder from now on: its hooks, settings and language servers run too. Trust it only if you trust its authors.`,
    'Trust Folder',
  )
  if (!ok) return
  await trustFolder(dir)
  void loadList(dir)
}

// ---- actions on a run

async function withBusy(id: string, fn: () => Promise<void>): Promise<void> {
  if (useWorkflows.getState().busy[id]) return
  useWorkflows.setState((s) => ({ busy: { ...s.busy, [id]: true } }))
  try {
    await fn()
  } finally {
    useWorkflows.setState((s) => ({ busy: { ...s.busy, [id]: false } }))
    await loadRuns()
  }
}

/** Sends the steps from `step` back, with a note. Returns the error to show in the dialog, '' on success. */
export async function sendBack(id: string, step: string, note: string): Promise<string> {
  let error = ''
  await withBusy(id, async () => {
    try {
      await API.SendBack(id, step, note.trim())
    } catch (err) {
      error = errorMessage(err)
    }
  })
  if (!error) closeDialog()
  return error
}

const CALLS: Record<string, (id: string) => Promise<void>> = {
  approve: (id) => API.Approve(id),
  recheck: (id) => API.Recheck(id),
  retry: (id) => API.Retry(id),
  skip: (id) => API.Skip(id),
  pause: (id) => API.Pause(id),
  resume: (id) => API.Resume(id),
  stop: (id) => API.Stop(id),
  remove: (id) => API.Remove(id),
}

/**
 * Does what a button of a run says: asks first when the action ends or overrides something, then calls Go,
 * whose refusal is shown as it is. Send back… opens its dialog instead. Removing a run closes its tab.
 */
export async function perform(run: Pick<RunInfo, 'id'>, name: string): Promise<void> {
  if (name === 'sendBack') {
    useWorkflows.setState({ dialog: { kind: 'sendBack', run: run.id } })
    return
  }
  const info = orderedActions([name])[0]
  const call = CALLS[name]
  if (!info || !call) return
  if (info.confirm && !(await confirmAction(info.confirm, info.confirmLabel))) return
  await withBusy(run.id, async () => {
    try {
      await call(run.id)
      if (name === 'remove') useEditor.getState().closeTab(runTabKey(run.id))
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  })
}

// ---- opening things

const runTabKey = (id: string) => `contrib:${RUN_VIEW}:${id}`

/** Opens the run's tab (the one place a run is steered from). */
export function openRun(id: string, title?: string): void {
  const run = useWorkflows.getState().runs?.find((r) => r.id === id)
  useEditor.getState().openContributed(RUN_VIEW, id, title ?? (run ? runTabTitle(run) : 'Workflow run'), { runId: id })
}

/** Opens the diagram tab of one workflow of a folder (one tab each). */
export function openWorkflowView(dir: string, name: string): void {
  useEditor.getState().openContributed(WORKFLOW_VIEW, workflowViewKey(dir, name), workflowViewTitle(name), { dir, name })
}

/**
 * Show Workflow Diagram…: the workflow of the given file, else of the active file when that is a workflow file,
 * else the first workflow of the active folder (there is no quick pick to choose from).
 */
export async function showWorkflowDiagram(folder = '', file = ''): Promise<void> {
  const dir = folder || activeRoot()
  if (!dir) return showToast('Open a folder first: its workflows are shown there', 'error')
  const active = activeContextPath()
  const path = file || (active && isWorkflowFile(active) ? active : '')
  await loadList(dir)
  const list = useWorkflows.getState().lists[dir]?.listing?.workflows ?? []
  const wanted = path ? workflowForPath(list, path) : undefined
  const workflow = wanted ?? list[0]
  if (!workflow) return showToast('This folder offers no workflow to show', 'info')
  openWorkflowView(dir, workflow.name)
}

async function taskOf(run: Pick<RunInfo, 'task'>) {
  const find = () => useTasks.getState().tasks.find((t) => t.id === run.task)
  let task = find()
  if (!task) {
    await loadTasks()
    task = find()
  }
  return task
}

const TASK_GONE = "The run's agent task is not listed: its repository may not be open in this window"

/** Shows the chat of a step (a chat is an ordinary chat tab in the task's worktree). */
export async function openStepChat(run: Pick<RunInfo, 'task'>, step: Pick<StepInfo, 'chat'>): Promise<void> {
  if (!step.chat) return showToast('This step has no chat yet', 'info')
  const task = await taskOf(run)
  if (task && !(await showFolder(task.worktree))) return
  if (!useAgentChat.getState().sessions[step.chat]) return showToast('This chat is closed. Open task shows the task and its latest chat.', 'info')
  useEditor.getState().setActive(chatTabId(step.chat))
}

/** What Open chat does for a step: its own chat, or (the first step still running in the task's chat) the agent task. */
export async function openRunChat(run: Pick<RunInfo, 'task' | 'steps' | 'queued'>, index: number): Promise<void> {
  const button = chatButton(run, index)
  if (button.target === 'task') return openTask(run)
  if (button.target === 'step') return openStepChat(run, run.steps[index])
  showToast(button.hint, 'info')
}

/** Shows the run tab of a script step. */
export function openStepRun(step: Pick<StepInfo, 'runTab'>): void {
  if (!step.runTab) return showToast('This step has not run yet', 'info')
  const id = runTabId(step.runTab)
  if (!useEditor.getState().tabs.some((t) => t.id === id)) return showToast('This run tab is closed. Run the script step again to see it.', 'info')
  useEditor.getState().setActive(id)
}

/** Open task: the run's agent task, its folder and chat, as in the Agent Tasks panel. */
export async function openTask(run: Pick<RunInfo, 'task'>): Promise<void> {
  const task = await taskOf(run)
  if (!task) return showToast(TASK_GONE, 'error')
  await openAgentTask(task)
}

/** Show changes: the Agent Tasks panel with the task's changed files open (Merge and Create PR are there). */
export async function showTaskChanges(run: Pick<RunInfo, 'task'>): Promise<void> {
  const task = await taskOf(run)
  if (!task) return showToast(TASK_GONE, 'error')
  useWorkspace.getState().setSidebar('tasks')
  if (!useTasks.getState().expanded[task.id]) toggleChanges(task)
}

export const openWorkflowFile = (path: string): void => useEditor.getState().openFile(path)

/** The runs that wait for the user: the activity bar's badge and the status bar item (a hook). */
export const useNeedsYou = (): number => useWorkflows((s) => (s.runs ? s.runs.filter((r) => r.needsUser).length : 0))
