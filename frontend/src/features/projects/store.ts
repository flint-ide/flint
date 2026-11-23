// Projects (Go: internal/features/projects): the panel's and the boards' state and
// actions. Go emits "projects:changed" with a project folder after its own writes
// ("" when the list of projects changed); agent task, git and file events change
// what a project shows too. Nothing polls: views follow events while mounted.
import { createStore } from '../../lib/store'
import * as API from '../../../bindings/flint/internal/features/projects/api'
import * as WorkflowsAPI from '../../../bindings/flint/internal/features/workflows/api'
import * as Workspace from '../../../bindings/flint/workspaceapi'
import { EventsOn } from '../../wails/runtime'
import { errorMessage } from '../../api/backend'
import { askAgent } from '../../lib/askAgent'
import { isFeatureActive } from '../../lib/activeFeatures'
import { RUN_VIEW, runTabData } from '../../lib/workflowRuns'
import { chooseAction, confirmAction, showToast } from '../../store/dialog'
import { alertDone } from '../../store/alerts'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { useAgentDefs, loadAgentDefs } from '../../store/agentDefs'
import { activeRoot, rootFor } from '../../store/activeRoot'
import { showFolder, loadTasks, openTask, useTasks } from '../../store/agentTasks'
import { trustFolder } from '../../store/trust'
import {
  applyCloneEvent,
  autoStartNotice,
  autoStartToast,
  cloneNotice,
  coerceItemEvent,
  reviewText,
  BOARD_VIEW,
  boardTitle,
  cloneFailure,
  clonedText,
  cloneFromInfo,
  coerceAutoStart,
  coerceCloneEvent,
  CONFIG_FILE,
  delegateIssues,
  delegateStep,
  locateQuestion,
  noCheckoutQuestion,
  notReadyQuestion,
  OLD_CONFIG_FILE,
  PLAN_TOOL_OFF,
  pickSelected,
  problemPrompt,
  startedText,
  togetherNote,
  touchesProject,
  trustProjectQuestion,
  trustQuestion,
  waitingToast,
  workflowOptions,
  type WorkflowOption,
  type ProjectData,
  type ProjectInfo,
  type CloneState,
  type DelegateIssue,
  type ItemInfo,
  type ProblemInfo,
  type RepoView,
  type Status,
  type TaskInfo,
} from './projects'

/** Where a dialog is drawn: in the panel (the commands switch to it) or in a board tab. */
export type DialogHost = 'panel' | 'board'

export type ProjectDialog =
  | { kind: 'newProject'; folder: string }
  | { kind: 'newItem'; folder: string; host: DialogHost }
  | { kind: 'plan'; folder: string; host: DialogHost }

interface Loaded {
  project: ProjectData | null
  /** Why the last read failed ('' when it worked); the previous items stay. */
  error: string
}

interface ProjectsState {
  /** The projects Flint knows; null before the first answer. */
  infos: ProjectInfo[] | null
  /** The project the panel shows ('' when none is known). */
  selected: string
  data: Record<string, Loaded>
  /** Items with an action running, by folder and id. */
  busy: Record<string, boolean>
  /** Groups the user collapsed or opened (statuses, "repositories"); done is collapsed until then. */
  collapsed: Record<string, boolean>
  /** The clone running for a project, by folder (from Project.clone and the clone events). */
  clones: Record<string, CloneState>
  dialog: ProjectDialog | null
}

export const useProjects = createStore<ProjectsState>(() => ({
  infos: null,
  selected: readRemembered(),
  data: {},
  busy: {},
  collapsed: { done: true },
  clones: {},
  dialog: null,
}))

const SELECTED_KEY = 'flint.projects.selected'
/** The key of the Backlog preview (before the feature was renamed): migrated once, then removed; it can go later. */
const OLD_SELECTED_KEY = 'flint.backlog.selected'

/** The project chosen last in this browser profile; a per-viewer convenience, empty without storage. */
function readRemembered(): string {
  try {
    const old = localStorage.getItem(OLD_SELECTED_KEY)
    if (old !== null) {
      if (localStorage.getItem(SELECTED_KEY) === null) localStorage.setItem(SELECTED_KEY, old)
      localStorage.removeItem(OLD_SELECTED_KEY)
    }
    return localStorage.getItem(SELECTED_KEY) ?? ''
  } catch {
    return ''
  }
}

function remember(folder: string): void {
  try {
    localStorage.setItem(SELECTED_KEY, folder)
  } catch {
    // Storage blocked: the choice lasts until the window closes.
  }
}

export const itemKey = (folder: string, id: string): string => `${folder}\n${id}`

// ---- reading

/** Lists the projects Flint knows and keeps the selection on one of them. */
export async function refreshProjects(): Promise<void> {
  try {
    const infos = (await API.Projects()) ?? []
    const selected = pickSelected(infos, useProjects.getState().selected)
    useProjects.setState({ infos, selected })
    if (selected) remember(selected)
  } catch (err) {
    showToast(`Couldn't list the projects: ${errorMessage(err)}`, 'error')
  }
}

const loadSeq: Record<string, number> = {}

/** Reads a project with its items; Go also moves items whose agent tasks moved on. */
export async function loadProject(folder: string): Promise<void> {
  if (!folder) return
  const seq = (loadSeq[folder] = (loadSeq[folder] ?? 0) + 1)
  let next: Loaded
  try {
    next = { project: await API.Load(folder), error: '' }
  } catch (err) {
    next = { project: useProjects.getState().data[folder]?.project ?? null, error: errorMessage(err) }
  }
  if (seq !== loadSeq[folder]) return
  useProjects.setState((s) => ({ data: { ...s.data, [folder]: next }, clones: syncClone(s.clones, folder, next.project) }))
}

/**
 * The clone of a project as the load saw it. A clone this window has just asked for (no destination
 * yet: Go is still asking the user) stays until the call ends.
 */
function syncClone(clones: Record<string, CloneState>, folder: string, project: ProjectData | null): Record<string, CloneState> {
  const running = cloneFromInfo(project?.clone)
  if (running) return { ...clones, [folder]: running }
  if (!clones[folder] || clones[folder].destination === '') return clones
  const { [folder]: _gone, ...rest } = clones
  return rest
}

function setClone(folder: string, state: CloneState | null): void {
  useProjects.setState((s) => {
    const { [folder]: _old, ...rest } = s.clones
    return { clones: state ? { ...rest, [folder]: state } : rest }
  })
}

export function selectProject(folder: string): void {
  useProjects.setState({ selected: folder })
  remember(folder)
  void loadProject(folder)
}

export function toggleGroup(group: string, open: boolean): void {
  useProjects.setState((s) => ({ collapsed: { ...s.collapsed, [group]: !open } }))
}

// ---- following events

const views = new Set<() => string[]>()
let unsubscribe: (() => void) | null = null
let timer: ReturnType<typeof setTimeout> | undefined
let wantList = false
let wantAll = false
const wantFolders = new Set<string>()

const targets = (): string[] => [...new Set([...views].flatMap((v) => v()).filter(Boolean))]

function schedule(what: { list?: boolean; all?: boolean; folder?: string }): void {
  wantList ||= !!what.list
  wantAll ||= !!what.all
  if (what.folder) wantFolders.add(what.folder)
  clearTimeout(timer)
  timer = setTimeout(() => {
    const list = wantList
    const folders = wantAll ? targets() : targets().filter((f) => wantFolders.has(f))
    wantList = wantAll = false
    wantFolders.clear()
    void (async () => {
      if (list) await refreshProjects()
      // The list may have moved the selection: read what the views show now.
      await Promise.all((list ? targets() : folders).map(loadProject))
    })()
  }, 100)
}

function subscribe(): () => void {
  const offs = [
    EventsOn('projects:changed', (folder: unknown) => (typeof folder === 'string' && folder ? schedule({ folder }) : schedule({ list: true, all: true }))),
    EventsOn('tasks:changed', () => schedule({ all: true })),
    EventsOn('git:changed', () => schedule({ all: true })),
    EventsOn('projects:clone-progress', (raw: unknown) => {
      const ev = coerceCloneEvent(raw)
      if (ev) setClone(ev.project, applyCloneEvent(useProjects.getState().clones[ev.project] ?? null, ev))
    }),
    EventsOn('projects:clone-done', (raw: unknown) => {
      const ev = coerceCloneEvent(raw)
      if (!ev) return
      setClone(ev.project, null)
      schedule({ folder: ev.project })
    }),
    EventsOn('fs:changed', (paths: string[]) => {
      for (const f of targets()) if (touchesProject(f, paths ?? [])) schedule({ folder: f })
    }),
  ]
  const offRoots = useWorkspace.subscribe((s, prev) => {
    if (s.roots !== prev.roots) schedule({ list: true, all: true })
  })
  return () => {
    clearTimeout(timer)
    wantList = wantAll = false
    wantFolders.clear()
    offs.forEach((off) => off())
    offRoots()
  }
}

/**
 * Tells the user about what projects do by themselves: how an automatic start went ("projects:auto-start":
 * Flint delegated an item that waited for others, or tried to and failed; the item keeps waiting then), an
 * item ready for review because all its agents finished ("projects:review"), and a clone that ended
 * ("projects:clone-done"). A toast in front (a clone's comes from the Clone call), a desktop notification
 * in the background. Runs for the whole session, not only while a view is mounted, because these happen at
 * any time. Returns the unsubscribe.
 */
export function subscribeAutoStart(): () => void {
  const offs = [
    EventsOn('projects:auto-start', (raw: unknown) => {
      const a = coerceAutoStart(raw)
      if (!a) return
      const t = autoStartToast(a)
      showToast(t.message, t.kind, t.kind === 'error' ? 9000 : 4500)
      const n = autoStartNotice(a)
      alertDone(n.title, n.body, '')
      if (!a.error) void loadTasks()
      if (a.project) schedule({ folder: a.project })
    }),
    EventsOn('projects:review', (raw: unknown) => {
      const e = coerceItemEvent(raw)
      if (!e) return
      showToast(reviewText(e), 'info', 4500)
      alertDone(reviewText(e), 'All its agent tasks finished', '')
    }),
    EventsOn('projects:clone-done', (raw: unknown) => {
      const ev = coerceCloneEvent(raw)
      const n = ev && cloneNotice(ev)
      if (n) alertDone(n.title, n.body, '')
    }),
  ]
  return () => offs.forEach((off) => off())
}

/**
 * Follows the projects a view shows (folders() answers which, now) while it is
 * mounted: reads them at once and again on events. Returns the unsubscribe.
 */
export function followProjects(folders: () => string[]): () => void {
  views.add(folders)
  if (views.size === 1) unsubscribe = subscribe()
  if (useAgentDefs.getState().defs === null) void loadAgentDefs()
  void refreshProjects().then(() => Promise.all(targets().map(loadProject)))
  return () => {
    views.delete(folders)
    if (views.size === 0) {
      unsubscribe?.()
      unsubscribe = null
    }
  }
}

// ---- opening, creating and closing projects

async function pickFolder(): Promise<string> {
  try {
    return await Workspace.PickFolder()
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return ''
  }
}

/** Open Project…: picks a folder that holds flint-project.json and shows it. */
export async function openProject(): Promise<void> {
  const dir = await pickFolder()
  if (!dir) return
  try {
    const info = await API.Open(dir)
    await refreshProjects()
    selectProject(info.path)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** New Project…: picks a folder, then asks for a name (the dialog calls createProject). */
export async function newProject(): Promise<void> {
  const dir = await pickFolder()
  if (!dir) return
  useWorkspace.getState().setSidebar('projects')
  useProjects.setState({ dialog: { kind: 'newProject', folder: dir } })
}

/** Makes the folder a project and shows it. Returns the error to show, '' on success. */
export async function createProject(folder: string, name: string): Promise<string> {
  try {
    const info = await API.Create(folder, name.trim())
    await refreshProjects()
    selectProject(info.path)
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/** Close Project: forgets a remembered project (its files and agent tasks stay). */
export async function closeProject(info: ProjectInfo): Promise<void> {
  const ok = await confirmAction(
    `Close the project "${info.name}"?\n\nIts files stay where they are. Flint forgets the folder and the links of its items to agent tasks (the tasks stay).`,
    'Close Project',
  )
  if (!ok) return
  try {
    await API.Close(info.path)
    await refreshProjects()
    const { selected } = useProjects.getState()
    if (selected) void loadProject(selected)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export function closeDialog(): void {
  useProjects.setState({ dialog: null })
}

// ---- items

/** New Item…: asks for a title in the panel (or in the board that asked). */
export function openNewItem(folder: string, host: DialogHost = 'panel'): void {
  if (!folder) {
    showToast('Open or create a project first: Open Project… or New Project…', 'error')
    return
  }
  if (host === 'panel') useWorkspace.getState().setSidebar('projects')
  useProjects.setState({ dialog: { kind: 'newItem', folder, host } })
}

/** Writes the item's file from a template and opens it. Returns the error to show, '' on success. */
export async function createItem(folder: string, title: string, workflow = ''): Promise<string> {
  try {
    const item = workflow ? await API.NewItemWith(folder, { title: title.trim(), workflow }) : await API.NewItem(folder, title.trim())
    useEditor.getState().openFile(item.path)
    void loadProject(folder)
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/**
 * The workflows of a folder for New Item's select (the Workflows binding's List: a binding, not the feature's code).
 * Empty while the Workflows feature is off. Returns the error to show too, '' when it worked.
 */
export async function listWorkflows(folder: string): Promise<{ options: WorkflowOption[]; error: string }> {
  if (!isFeatureActive('workflows')) return { options: [], error: '' }
  try {
    return { options: workflowOptions((await WorkflowsAPI.List(folder)).workflows ?? []), error: '' }
  } catch (err) {
    return { options: [], error: errorMessage(err) }
  }
}

/** Plan with Agent…: asks for the goal and the agent in the panel (or in the board that asked). */
export function openPlan(folder: string, host: DialogHost = 'panel'): void {
  if (!folder) {
    showToast('Open or create a project first: Open Project… or New Project…', 'error')
    return
  }
  if (host === 'panel') useWorkspace.getState().setSidebar('projects')
  useProjects.setState({ dialog: { kind: 'plan', folder, host } })
}

/**
 * Asks Go for the planning prompt and puts it into a chat draft with the chosen agent: the user reads it,
 * adds words and sends it; nothing is sent for them. When the planning tool is off the user is told what to
 * switch on. Returns the error to show in the dialog, '' on success.
 */
export async function planWithAgent(folder: string, goal: string, agent: string): Promise<string> {
  let plan
  try {
    plan = await API.PlanPrompt(folder, goal.trim())
  } catch (err) {
    return errorMessage(err)
  }
  if (!plan.toolOn) showToast(PLAN_TOOL_OFF, 'info', 8000)
  const id = await askAgent({ prompt: plan.prompt, root: rootFor(folder) || activeRoot(), agent })
  if (!id) return `No chat with ${agent} could be opened.`
  closeDialog()
  return ''
}

/** Opens the tab of a workflow run (the Workflows feature draws it; Projects only names it by id, through the tab store). */
export function openWorkflowRun(id: string): void {
  if (!isFeatureActive('workflows')) return showToast('The Workflows feature is off: switch it on to open this run', 'info')
  useEditor.getState().openContributed(RUN_VIEW, id, 'Workflow run', runTabData(id))
}

export const openItemFile = (item: ItemInfo): void => useEditor.getState().openFile(item.path)

/**
 * Rename flint-backlog.json to flint-project.json: the one action of a project in the Backlog preview's format.
 * Go asks first (as for Clone… and Delegate) and refuses when flint-project.json exists; the content is not touched.
 */
export async function renameOldConfig(folder: string): Promise<void> {
  try {
    const info = await API.RenameOldConfig(folder)
    showToast(`Renamed ${OLD_CONFIG_FILE} to ${CONFIG_FILE}: "${info.name}" can be edited and delegated now. Commit the rename if the folder is in a repository.`)
  } catch (err) {
    const msg = errorMessage(err)
    if (!/cancelled/.test(msg)) showToast(msg, 'error')
  }
  await refreshProjects()
  void loadProject(folder)
}

/** Opens the project's PROJECT.md in the editor. */
export const openContext = (project: Pick<ProjectData, 'context'>): void => useEditor.getState().openFile(project.context.path)

/** Add Project Context…: Go writes PROJECT.md from a template (never over a file), then it opens in the editor. */
export async function addContext(folder: string): Promise<void> {
  try {
    const c = await API.AddContext(folder)
    useEditor.getState().openFile(c.path)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
  void loadProject(folder)
}

async function withBusy(folder: string, id: string, fn: () => Promise<void>): Promise<void> {
  const key = itemKey(folder, id)
  if (useProjects.getState().busy[key]) return
  useProjects.setState((s) => ({ busy: { ...s.busy, [key]: true } }))
  try {
    await fn()
  } finally {
    useProjects.setState((s) => ({ busy: { ...s.busy, [key]: false } }))
  }
}

/** Sets an item's status (any to any); Go's refusal is shown as it is. */
export function setStatus(folder: string, id: string, status: Status): Promise<void> {
  return withBusy(folder, id, async () => {
    try {
      await API.SetStatus(folder, id, status)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
    await loadProject(folder)
  })
}

// ---- Repositories and Delegate

/**
 * Locate…: says which repository to find, then asks for the folder. False when the user stopped or it was
 * refused. asked: the user has just been told which repository (Delegate's question), so go on to the folder.
 */
export async function locateRepo(folder: string, repo: string, url: string, note = '', asked = false): Promise<boolean> {
  if (!asked && !(await confirmAction(locateQuestion(repo, url, note), 'Choose Folder…'))) return false
  const dir = await pickFolder()
  if (!dir) return false
  try {
    await API.Locate(folder, repo, dir)
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return false
  }
  void loadProject(folder)
  return true
}

/**
 * Clone…: asks for the folder to clone into, then Go asks again with the URL and the destination and runs git.
 * Progress arrives as events (and Project.clone); Cancel calls cancelClone. Resolves when the clone ended: true
 * when the repository is located now. The clone is not in the workspace: Add to Workspace applies, as for a
 * located folder. It is in Restricted Mode (Trust Folder… applies) unless the chosen folder is inside a trusted
 * one: Go then says in its question that the clone will be trusted (and refuses it for an untrusted project).
 */
export async function cloneRepo(folder: string, repo: string): Promise<boolean> {
  if (useProjects.getState().clones[folder]) {
    showToast('A clone of this project is running: wait for it or cancel it', 'error')
    return false
  }
  const parent = await pickFolder()
  if (!parent) return false
  setClone(folder, { repo, destination: '', progress: '' })
  try {
    const r = await API.Clone(folder, repo, parent)
    showToast(clonedText(repo, r))
    return true
  } catch (err) {
    const f = cloneFailure(repo, errorMessage(err))
    showToast(f.message, f.kind)
    return false
  } finally {
    setClone(folder, null)
    void loadProject(folder)
  }
}

/** Stops the clone running for a project; what it made is removed. */
export async function cancelClone(folder: string): Promise<void> {
  try {
    await API.CancelClone(folder)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Cancel start: forgets an item's waiting start. */
export function cancelStart(folder: string, id: string): Promise<void> {
  return withBusy(folder, id, async () => {
    try {
      await API.CancelStart(folder, id)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
    await loadProject(folder)
  })
}

/** Adds a located checkout outside the workspace to it. */
export async function attachRepo(folder: string, repo: string, dir: string): Promise<boolean> {
  if (!(await showFolder(dir))) return false
  showToast(`Added ${dir} to the workspace`)
  void loadProject(folder)
  return true
}

/** Trusts a repository's checkout after saying that it is for the whole folder, from now on. */
export async function trustRepo(folder: string, repo: string, dir: string, note = ''): Promise<boolean> {
  if (!(await confirmAction(trustQuestion(repo, dir, note), 'Trust Folder'))) return false
  await trustFolder(dir)
  void loadProject(folder)
  return true
}

/** Trusts the project's own folder (get_project shows it then, and Delegate stops asking). */
export async function trustProject(info: Pick<ProjectInfo, 'path' | 'name'>): Promise<void> {
  if (!(await confirmAction(trustProjectQuestion(info.name, info.path), 'Trust Folder'))) return
  await trustFolder(info.path)
  await refreshProjects()
  void loadProject(info.path)
}

/** The actions of the Repositories group: Locate…, Clone…, Add to Workspace, Trust. */
export const repoActions = {
  locate: (folder: string, r: RepoView) => void locateRepo(folder, r.name, r.url),
  clone: (folder: string, r: RepoView) => void cloneRepo(folder, r.name),
  attach: (folder: string, r: RepoView) => void attachRepo(folder, r.name, r.folder),
  trust: (folder: string, r: RepoView) => void trustRepo(folder, r.name, r.folder),
}

/** Fixes one thing in the way of Delegate with the user's help; false when the user stopped. */
async function resolve(folder: string, issue: DelegateIssue, item: ItemInfo): Promise<boolean> {
  const note = togetherNote(item)
  switch (issue.fix) {
    case 'locate': {
      // No checkout at all: clone is an alternative to locating one. A located folder that went bad is located again.
      if (issue.folder) return locateRepo(folder, issue.repo, issue.url, note)
      const choice = await chooseAction(noCheckoutQuestion(issue.repo, issue.url, note), 'Locate…', 'Clone…')
      if (choice === 'cancel') return false
      return choice === 'alternate' ? cloneRepo(folder, issue.repo) : locateRepo(folder, issue.repo, issue.url, note, true)
    }
    case 'attach':
      return attachRepo(folder, issue.repo, issue.folder)
    case 'trust':
      return trustRepo(folder, issue.repo, issue.folder, note)
    default:
      return false
  }
}

/**
 * Delegate: starts the item's agent tasks. Before asking Go it walks the user
 * through what is missing (Locate…, attaching the folder, trusting it), one step
 * at a time (delegateStep); a step that made no progress stops it with the reason.
 * Go starts the chats itself; the window adopts them from "chat:opened", so none
 * is started here. Go asks first, natively, for an item of an untrusted project.
 */
export function delegateItem(folder: string, id: string): Promise<void> {
  return withBusy(folder, id, async () => {
    let last = ''
    for (let step = 0; step < 12; step++) {
      await loadProject(folder)
      const project = useProjects.getState().data[folder]?.project
      const item = project?.items.find((i) => i.id === id)
      if (!project || !item) return showToast('The item is not in the project any more', 'error')
      const next = delegateStep(delegateIssues(item, project.repositories, activeRoot()), last)
      if (next.kind === 'delegate') break
      if (next.kind === 'stuck') return showToast(next.message, 'error')
      if (!(await resolve(folder, next.issue, item))) return // the user stopped
      last = next.sig
    }
    try {
      const res = await API.Delegate(folder, id, activeRoot())
      if (res.notReady) {
        // Nothing was started: the item waits for others. Offer to start it when they are done.
        const items = useProjects.getState().data[folder]?.project?.items ?? []
        const waitingFor = res.waitingFor?.length ? res.waitingFor : res.item.waitingFor
        if (await confirmAction(notReadyQuestion(res.item, waitingFor, items), 'Start When Ready')) {
          await API.StartWhenReady(folder, id, activeRoot())
          showToast(waitingToast(res.item.title, waitingFor, items))
        }
        return await loadProject(folder)
      }
      const n = res.tasks?.length ?? 0
      if (res.warning) showToast(`${startedText(res.item, n)}, but ${res.warning}`, 'error')
      else showToast(startedText(res.item, n))
      void loadTasks()
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
    await loadProject(folder)
  })
}

/** Open on an agent task of an item: its folder and chat, as in the Agent Tasks panel. */
export async function openItemTask(t: TaskInfo): Promise<void> {
  const find = () => useTasks.getState().tasks.find((x) => x.id === t.id)
  let task = find()
  if (!task) {
    await loadTasks()
    task = find()
  }
  if (!task) return showToast('The agent task is not listed: its repository may not be open in this window', 'error')
  await openTask(task)
}

// ---- boards and problems

/** Opens the board of a project in a tab. */
export function openBoard(info: Pick<ProjectInfo, 'path' | 'name'>): void {
  useEditor.getState().openContributed(BOARD_VIEW, info.path, boardTitle(info.name), { folder: info.path })
}

/** Show Project Board: the selected project's, listing the projects first when none is known yet. */
export async function showBoard(): Promise<void> {
  if (useProjects.getState().infos === null) await refreshProjects()
  const { infos, selected } = useProjects.getState()
  const info = infos?.find((i) => i.path === selected)
  if (!info) return showToast('No project yet: Open Project… or New Project…', 'error')
  openBoard(info)
}

/** Ask the agent: hands a problem file to the agent chat. */
export function askAboutProblem(project: ProjectData, p: ProblemInfo): void {
  const path = `${project.path}/${p.file}`
  void askAgent({ prompt: problemPrompt(project.name, project.path, p), attachments: [{ path }], root: rootFor(path) })
}
