// Backlog (Go: internal/features/backlog): the panel's and the boards' state and
// actions. Go emits "backlog:changed" with a backlog folder after its own writes
// ("" when the list of backlogs changed); agent task, git and file events change
// what a backlog shows too. Nothing polls: views follow events while mounted.
import { createStore } from '../../lib/store'
import * as API from '../../../bindings/flint/internal/features/backlog/api'
import * as Workspace from '../../../bindings/flint/workspaceapi'
import { EventsOn } from '../../wails/runtime'
import { errorMessage } from '../../api/backend'
import { askAgent } from '../../lib/askAgent'
import { confirmAction, showToast } from '../../store/dialog'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { useAgentDefs, loadAgentDefs } from '../../store/agentDefs'
import { activeRoot, rootFor } from '../../store/activeRoot'
import { showFolder, loadTasks, openTask, useTasks } from '../../store/agentTasks'
import { trustFolder } from '../../store/trust'
import {
  BOARD_VIEW,
  boardTitle,
  delegateIssues,
  delegateStep,
  locateQuestion,
  pickSelected,
  problemPrompt,
  touchesBacklog,
  trustBacklogQuestion,
  trustQuestion,
  type BacklogData,
  type BacklogInfo,
  type DelegateIssue,
  type ItemInfo,
  type ProblemInfo,
  type RepoView,
  type Status,
  type TaskInfo,
} from './backlog'

/** Where a dialog is drawn: in the panel (the commands switch to it) or in a board tab. */
export type DialogHost = 'panel' | 'board'

export type BacklogDialog = { kind: 'newBacklog'; folder: string } | { kind: 'newItem'; folder: string; host: DialogHost }

interface Loaded {
  backlog: BacklogData | null
  /** Why the last read failed ('' when it worked); the previous items stay. */
  error: string
}

interface BacklogState {
  /** The backlogs Flint knows; null before the first answer. */
  infos: BacklogInfo[] | null
  /** The backlog the panel shows ('' when none is known). */
  selected: string
  data: Record<string, Loaded>
  /** Items with an action running, by folder and id. */
  busy: Record<string, boolean>
  /** Groups the user collapsed or opened (statuses, "repositories"); done is collapsed until then. */
  collapsed: Record<string, boolean>
  dialog: BacklogDialog | null
}

export const useBacklog = createStore<BacklogState>(() => ({
  infos: null,
  selected: readRemembered(),
  data: {},
  busy: {},
  collapsed: { done: true },
  dialog: null,
}))

const SELECTED_KEY = 'flint.backlog.selected'

/** The backlog chosen last in this browser profile; a per-viewer convenience, empty without storage. */
function readRemembered(): string {
  try {
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

/** Lists the backlogs Flint knows and keeps the selection on one of them. */
export async function refreshBacklogs(): Promise<void> {
  try {
    const infos = (await API.Backlogs()) ?? []
    const selected = pickSelected(infos, useBacklog.getState().selected)
    useBacklog.setState({ infos, selected })
    if (selected) remember(selected)
  } catch (err) {
    showToast(`Couldn't list the backlogs: ${errorMessage(err)}`, 'error')
  }
}

const loadSeq: Record<string, number> = {}

/** Reads a backlog with its items; Go also moves items whose agent tasks moved on. */
export async function loadBacklog(folder: string): Promise<void> {
  if (!folder) return
  const seq = (loadSeq[folder] = (loadSeq[folder] ?? 0) + 1)
  let next: Loaded
  try {
    next = { backlog: await API.Load(folder), error: '' }
  } catch (err) {
    next = { backlog: useBacklog.getState().data[folder]?.backlog ?? null, error: errorMessage(err) }
  }
  if (seq === loadSeq[folder]) useBacklog.setState((s) => ({ data: { ...s.data, [folder]: next } }))
}

export function selectBacklog(folder: string): void {
  useBacklog.setState({ selected: folder })
  remember(folder)
  void loadBacklog(folder)
}

export function toggleGroup(group: string, open: boolean): void {
  useBacklog.setState((s) => ({ collapsed: { ...s.collapsed, [group]: !open } }))
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
      if (list) await refreshBacklogs()
      // The list may have moved the selection: read what the views show now.
      await Promise.all((list ? targets() : folders).map(loadBacklog))
    })()
  }, 100)
}

function subscribe(): () => void {
  const offs = [
    EventsOn('backlog:changed', (folder: unknown) => (typeof folder === 'string' && folder ? schedule({ folder }) : schedule({ list: true, all: true }))),
    EventsOn('tasks:changed', () => schedule({ all: true })),
    EventsOn('git:changed', () => schedule({ all: true })),
    EventsOn('fs:changed', (paths: string[]) => {
      for (const f of targets()) if (touchesBacklog(f, paths ?? [])) schedule({ folder: f })
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
 * Follows the backlogs a view shows (folders() answers which, now) while it is
 * mounted: reads them at once and again on events. Returns the unsubscribe.
 */
export function followBacklogs(folders: () => string[]): () => void {
  views.add(folders)
  if (views.size === 1) unsubscribe = subscribe()
  if (useAgentDefs.getState().defs === null) void loadAgentDefs()
  void refreshBacklogs().then(() => Promise.all(targets().map(loadBacklog)))
  return () => {
    views.delete(folders)
    if (views.size === 0) {
      unsubscribe?.()
      unsubscribe = null
    }
  }
}

// ---- opening, creating and closing backlogs

async function pickFolder(): Promise<string> {
  try {
    return await Workspace.PickFolder()
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return ''
  }
}

/** Open Backlog…: picks a folder that holds flint-backlog.json and shows it. */
export async function openBacklog(): Promise<void> {
  const dir = await pickFolder()
  if (!dir) return
  try {
    const info = await API.Open(dir)
    await refreshBacklogs()
    selectBacklog(info.path)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** New Backlog…: picks a folder, then asks for a name (the dialog calls createBacklog). */
export async function newBacklog(): Promise<void> {
  const dir = await pickFolder()
  if (!dir) return
  useWorkspace.getState().setSidebar('backlog')
  useBacklog.setState({ dialog: { kind: 'newBacklog', folder: dir } })
}

/** Makes the folder a backlog and shows it. Returns the error to show, '' on success. */
export async function createBacklog(folder: string, name: string): Promise<string> {
  try {
    const info = await API.Create(folder, name.trim())
    await refreshBacklogs()
    selectBacklog(info.path)
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

/** Close Backlog: forgets a remembered backlog (its files and agent tasks stay). */
export async function closeBacklog(info: BacklogInfo): Promise<void> {
  const ok = await confirmAction(
    `Close the backlog "${info.name}"?\n\nIts files stay where they are. Flint forgets the folder and the links of its items to agent tasks (the tasks stay).`,
    'Close Backlog',
  )
  if (!ok) return
  try {
    await API.Close(info.path)
    await refreshBacklogs()
    const { selected } = useBacklog.getState()
    if (selected) void loadBacklog(selected)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export function closeDialog(): void {
  useBacklog.setState({ dialog: null })
}

// ---- items

/** New Item…: asks for a title in the panel (or in the board that asked). */
export function openNewItem(folder: string, host: DialogHost = 'panel'): void {
  if (!folder) {
    showToast('Open or create a backlog first: Open Backlog… or New Backlog…', 'error')
    return
  }
  if (host === 'panel') useWorkspace.getState().setSidebar('backlog')
  useBacklog.setState({ dialog: { kind: 'newItem', folder, host } })
}

/** Writes the item's file from a template and opens it. Returns the error to show, '' on success. */
export async function createItem(folder: string, title: string): Promise<string> {
  try {
    const item = await API.NewItem(folder, title.trim())
    useEditor.getState().openFile(item.path)
    void loadBacklog(folder)
    return ''
  } catch (err) {
    return errorMessage(err)
  }
}

export const openItemFile = (item: ItemInfo): void => useEditor.getState().openFile(item.path)

async function withBusy(folder: string, id: string, fn: () => Promise<void>): Promise<void> {
  const key = itemKey(folder, id)
  if (useBacklog.getState().busy[key]) return
  useBacklog.setState((s) => ({ busy: { ...s.busy, [key]: true } }))
  try {
    await fn()
  } finally {
    useBacklog.setState((s) => ({ busy: { ...s.busy, [key]: false } }))
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
    await loadBacklog(folder)
  })
}

// ---- Repositories and Delegate

/** Locate…: says which repository to find, then asks for the folder. False when the user stopped or it was refused. */
export async function locateRepo(folder: string, repo: string, url: string): Promise<boolean> {
  if (!(await confirmAction(locateQuestion(repo, url), 'Choose Folder…'))) return false
  const dir = await pickFolder()
  if (!dir) return false
  try {
    await API.Locate(folder, repo, dir)
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return false
  }
  void loadBacklog(folder)
  return true
}

/** Adds a located checkout outside the workspace to it. */
export async function attachRepo(folder: string, repo: string, dir: string): Promise<boolean> {
  if (!(await showFolder(dir))) return false
  showToast(`Added ${dir} to the workspace`)
  void loadBacklog(folder)
  return true
}

/** Trusts a repository's checkout after saying that it is for the whole folder, from now on. */
export async function trustRepo(folder: string, repo: string, dir: string): Promise<boolean> {
  if (!(await confirmAction(trustQuestion(repo, dir), 'Trust Folder'))) return false
  await trustFolder(dir)
  void loadBacklog(folder)
  return true
}

/** Trusts the backlog's own folder (get_backlog shows it then, and Delegate stops asking). */
export async function trustBacklog(info: Pick<BacklogInfo, 'path' | 'name'>): Promise<void> {
  if (!(await confirmAction(trustBacklogQuestion(info.name, info.path), 'Trust Folder'))) return
  await trustFolder(info.path)
  await refreshBacklogs()
  void loadBacklog(info.path)
}

/** The actions of the Repositories group: Locate…, Add to Workspace, Trust. */
export const repoActions = {
  locate: (folder: string, r: RepoView) => void locateRepo(folder, r.name, r.url),
  attach: (folder: string, r: RepoView) => void attachRepo(folder, r.name, r.folder),
  trust: (folder: string, r: RepoView) => void trustRepo(folder, r.name, r.folder),
}

/** Fixes one thing in the way of Delegate with the user's help; false when the user stopped. */
async function resolve(folder: string, issue: DelegateIssue): Promise<boolean> {
  switch (issue.fix) {
    case 'locate':
      return locateRepo(folder, issue.repo, issue.url)
    case 'attach':
      return attachRepo(folder, issue.repo, issue.folder)
    case 'trust':
      return trustRepo(folder, issue.repo, issue.folder)
    default:
      return false
  }
}

/**
 * Delegate: starts the item's agent tasks. Before asking Go it walks the user
 * through what is missing (Locate…, attaching the folder, trusting it), one step
 * at a time (delegateStep); a step that made no progress stops it with the reason.
 * Go starts the chats itself; the window adopts them from "chat:opened", so none
 * is started here. Go asks first, natively, for an item of an untrusted backlog.
 */
export function delegateItem(folder: string, id: string): Promise<void> {
  return withBusy(folder, id, async () => {
    let last = ''
    for (let step = 0; step < 12; step++) {
      await loadBacklog(folder)
      const backlog = useBacklog.getState().data[folder]?.backlog
      const item = backlog?.items.find((i) => i.id === id)
      if (!backlog || !item) return showToast('The item is not in the backlog any more', 'error')
      const next = delegateStep(delegateIssues(item, backlog.repositories, activeRoot()), last)
      if (next.kind === 'delegate') break
      if (next.kind === 'stuck') return showToast(next.message, 'error')
      if (!(await resolve(folder, next.issue))) return // the user stopped
      last = next.sig
    }
    try {
      const res = await API.Delegate(folder, id, activeRoot())
      const n = res.tasks?.length ?? 0
      if (res.warning) showToast(`${n} agent ${n === 1 ? 'task' : 'tasks'} started, but ${res.warning}`, 'error')
      else showToast(`Started ${n} agent ${n === 1 ? 'task' : 'tasks'}`)
      void loadTasks()
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
    await loadBacklog(folder)
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

/** Opens the board of a backlog in a tab. */
export function openBoard(info: Pick<BacklogInfo, 'path' | 'name'>): void {
  useEditor.getState().openContributed(BOARD_VIEW, info.path, boardTitle(info.name), { folder: info.path })
}

/** Show Backlog Board: the selected backlog's, listing the backlogs first when none is known yet. */
export async function showBoard(): Promise<void> {
  if (useBacklog.getState().infos === null) await refreshBacklogs()
  const { infos, selected } = useBacklog.getState()
  const info = infos?.find((i) => i.path === selected)
  if (!info) return showToast('No backlog yet: Open Backlog… or New Backlog…', 'error')
  openBoard(info)
}

/** Ask the agent: hands a problem file to the agent chat. */
export function askAboutProblem(backlog: BacklogData, p: ProblemInfo): void {
  const path = `${backlog.path}/${p.file}`
  void askAgent({ prompt: problemPrompt(backlog.name, backlog.path, p), attachments: [{ path }], root: rootFor(path) })
}
